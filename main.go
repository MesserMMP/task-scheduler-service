package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"task-scheduler-service/scheduler"
)

func main() {
	cfg, err := Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := scheduler.NewPostgresRepository(pool)
	if err := repo.EnsureSchema(ctx); err != nil {
		logger.Error("ensure schema", "error", err)
		os.Exit(1)
	}

	svc := scheduler.NewService(repo, &http.Client{Timeout: cfg.RequestTimeout}, cfg.RetryBaseDelay, logger)
	h := scheduler.NewHandler(svc, logger)

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go svc.StartScheduler(ctx, cfg.SchedulerPollInterval, cfg.SchedulerBatchSize)

	go func() {
		logger.Info("http server started", "port", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server crashed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown", "error", err)
	}

	svc.Wait()
	logger.Info("shutdown completed")
}
