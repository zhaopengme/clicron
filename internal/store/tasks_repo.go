package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clicrontab/internal/core"
)

var ErrTaskNotFound = errors.New("task not found")

func (s *Store) InsertTask(ctx context.Context, task *core.Task) error {
	now := time.Now().UTC()
	task.CreatedAt = now
	task.UpdatedAt = now
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO tasks (id, name, command, cron, timeout_seconds, status, last_run_at, next_run_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, task.ID, nullableString(task.Name), task.Command, task.Cron, nullableInt(task.TimeoutSeconds),
		task.Status, nullableTime(task.LastRunAt), nullableTime(task.NextRunAt),
		task.CreatedAt.Format(time.RFC3339Nano), task.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (s *Store) UpdateTask(ctx context.Context, task *core.Task) error {
	task.UpdatedAt = time.Now().UTC()
	res, err := s.DB.ExecContext(ctx, `
		UPDATE tasks
		SET name = ?, command = ?, cron = ?, timeout_seconds = ?, status = ?, last_run_at = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableString(task.Name), task.Command, task.Cron, nullableInt(task.TimeoutSeconds), task.Status,
		nullableTime(task.LastRunAt), nullableTime(task.NextRunAt), task.UpdatedAt.Format(time.RFC3339Nano), task.ID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task rows: %w", err)
	}
	if rows == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *Store) GetTask(ctx context.Context, id string) (*core.Task, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, name, command, cron, timeout_seconds, status, last_run_at, next_run_at, created_at, updated_at
		FROM tasks WHERE id = ?
	`, id)
	task, err := scanTask(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

func (s *Store) ListTasks(ctx context.Context, status *core.TaskStatus) ([]*core.Task, error) {
	var rows *sql.Rows
	var err error
	if status != nil {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, name, command, cron, timeout_seconds, status, last_run_at, next_run_at, created_at, updated_at
			FROM tasks
			WHERE status = ?
			ORDER BY created_at DESC
		`, *status)
	} else {
		rows, err = s.DB.QueryContext(ctx, `
			SELECT id, name, command, cron, timeout_seconds, status, last_run_at, next_run_at, created_at, updated_at
			FROM tasks
			ORDER BY created_at DESC
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()
	var tasks []*core.Task
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Store) UpdateTaskScheduleInfo(ctx context.Context, id string, lastRunAt, nextRunAt *time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE tasks
		SET last_run_at = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableTime(lastRunAt), nullableTime(nextRunAt), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update task schedule info: %w", err)
	}
	return nil
}

func (s *Store) UpdateTaskNextRun(ctx context.Context, id string, nextRunAt *time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE tasks
		SET next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, nullableTime(nextRunAt), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update next_run_at: %w", err)
	}
	return nil
}

func (s *Store) UpdateTaskStatus(ctx context.Context, id string, status core.TaskStatus) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, updated_at = ?
		WHERE id = ?
	`, status, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	return nil
}

func scanTask(scanner interface {
	Scan(dest ...any) error
}) (*core.Task, error) {
	var (
		id        string
		name      sql.NullString
		command   string
		cronExpr  string
		timeout   sql.NullInt64
		status    string
		lastRun   sql.NullString
		nextRun   sql.NullString
		createdAt string
		updatedAt string
	)
	if err := scanner.Scan(&id, &name, &command, &cronExpr, &timeout, &status, &lastRun, &nextRun, &createdAt, &updatedAt); err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	task := &core.Task{
		ID:      id,
		Command: command,
		Cron:    cronExpr,
		Status:  core.TaskStatus(status),
	}
	if name.Valid {
		task.Name = &name.String
	}
	if timeout.Valid {
		val := int(timeout.Int64)
		task.TimeoutSeconds = &val
	}
	if lastRun.Valid {
		if t, err := time.Parse(time.RFC3339Nano, lastRun.String); err == nil {
			task.LastRunAt = &t
		}
	}
	if nextRun.Valid {
		if t, err := time.Parse(time.RFC3339Nano, nextRun.String); err == nil {
			task.NextRunAt = &t
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		task.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		task.UpdatedAt = t
	}
	return task, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}
