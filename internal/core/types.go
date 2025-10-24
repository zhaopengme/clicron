package core

import (
	"time"
)

// TaskStatus describes the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusActive TaskStatus = "active"
	TaskStatusPaused TaskStatus = "paused"
)

// RunStatus describes the state of an individual execution.
type RunStatus string

const (
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
	RunStatusTimedOut  RunStatus = "timed_out"
	RunStatusSkipped   RunStatus = "skipped"
)

// Task represents a scheduled automation command.
type Task struct {
	ID             string
	Name           *string
	Command        string
	Cron           string
	TimeoutSeconds *int
	WorkingDir     *string
	Status         TaskStatus
	LastRunAt      *time.Time
	NextRunAt      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Run captures a single execution attempt of a task.
type Run struct {
	ID          string
	TaskID      string
	Status      RunStatus
	ScheduledAt time.Time
	StartedAt   *time.Time
	EndedAt     *time.Time
	ExitCode    *int
	Error       *string
	CreatedAt   time.Time
}
