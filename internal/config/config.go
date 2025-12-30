package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ServerConfig holds server-related settings.
type ServerConfig struct {
	Addr      string
	AuthToken string
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level     string
	Retention int
}

// BarkConfig holds Bark notification settings.
type BarkConfig struct {
	URL     string
	Enabled bool
}

// NotificationConfig holds all notification settings.
type NotificationConfig struct {
	Bark BarkConfig
}

// Config holds all runtime configuration options for the daemon.
type Config struct {
	Server       ServerConfig
	Log          LogConfig
	Notification NotificationConfig

	// Flat fields for compatibility and command-line flags
	StateDir      string
	UseUTC        bool
	ShutdownGrace time.Duration

	// Legacy fields mapped to nested ones
	Addr       string
	LogLevel   string
	RunLogKeep int
	AuthToken  string
}

const (
	defaultAddr          = "0.0.0.0:7070"
	defaultLogLevel      = "info"
	defaultRunLogKeep    = 20
	defaultShutdownGrace = 5 * time.Second
)

// getEnvString returns the environment variable value or default
func getEnvString(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

// getEnvInt returns the environment variable as int or default
func getEnvInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvBool returns the environment variable as bool or default
func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		lower := strings.ToLower(val)
		return lower == "true" || lower == "1" || lower == "yes"
	}
	return defaultVal
}

// getEnvDuration returns the environment variable as duration or default
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

// Parse parses command line flags and environment variables into Config.
// Priority: CLI flags > Environment variables > .env file > defaults
func Parse() (*Config, error) {
	// Load .env file if exists (silent fail if not present)
	// Check multiple locations: current directory, then config directory
	envFiles := []string{".env"}
	if configDir, err := os.UserConfigDir(); err == nil {
		envFiles = append(envFiles, filepath.Join(configDir, "clicrontab", ".env"))
	}
	_ = godotenv.Load(envFiles...) // Ignore error - file is optional

	// Build config from environment variables with defaults
	cfg := &Config{
		Server: ServerConfig{
			Addr:      getEnvString("CLICRON_ADDR", defaultAddr),
			AuthToken: getEnvString("CLICRON_AUTH_TOKEN", ""),
		},
		Log: LogConfig{
			Level:     getEnvString("CLICRON_LOG_LEVEL", defaultLogLevel),
			Retention: getEnvInt("CLICRON_LOG_RETENTION", defaultRunLogKeep),
		},
		Notification: NotificationConfig{
			Bark: BarkConfig{
				URL:     getEnvString("CLICRON_BARK_URL", ""),
				Enabled: getEnvBool("CLICRON_BARK_ENABLED", false),
			},
		},
		StateDir:      getEnvString("CLICRON_STATE_DIR", ""),
		UseUTC:        getEnvBool("CLICRON_USE_UTC", false),
		ShutdownGrace: getEnvDuration("CLICRON_SHUTDOWN_GRACE", defaultShutdownGrace),
	}

	// Define CLI flags (these will override environment variables)
	var addr, logLevel string
	var runLogKeep int
	var stateDir string
	var useUTC bool
	var shutdownGrace time.Duration

	flag.StringVar(&addr, "addr", "", "HTTP listen address (overrides env)")
	flag.StringVar(&stateDir, "state-dir", "", "Directory to store database and run logs")
	flag.StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	flag.BoolVar(&useUTC, "use-utc", false, "Use UTC for cron evaluation instead of system local time")
	flag.IntVar(&runLogKeep, "run-log-keep", 0, "Number of recent runs to retain per task")
	flag.DurationVar(&shutdownGrace, "shutdown-grace", 0, "Grace period when shutting down")

	flag.Parse()

	// Apply CLI flags if set (they take precedence)
	if addr != "" {
		cfg.Server.Addr = addr
	}
	if logLevel != "" {
		cfg.Log.Level = logLevel
	}
	if runLogKeep > 0 {
		cfg.Log.Retention = runLogKeep
	}
	if stateDir != "" {
		cfg.StateDir = stateDir
	}
	// For bool flags, check if explicitly set via flag.Visit
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "use-utc":
			cfg.UseUTC = useUTC
		case "shutdown-grace":
			cfg.ShutdownGrace = shutdownGrace
		}
	})

	// Sync flat fields for backward compatibility
	cfg.Addr = cfg.Server.Addr
	cfg.AuthToken = cfg.Server.AuthToken
	cfg.LogLevel = cfg.Log.Level
	cfg.RunLogKeep = cfg.Log.Retention

	// Resolve state dir if not set
	if cfg.StateDir == "" {
		dir, err := defaultStateDir()
		if err != nil {
			return nil, fmt.Errorf("resolve default state dir: %w", err)
		}
		cfg.StateDir = dir
	}

	// Ensure retention is valid
	if cfg.RunLogKeep < 1 {
		cfg.RunLogKeep = defaultRunLogKeep
		cfg.Log.Retention = defaultRunLogKeep
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
