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

	cmdCtx := ctx
	cancel := func() {}
	if task.TimeoutSeconds != nil && *task.TimeoutSeconds > 0 {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := commandForTask(cmdCtx, task.Command)
	cmd.Stdout = runLogWriter
	cmd.Stderr = runLogWriter

	var timeoutTriggered atomic.Bool
	var watchdog *time.Timer
	if task.TimeoutSeconds != nil && *task.TimeoutSeconds > 0 {
		duration := time.Duration(*task.TimeoutSeconds) * time.Second
		watchdog = time.AfterFunc(duration, func() {
			timeoutTriggered.Store(true)
			e.logger.Warn("task exceeded timeout, sending termination", "task_id", task.ID, "run_id", run.ID, "timeout", duration)
			sendTermination(cmd.Process)
			time.AfterFunc(5*time.Second, func() {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			})
		})
	}

	err = cmd.Start()
	if err != nil {
		e.store.MarkRunCompleted(ctx, run.ID, RunStatusFailed, time.Now().UTC(), nil, ptrString(fmt.Sprintf("failed to start command: %v", err)))
		return fmt.Errorf("start command: %w", err)
	}
	waitErr := cmd.Wait()
	if watchdog != nil {
		watchdog.Stop()
	}

	endedAt := time.Now().UTC()
	var exitCode *int
	var status RunStatus
	var errMsg *string

	if timeoutTriggered.Load() {
		status = RunStatusTimedOut
		errMsg = ptrString("run timed out")
	} else if waitErr == nil {
		status = RunStatusSucceeded
		code := 0
		exitCode = &code
	} else {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code := exitErr.ExitCode()
			exitCode = &code
		}
		status = RunStatusFailed
		errMsg = ptrString(waitErr.Error())
	}

	if err := e.store.MarkRunCompleted(ctx, run.ID, status, endedAt, exitCode, errMsg); err != nil {
		return fmt.Errorf("mark run completed: %w", err)
	}
	return nil
}

func commandForTask(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command) // #nosec G204
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command) // #nosec G204
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

func sendTermination(process *os.Process) {
	if process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = process.Kill()
		return
	}
	_ = process.Signal(syscall.SIGTERM)
}

func ptrString(v string) *string {
	return &v
}
