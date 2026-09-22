package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"hackmax/backend/internal/config"
	"hackmax/backend/internal/integrations/processor"
	"hackmax/backend/internal/jobs"
	"hackmax/backend/internal/platform/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := postgres.Open(startup, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()
	workers := river.NewWorkers()
	river.AddWorker(workers, &jobs.ProcessorProbeWorker{Processor: processor.New(cfg.ProcessorURL, cfg.ProcessorToken)})
	client, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{Workers: workers, Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}}})
	if err != nil {
		return err
	}
	if err = client.Start(ctx); err != nil {
		return err
	}
	slog.Info("worker started", "registered_job", "system.processor_probe")
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.Stop(shutdown)
}
