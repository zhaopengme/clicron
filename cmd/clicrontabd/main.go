package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clicrontab/internal/api"
	"clicrontab/internal/config"
	"clicrontab/internal/core"
	clicrontabmcp "clicrontab/internal/mcp"
	"clicrontab/internal/logging"
	"clicrontab/internal/store"
)

func main() {
	cfg, err := config.Parse()
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}

	logger := logging.New(cfg.LogLevel)

	baseCtx := context.Background()
	storeInst, err := store.Open(baseCtx, cfg.StateDir, cfg.RunLogKeep)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer storeInst.DB.Close()

	location := time.Local
	if cfg.UseUTC {
		location = time.UTC
	}

	executor := core.NewCommandExecutor(storeInst, logger)
	scheduler := core.NewScheduler(storeInst, executor, logger, location)

	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	scheduler.Start(ctx)
	if err := scheduler.Sync(ctx); err != nil {
		logger.Error("initial sync", "err", err)
	}

	// Run based on mode
	switch cfg.Mode {
	case "http", "":
		runHTTPMode(cfg, storeInst, scheduler, logger, location, ctx, cancel)
	case "mcp":
		runMCPMode(storeInst, scheduler, logger, location, ctx, cancel)
	case "both":
		runBothMode(cfg, storeInst, scheduler, logger, location, ctx, cancel)
	default:
		logger.Error("invalid mode", "mode", cfg.Mode, "valid", []string{"http", "mcp", "both"})
		os.Exit(1)
	}
}

// runHTTPMode starts only the HTTP server.
func runHTTPMode(cfg *config.Config, store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location, ctx context.Context, cancel context.CancelFunc) {
	server, err := api.NewServer(cfg.Addr, store, scheduler, logger, location)
	if err != nil {
		logger.Error("create server", "err", err)
		os.Exit(1)
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigs:
		logger.Info("received signal", "signal", sig.String())
	case err := <-serverErr:
		logger.Error("server error", "err", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", "err", err)
	}

	stopCtx := scheduler.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(cfg.ShutdownGrace):
		logger.Warn("scheduler stop timed out")
	}
}

// runMCPMode starts only the MCP server.
func runMCPMode(store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location, ctx context.Context, cancel context.CancelFunc) {
	// Create MCP server
	mcpServer := clicrontabmcp.NewMCPServer(store, scheduler, logger, location)

	// Handle shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigs
		logger.Info("received signal, shutting down...")
		cancel()
	}()

	// Run MCP server (blocking)
	if err := mcpServer.Run(); err != nil {
		logger.Error("mcp server error", "err", err)
		os.Exit(1)
	}
}

// runBothMode starts both HTTP and MCP servers.
func runBothMode(cfg *config.Config, store *store.Store, scheduler *core.Scheduler, logger *slog.Logger, location *time.Location, ctx context.Context, cancel context.CancelFunc) {
	// Start MCP server in background
	mcpServer := clicrontabmcp.NewMCPServer(store, scheduler, logger, location)
	mcpErr := make(chan error, 1)
	go func() {
		if err := mcpServer.Run(); err != nil {
			mcpErr <- err
		}
	}()

	// Start HTTP server
	server, err := api.NewServer(cfg.Addr, store, scheduler, logger, location)
	if err != nil {
		logger.Error("create server", "err", err)
		os.Exit(1)
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigs:
		logger.Info("received signal", "signal", sig.String())
	case err := <-serverErr:
		logger.Error("server error", "err", err)
	case err := <-mcpErr:
		logger.Error("mcp server error", "err", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", "err", err)
	}

	stopCtx := scheduler.Stop()
	select {
	case <-stopCtx.Done():
	case <-time.After(cfg.ShutdownGrace):
		logger.Warn("scheduler stop timed out")
	}

	// Note: MCP server will be terminated when the process exits
	logger.Info("shutdown complete")
}
