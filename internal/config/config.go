package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds all runtime configuration options for the daemon.
type Config struct {
	Addr          string
	StateDir      string
	LogLevel      string
	UseUTC        bool
	RunLogKeep    int
	ShutdownGrace time.Duration
}

const (
	defaultAddr          = "127.0.0.1:7070"
	defaultLogLevel      = "info"
	defaultRunLogKeep    = 20
	defaultShutdownGrace = 5 * time.Second
)

// Parse parses command line flags into Config.
func Parse() (*Config, error) {
	cfg := &Config{}
	flag.StringVar(&cfg.Addr, "addr", defaultAddr, "HTTP listen address (only 127.0.0.1 is allowed)")
	flag.StringVar(&cfg.StateDir, "state-dir", "", "Directory to store database and run logs (default: OS-specific user state dir)")
	flag.StringVar(&cfg.LogLevel, "log-level", defaultLogLevel, "Log level (debug, info, warn, error)")
	flag.BoolVar(&cfg.UseUTC, "use-utc", false, "Use UTC for cron evaluation instead of system local time")
	flag.IntVar(&cfg.RunLogKeep, "run-log-keep", defaultRunLogKeep, "Number of recent runs to retain per task")
	flag.DurationVar(&cfg.ShutdownGrace, "shutdown-grace", defaultShutdownGrace, "Grace period when shutting down HTTP server and running jobs")
	flag.Parse()

	if cfg.StateDir == "" {
		dir, err := defaultStateDir()
		if err != nil {
			return nil, fmt.Errorf("resolve default state dir: %w", err)
		}
		cfg.StateDir = dir
	}
	if cfg.RunLogKeep < 1 {
		cfg.RunLogKeep = defaultRunLogKeep
	}
	return cfg, nil
}

func defaultStateDir() (string, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(baseDir, "clicrontab")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}
