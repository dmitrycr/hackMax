package config

import (
	"errors"
	"net/url"
	"os"
)

type Config struct {
	HTTPAddr       string
	DatabaseURL    string
	ProcessorURL   string
	ProcessorToken string
	StorageDir     string
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:       env("HTTP_ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		ProcessorURL:   env("PROCESSOR_URL", "http://localhost:8000"),
		ProcessorToken: os.Getenv("PROCESSOR_TOKEN"),
		StorageDir:     env("STORAGE_DIR", "./data/files"),
	}
	if c.DatabaseURL == "" || c.ProcessorToken == "" {
		return c, errors.New("DATABASE_URL and PROCESSOR_TOKEN are required")
	}
	u, err := url.Parse(c.ProcessorURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return c, errors.New("PROCESSOR_URL must be an HTTP(S) origin without credentials")
	}
	return c, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
