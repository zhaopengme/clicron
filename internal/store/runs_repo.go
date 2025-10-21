package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"clicrontab/internal/core"
)

var ErrRunNotFound = errors.New("run not found")

func (s *Store) InsertRun(ctx context.Context, run *core.Run) error {
	now := time.Now().UTC()
	run.CreatedAt = now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO runs (id, task_id, status, scheduled_at, started_at, ended_at, exit_code, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.TaskID, run.Status, run.ScheduledAt.UTC().Format(time.RFC3339Nano),
		nullableTime(run.StartedAt), nullableTime(run.EndedAt), nullableInt(run.ExitCode), nullableString(run.Error),
		run.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

func (s *Store) MarkRunStarted(ctx context.Context, id string, startedAt time.Time) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE runs
		SET status = ?, started_at = ?
		WHERE id = ?
	`, core.RunStatusRunning, startedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("mark run started: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (s *Store) MarkRunCompleted(ctx context.Context, id string, status core.RunStatus, endedAt time.Time, exitCode *int, errMsg *string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE runs
		SET status = ?, ended_at = ?, exit_code = ?, error = ?
		WHERE id = ?
	`, status, endedAt.UTC().Format(time.RFC3339Nano), nullableInt(exitCode), nullableString(errMsg), id)
	if err != nil {
		return fmt.Errorf("mark run completed: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (s *Store) UpdateRunStatus(ctx context.Context, id string, status core.RunStatus, errMsg *string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE runs
		SET status = ?, error = ?
		WHERE id = ?
	`, status, nullableString(errMsg), id)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (s *Store) GetRun(ctx context.Context, id string) (*core.Run, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, task_id, status, scheduled_at, started_at, ended_at, exit_code, error, created_at
		FROM runs WHERE id = ?
	`, id)
	run, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRunNotFound
		}
		return nil, err
	}
	return run, nil
}

func (s *Store) ListRuns(ctx context.Context, taskID string, limit, offset int) ([]*core.Run, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, task_id, status, scheduled_at, started_at, ended_at, exit_code, error, created_at
		FROM runs
		WHERE task_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, taskID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var runs []*core.Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

// RunLogPath returns the absolute path for the run's combined log file.
func (s *Store) RunLogPath(runID string) string {
	return filepath.Join(s.StateDir, "runs", runID, "combined.log")
}

// EnsureRunLogDir makes sure the directory for a run's log exists.
func (s *Store) EnsureRunLogDir(runID string) error {
	return os.MkdirAll(filepath.Dir(s.RunLogPath(runID)), 0o755)
}

// PruneOldRunLogs removes log files beyond the retention limit for a task.
func (s *Store) PruneOldRunLogs(ctx context.Context, taskID string) error {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id FROM runs
		WHERE task_id = ?
		ORDER BY created_at DESC
		LIMIT -1 OFFSET ?
	`, taskID, s.LogRetention)
	if err != nil {
		return fmt.Errorf("query runs for pruning: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		path := s.RunLogPath(id)
		_ = os.Remove(path)
		dir := filepath.Dir(path)
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return rows.Err()
}

func scanRun(scanner interface {
	Scan(dest ...any) error
}) (*core.Run, error) {
	var (
		id          string
		taskID      string
		status      string
		scheduledAt string
		startedAt   sql.NullString
		endedAt     sql.NullString
		exitCode    sql.NullInt64
		errMsg      sql.NullString
		createdAt   string
	)
	if err := scanner.Scan(&id, &taskID, &status, &scheduledAt, &startedAt, &endedAt, &exitCode, &errMsg, &createdAt); err != nil {
		return nil, fmt.Errorf("scan run: %w", err)
	}
	run := &core.Run{
		ID:          id,
		TaskID:      taskID,
		Status:      core.RunStatus(status),
		ScheduledAt: mustParseTime(scheduledAt),
		CreatedAt:   mustParseTime(createdAt),
	}
	if startedAt.Valid {
		t := mustParseTime(startedAt.String)
		run.StartedAt = &t
	}
	if endedAt.Valid {
		t := mustParseTime(endedAt.String)
		run.EndedAt = &t
	}
	if exitCode.Valid {
		val := int(exitCode.Int64)
		run.ExitCode = &val
	}
	if errMsg.Valid {
		run.Error = &errMsg.String
	}
	return run, nil
}

func mustParseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		panic(fmt.Sprintf("invalid stored time %q: %v", value, err))
	}
	return t
}
