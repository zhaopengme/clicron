package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// CommandExecutor executes task commands and records their results.
type CommandExecutor struct {
	store  Store
	logger *slog.Logger
}

// NewCommandExecutor creates a new executor.
func NewCommandExecutor(store Store, logger *slog.Logger) *CommandExecutor {
	return &CommandExecutor{
		store:  store,
		logger: logger,
	}
}

// Execute runs the task command according to timeout and records run status.
func (e *CommandExecutor) Execute(ctx context.Context, task *Task, run *Run) error {
	if err := e.store.EnsureRunLogDir(run.ID); err != nil {
		return fmt.Errorf("ensure run log dir: %w", err)
	}
	logPath := e.store.RunLogPath(run.ID)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	runLogWriter := &syncWriter{w: logFile}

	startedAt := time.Now().UTC()
	if err := e.store.MarkRunStarted(ctx, run.ID, startedAt); err != nil {
		return fmt.Errorf("mark run started: %w", err)
	}
	if err := e.store.UpdateTaskScheduleInfo(ctx, task.ID, &startedAt, task.NextRunAt); err != nil {
		e.logger.Warn("update task schedule info", "task_id", task.ID, "err", err)
	}

	// Setup command context with timeout if configured
	cmdCtx := ctx
	cancel := func() {}
	var timeoutTriggered atomic.Bool
	var watchdog *time.Timer
	var killTimer *time.Timer

	if task.TimeoutSeconds != nil && *task.TimeoutSeconds > 0 {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := commandForTask(cmdCtx, task.Command)

	// Capture a tail of combined output for easier troubleshooting in service logs
	// while also writing full output to the run log file.
	outputTail := newTailBuffer(8 * 1024) // keep last 8KB
	multi := io.MultiWriter(runLogWriter, outputTail)
	cmd.Stdout = multi
	cmd.Stderr = multi

	// Set working directory if specified
	if task.WorkingDir != nil && *task.WorkingDir != "" {
		cmd.Dir = *task.WorkingDir
		e.logger.Debug("using working directory", "task_id", task.ID, "working_dir", *task.WorkingDir)
	}

	err = cmd.Start()
	if err != nil {
		e.store.MarkRunCompleted(ctx, run.ID, RunStatusFailed, time.Now().UTC(), nil, ptrString(fmt.Sprintf("failed to start command: %v", err)))
		return fmt.Errorf("start command: %w", err)
	}

	// Log process start with PID for debugging
	e.logger.Info("task process started", "task_id", task.ID, "run_id", run.ID, "pid", cmd.Process.Pid)

	// Start timeout watchdog after process has started
	if task.TimeoutSeconds != nil && *task.TimeoutSeconds > 0 {
		duration := time.Duration(*task.TimeoutSeconds) * time.Second
		watchdog = time.AfterFunc(duration, func() {
			timeoutTriggered.Store(true)
			e.logger.Warn("task exceeded timeout, sending termination", "task_id", task.ID, "run_id", run.ID, "timeout", duration)

			// First attempt: graceful termination (SIGTERM on Unix, Kill on Windows)
			sendTermination(cmd.Process)

			// Second attempt: force kill after 5 seconds if process still alive
			killTimer = time.AfterFunc(5*time.Second, func() {
				if cmd.Process != nil {
					e.logger.Warn("force killing task after grace period", "task_id", task.ID, "run_id", run.ID)
					_ = cmd.Process.Kill()
				}
			})
		})
	}

	waitErr := cmd.Wait()

	// Stop timers if they exist and haven't fired yet
	if watchdog != nil {
		watchdog.Stop()
	}
	if killTimer != nil {
		killTimer.Stop()
	}

	endedAt := time.Now().UTC()
	var exitCode *int
	var status RunStatus
	var errMsg *string

	if timeoutTriggered.Load() {
		status = RunStatusTimedOut
		errMsg = ptrString("run timed out")
		e.logger.Info(
			"task timed out",
			"task_id", task.ID,
			"run_id", run.ID,
			"pid", cmd.Process.Pid,
			"output_tail", outputTail.String(),
			"log_path", e.store.RunLogPath(run.ID),
		)
	} else if waitErr == nil {
		status = RunStatusSucceeded
		code := 0
		exitCode = &code
		e.logger.Info(
			"task completed successfully",
			"task_id", task.ID,
			"run_id", run.ID,
			"pid", cmd.Process.Pid,
			"exit_code", code,
			"output_tail", outputTail.String(),
			"log_path", e.store.RunLogPath(run.ID),
		)
	} else {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code := exitErr.ExitCode()
			exitCode = &code
		}
		status = RunStatusFailed
		errMsg = ptrString(waitErr.Error())
		e.logger.Warn(
			"task failed",
			"task_id", task.ID,
			"run_id", run.ID,
			"pid", cmd.Process.Pid,
			"exit_code", func() any {
				if exitCode != nil {
					return *exitCode
				}
				return nil
			}(),
			"error", waitErr,
			"output_tail", outputTail.String(),
			"log_path", e.store.RunLogPath(run.ID),
		)
	}

	if err := e.store.MarkRunCompleted(ctx, run.ID, status, endedAt, exitCode, errMsg); err != nil {
		return fmt.Errorf("mark run completed: %w", err)
	}
	return nil
}

// commandForTask creates an exec.Cmd for the given command.
// On Unix systems, it uses the user's default shell ($SHELL) as a login shell,
// which loads the user's shell configuration files (.bashrc, .zshrc, etc.).
// This ensures that user-defined PATH, aliases, environment variables, and functions are available.
func commandForTask(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command) // #nosec G204
	}

	// Use user's default shell with login mode to load configuration files
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh" // fallback to POSIX shell
	}

	// -l: login shell (loads .bash_profile, .zshrc, etc.)
	// -c: execute command string
	return exec.CommandContext(ctx, shell, "-l", "-c", command) // #nosec G204
}

type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// tailBuffer keeps only the last N bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func newTailBuffer(capacity int) *tailBuffer {
	if capacity <= 0 {
		capacity = 4096
	}
	return &tailBuffer{cap: capacity, buf: make([]byte, 0, capacity)}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(p) >= t.cap {
		// Only keep the last cap bytes from p
		t.buf = append(t.buf[:0], p[len(p)-t.cap:]...)
		return len(p), nil
	}
	// Ensure capacity by dropping from the front if needed
	need := len(p)
	if len(t.buf)+need > t.cap {
		drop := len(t.buf) + need - t.cap
		if drop >= len(t.buf) {
			t.buf = t.buf[:0]
		} else {
			t.buf = t.buf[drop:]
		}
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// sendTermination attempts to gracefully terminate a process.
// On Unix systems, it sends SIGTERM to allow the process to clean up resources.
// On Windows, graceful termination via signals is not supported, so it directly
// kills the process. This means Windows processes cannot perform cleanup operations
// when terminated due to timeout.
func sendTermination(process *os.Process) {
	if process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// Windows doesn't support SIGTERM, must use Kill directly
		_ = process.Kill()
		return
	}
	// Unix: send SIGTERM for graceful shutdown
	_ = process.Signal(syscall.SIGTERM)
}

func ptrString(v string) *string {
	return &v
}
