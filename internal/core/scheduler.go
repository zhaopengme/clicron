package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// Store abstracts the persistence layer used by the scheduler and executor.
type Store interface {
	// Task operations
	GetTask(ctx context.Context, id string) (*Task, error)
	ListTasks(ctx context.Context, status *TaskStatus) ([]*Task, error)
	UpdateTaskScheduleInfo(ctx context.Context, id string, lastRunAt, nextRunAt *time.Time) error
	UpdateTaskNextRun(ctx context.Context, id string, nextRunAt *time.Time) error

	// Run operations
	InsertRun(ctx context.Context, run *Run) error
	MarkRunStarted(ctx context.Context, id string, startedAt time.Time) error
	MarkRunCompleted(ctx context.Context, id string, status RunStatus, endedAt time.Time, exitCode *int, errMsg *string) error
	UpdateRunStatus(ctx context.Context, id string, status RunStatus, errMsg *string) error

	// Log helpers
	EnsureRunLogDir(runID string) error
	RunLogPath(runID string) string
	PruneOldRunLogs(ctx context.Context, taskID string) error
}

// Executor runs commands associated with a task.
type Executor interface {
	Execute(ctx context.Context, task *Task, run *Run) error
}

// Scheduler manages cron-based scheduling and dispatching of tasks.
type Scheduler struct {
	store    Store
	executor Executor
	logger   *slog.Logger
	location *time.Location

	cron    *cron.Cron
	entryMu sync.RWMutex
	entries map[string]cron.EntryID

	running sync.Map // taskID -> struct{}{}

	ctx context.Context
}

// NewScheduler constructs a scheduler with the given dependencies.
func NewScheduler(store Store, executor Executor, logger *slog.Logger, location *time.Location) *Scheduler {
	if location == nil {
		location = time.Local
	}
	c := cron.New(
		cron.WithParser(cronParser),
		cron.WithLocation(location),
	)
	return &Scheduler{
		store:    store,
		executor: executor,
		logger:   logger,
		location: location,
		cron:     c,
		entries:  make(map[string]cron.EntryID),
	}
}

// Start begins the scheduling loop. ctx is used for background operations (DB updates, executor runs).
func (s *Scheduler) Start(ctx context.Context) {
	s.ctx = ctx
	s.cron.Start()
}

// Stop stops the scheduler and waits for currently running cron jobs to finish dispatch.
func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}

// Sync loads all tasks from the store and ensures they are scheduled appropriately.
func (s *Scheduler) Sync(ctx context.Context) error {
	tasks, err := s.store.ListTasks(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	for _, task := range tasks {
		if task.Status == TaskStatusActive {
			if err := s.scheduleTask(ctx, task); err != nil {
				s.logger.Error("schedule task", "task_id", task.ID, "err", err)
			}
		} else {
			s.unscheduleTask(task.ID)
		}
	}
	return nil
}

// AddOrUpdateTask updates the scheduler entry for a task that may have been created or modified.
func (s *Scheduler) AddOrUpdateTask(ctx context.Context, task *Task) error {
	s.unscheduleTask(task.ID)
	if task.Status == TaskStatusActive {
		if err := s.scheduleTask(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// RemoveTask stops scheduling for the given task ID.
func (s *Scheduler) RemoveTask(taskID string) {
	s.unscheduleTask(taskID)
}

// RunTaskNow enqueues an immediate execution for the task if it is not already running.
func (s *Scheduler) RunTaskNow(ctx context.Context, task *Task) (*Run, error) {
	if s.isTaskRunning(task.ID) {
		return nil, errors.New("task is already running")
	}
	run := &Run{
		ID:          NewID(),
		TaskID:      task.ID,
		Status:      RunStatusQueued,
		ScheduledAt: time.Now().UTC(),
	}
	if err := s.store.InsertRun(ctx, run); err != nil {
		return nil, err
	}
	s.launchExecution(task, run)
	return run, nil
}

func (s *Scheduler) scheduleTask(ctx context.Context, task *Task) error {
	schedule, err := ParseCron(task.Cron)
	if err != nil {
		return err
	}
	now := time.Now().In(s.location)
	nextTimes := NextOccurrences(schedule, now, 1)
	if len(nextTimes) == 1 {
		nextUTC := nextTimes[0].UTC()
		if err := s.store.UpdateTaskNextRun(ctx, task.ID, &nextUTC); err != nil {
			s.logger.Warn("update next_run_at failed", "task_id", task.ID, "err", err)
		}
	}
	job := func() {
		entryID, ok := s.getEntryID(task.ID)
		if !ok {
			return
		}
		entry := s.cron.Entry(entryID)
		scheduledAt := entry.Prev
		if scheduledAt.IsZero() {
			scheduledAt = time.Now().In(s.location)
		}
		next := entry.Next
		if !next.IsZero() {
			nextUTC := next.UTC()
			if err := s.store.UpdateTaskNextRun(s.ctxOrBackground(), task.ID, &nextUTC); err != nil {
				s.logger.Error("update next_run_at", "task_id", task.ID, "err", err)
			}
		}
		s.handleScheduledTrigger(task.ID, scheduledAt.In(time.UTC))
	}
	entryID := s.cron.Schedule(schedule, cron.FuncJob(job))
	s.setEntryID(task.ID, entryID)
	return nil
}

func (s *Scheduler) handleScheduledTrigger(taskID string, scheduledAt time.Time) {
	ctx := s.ctxOrBackground()
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		s.logger.Error("fetch task for scheduled run", "task_id", taskID, "err", err)
		return
	}
	if task.Status != TaskStatusActive {
		return
	}
	if s.isTaskRunning(task.ID) {
		s.logger.Info("skipping run because task is already running", "task_id", task.ID)
		run := &Run{
			ID:          NewID(),
			TaskID:      task.ID,
			Status:      RunStatusSkipped,
			ScheduledAt: scheduledAt,
		}
		if err := s.store.InsertRun(ctx, run); err != nil {
			s.logger.Error("record skipped run", "task_id", task.ID, "err", err)
		}
		return
	}
	run := &Run{
		ID:          NewID(),
		TaskID:      task.ID,
		Status:      RunStatusQueued,
		ScheduledAt: scheduledAt,
	}
	if err := s.store.InsertRun(ctx, run); err != nil {
		s.logger.Error("insert run", "task_id", task.ID, "err", err)
		return
	}
	s.launchExecution(task, run)
}

func (s *Scheduler) launchExecution(task *Task, run *Run) {
	s.markTaskRunning(task.ID, true)
	go func() {
		defer s.markTaskRunning(task.ID, false)
		ctx := s.ctxOrBackground()
		if err := s.executor.Execute(ctx, task, run); err != nil {
			s.logger.Error("execute task", "task_id", task.ID, "run_id", run.ID, "err", err)
		}
		if err := s.store.PruneOldRunLogs(ctx, task.ID); err != nil {
			s.logger.Warn("prune run logs", "task_id", task.ID, "err", err)
		}
	}()
}

func (s *Scheduler) setEntryID(taskID string, entryID cron.EntryID) {
	s.entryMu.Lock()
	defer s.entryMu.Unlock()
	s.entries[taskID] = entryID
}

func (s *Scheduler) getEntryID(taskID string) (cron.EntryID, bool) {
	s.entryMu.RLock()
	defer s.entryMu.RUnlock()
	id, ok := s.entries[taskID]
	return id, ok
}

func (s *Scheduler) unscheduleTask(taskID string) {
	s.entryMu.Lock()
	defer s.entryMu.Unlock()
	if entryID, ok := s.entries[taskID]; ok {
		s.cron.Remove(entryID)
		delete(s.entries, taskID)
	}
}

func (s *Scheduler) isTaskRunning(taskID string) bool {
	_, ok := s.running.Load(taskID)
	return ok
}

func (s *Scheduler) markTaskRunning(taskID string, running bool) {
	if running {
		s.running.Store(taskID, struct{}{})
	} else {
		s.running.Delete(taskID)
	}
}

func (s *Scheduler) ctxOrBackground() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
