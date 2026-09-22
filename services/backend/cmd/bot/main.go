package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/joho/godotenv"
	maxintegration "hackmax/backend/internal/integrations/max"
)

func main() {
	if err := run(); err != nil {
		slog.Error("MAX-бот остановлен", "error", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := flag.String("env-file", "", "путь к .env для локального запуска")
	flag.Parse()
	if *envFile != "" {
		// Load preserves variables explicitly supplied by the process environment.
		if err := godotenv.Load(*envFile); err != nil {
			return fmt.Errorf("загрузка .env: %w", err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var options maxintegration.Options
	if value := os.Getenv("MAX_INSECURE_SKIP_VERIFY"); value != "" {
		insecure, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("MAX_INSECURE_SKIP_VERIFY должен быть true или false")
		}
		options.InsecureSkipVerify = insecure
	}
	return maxintegration.Run(ctx, os.Getenv("MAX_BOT_TOKEN"), options)
}
