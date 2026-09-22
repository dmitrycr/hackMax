// Probe enqueues one infrastructure check; the worker must complete it.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"hackmax/backend/internal/config"
	"hackmax/backend/internal/integrations/llm"
	"hackmax/backend/internal/jobs"
	"hackmax/backend/internal/platform/postgres"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	useLLM := flag.Bool("llm", false, "send a synthetic structured request to OpenRouter via River")
	flag.Parse()
	var args river.JobArgs = jobs.ProcessorProbeArgs{}
	label := "Python"
	waitTimeout := 90 * time.Second
	if *useLLM {
		if os.Getenv("OPENROUTER_API_KEY") == "" {
			return llm.ErrDisabled
		}
		limits, err := llm.LimitsFromEnv()
		if err != nil {
			return err
		}
		if _, err := llm.NewWithLimits(os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENROUTER_MODEL"), limits); err != nil {
			return err
		}
		waitTimeout = limits.Timeout + time.Minute
		args, label = jobs.LLMProbeArgs{}, "OpenRouter (schema and references validated)"
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	client, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		return err
	}
	result, err := client.Insert(ctx, args, &river.InsertOpts{MaxAttempts: 1})
	if err != nil {
		return err
	}
	for {
		var state string
		if err := pool.QueryRow(ctx, "SELECT state::text FROM river_job WHERE id=$1", result.Job.ID).Scan(&state); err != nil {
			return err
		}
		if state == "completed" {
			fmt.Printf("River → Go worker → %s: OK (job %d)\n", label, result.Job.ID)
			return nil
		}
		if state == "discarded" || state == "cancelled" {
			return fmt.Errorf("probe job %d: %s", result.Job.ID, state)
		}
		select {
		case <-ctx.Done():
			return errors.New("probe wait timed out; the queued job may still run, check worker logs before retrying")
		case <-time.After(250 * time.Millisecond):
		}
	}
}
