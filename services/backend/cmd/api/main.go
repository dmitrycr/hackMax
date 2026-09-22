package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hackmax/backend/internal/config"
	"hackmax/backend/internal/integrations/processor"
	"hackmax/backend/internal/platform/postgres"
	httptransport "hackmax/backend/internal/transport/http"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
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
	proc := processor.New(cfg.ProcessorURL, cfg.ProcessorToken)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: httptransport.New(httptransport.Dependencies{Database: pool.Ping, Processor: proc.Ready}), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	errors := make(chan error, 1)
	go func() { slog.Info("api listening", "address", cfg.HTTPAddr); errors <- server.ListenAndServe() }()
	select {
	case err := <-errors:
		if err != http.ErrServerClosed {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
