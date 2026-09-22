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
	"hackmax/backend/internal/integrations/llm"
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
	limits, err := llm.LimitsFromEnv()
	if err != nil {
		return err
	}
	llmClient, err := llm.NewWithLimits(os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENROUTER_MODEL"), limits)
	if err != nil {
		return err
	}
	river.AddWorker(workers, &jobs.LLMProbeWorker{Client: llmClient, RequestTimeout: limits.Timeout})
	client, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{Workers: workers, Queues: map[string]river.QueueConfig{
		river.QueueDefault: {MaxWorkers: 2}, "llm": {MaxWorkers: 1},
	}})
	if err != nil {
		return err
	}
	if err = client.Start(ctx); err != nil {
		return err
	}
	slog.Info("worker started", "llm_configured", os.Getenv("OPENROUTER_API_KEY") != "")
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.Stop(shutdown)
}
