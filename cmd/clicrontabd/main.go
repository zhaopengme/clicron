package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clicrontab/internal/api"
	"clicrontab/internal/config"
	"clicrontab/internal/core"
	"clicrontab/internal/logging"
	clicrontabmcp "clicrontab/internal/mcp"
	"clicrontab/internal/notify"
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

	var notifier notify.Notifier = &notify.NoOpNotifier{}
	if cfg.Notification.Bark.Enabled && cfg.Notification.Bark.URL != "" {
		bark, err := notify.NewBarkNotifier(cfg.Notification.Bark.URL)
		if err != nil {
			logger.Error("init bark notifier", "err", err)
		} else {
			notifier = bark
			logger.Info("bark notification enabled", "url", cfg.Notification.Bark.URL)
		}
	}

	executor := core.NewCommandExecutor(storeInst, logger, notifier)
	scheduler := core.NewScheduler(storeInst, executor, logger, location)

	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	scheduler.Start(ctx)
	if err := scheduler.Sync(ctx); err != nil {
		logger.Error("initial sync", "err", err)
	}

	// Initialize MCP server handler
	mcpServer := clicrontabmcp.NewMCPServer(storeInst, scheduler, logger, location, cfg.Addr)

	// Initialize HTTP server (mounts MCP handler at /mcp)
	server, err := api.NewServer(cfg.Addr, cfg.AuthToken, storeInst, scheduler, mcpServer, logger, location)
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

	logger.Info("shutdown complete")
}
