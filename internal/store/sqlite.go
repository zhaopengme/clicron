package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store wraps the SQLite database and state configuration.
type Store struct {
	DB           *sql.DB
	StateDir     string
	LogRetention int
}

// Open opens the SQLite database located under stateDir and runs migrations.
func Open(ctx context.Context, stateDir string, logRetention int) (*Store, error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure state dir: %w", err)
	}
	dbPath := filepath.Join(stateDir, "db.sqlite")
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, fmt.Errorf("open sqlite: %w", err)
    }
    // SQLite allows only one writer. Multiple pooled connections can cause
    // frequent SQLITE_BUSY under concurrent schedules. Keep a single
    // connection so WAL+busy_timeout are consistently applied and writes
    // are serialized within the process.
    db.SetMaxOpenConns(1)
    db.SetMaxIdleConns(1)
	timeout := int((3 * time.Second) / time.Millisecond)
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", timeout)); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	if err := runMigrations(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{
		DB:           db,
		StateDir:     stateDir,
		LogRetention: logRetention,
	}, nil
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	type mig struct {
		Version string
		SQL     string
	}
	entries := []mig{
		{Version: "0001_init", SQL: mustReadMigration("migrations/0001_init.sql")},
		{Version: "0002_add_working_dir", SQL: mustReadMigration("migrations/0002_add_working_dir.sql")},
	}
	for _, entry := range entries {
		applied, err := isMigrationApplied(ctx, db, entry.Version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if _, err := db.ExecContext(ctx, entry.SQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Version, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`,
			entry.Version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Version, err)
		}
	}
	return nil
}

func isMigrationApplied(ctx context.Context, db *sql.DB, version string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, version).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	return count > 0, nil
}

func mustReadMigration(path string) string {
	data, err := migrations.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("read migration %s: %v", path, err))
	}
	return string(data)
}
