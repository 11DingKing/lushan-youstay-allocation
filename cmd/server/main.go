package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/11DingKing/lushan-youstay-allocation/internal/auth"
	"github.com/11DingKing/lushan-youstay-allocation/internal/booking"
	"github.com/11DingKing/lushan-youstay-allocation/internal/bootstrap"
	"github.com/11DingKing/lushan-youstay-allocation/internal/clock"
	"github.com/11DingKing/lushan-youstay-allocation/internal/config"
	"github.com/11DingKing/lushan-youstay-allocation/internal/domain"
	"github.com/11DingKing/lushan-youstay-allocation/internal/httpapi"
	"github.com/11DingKing/lushan-youstay-allocation/internal/operations"
	"github.com/11DingKing/lushan-youstay-allocation/internal/settlement"
	storesqlite "github.com/11DingKing/lushan-youstay-allocation/internal/store/sqlite"
	"github.com/11DingKing/lushan-youstay-allocation/internal/worker"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	if cfg.DatabasePath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
			return err
		}
	}
	root, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	store, err := storesqlite.Open(root, cfg.DatabasePath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := bootstrap.SeedCatalog(root, store, clock.Real{}.Now()); err != nil {
		return err
	}
	authService := auth.NewService(store, clock.Real{}, cfg.SessionTTL)
	bookingService := booking.NewService(store, clock.Real{}, booking.DefaultPricePolicy(), 15*60*1e9)
	operationsService := operations.NewService(store, clock.Real{})
	settlementService := settlement.NewService(store, clock.Real{})
	dispatch := worker.NewDispatchHandler()
	dispatch.Register(domain.TaskCleaning, func(ctx context.Context, task domain.OperationalTask) error { return ctx.Err() })
	dispatch.Register(domain.TaskInspection, func(ctx context.Context, task domain.OperationalTask) error { return ctx.Err() })
	dispatch.Register(domain.TaskRepair, func(ctx context.Context, task domain.OperationalTask) error { return ctx.Err() })
	dispatch.Register(domain.TaskRelocation, func(ctx context.Context, task domain.OperationalTask) error { return ctx.Err() })
	runner := worker.NewRunner(clock.Real{}, store, authService, store, dispatch, cfg.WorkerInterval, logger)
	go func() {
		if err := runner.Run(root); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker exited", "error", err)
			cancel()
		}
	}()
	handler := httpapi.New(httpapi.Dependencies{Auth: authService, Booking: bookingService, Operations: operationsService, Settlement: settlementService, Store: store, Logger: logger})
	server := &http.Server{Addr: cfg.ListenAddr, Handler: handler, ReadHeaderTimeout: 5e9, ReadTimeout: 15e9, WriteTimeout: 30e9, IdleTimeout: 60e9}
	result := make(chan error, 1)
	go func() { logger.Info("server listening", "address", cfg.ListenAddr); result <- server.ListenAndServe() }()
	select {
	case err := <-result:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-root.Done():
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	return nil
}
