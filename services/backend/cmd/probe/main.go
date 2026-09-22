// Probe enqueues one infrastructure check; the worker must complete it.
package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"hackmax/backend/internal/config"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	result, err := client.Insert(ctx, jobs.ProcessorProbeArgs{}, &river.InsertOpts{MaxAttempts: 1})
	if err != nil {
		return err
	}
	for {
		var state string
		if err := pool.QueryRow(ctx, "SELECT state::text FROM river_job WHERE id=$1", result.Job.ID).Scan(&state); err != nil {
			return err
		}
		if state == "completed" {
			fmt.Printf("River → Go worker → Python: OK (job %d)\n", result.Job.ID)
			return nil
		}
		if state == "discarded" || state == "cancelled" {
			return fmt.Errorf("probe job %d: %s", result.Job.ID, state)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
