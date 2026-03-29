package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPPort              string
	DatabaseURL           string
	SchedulerPollInterval time.Duration
	SchedulerBatchSize    int
	RetryBaseDelay        time.Duration
	RequestTimeout        time.Duration
	ShutdownTimeout       time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		HTTPPort:              envOrDefault("HTTP_PORT", "8080"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		SchedulerPollInterval: durationOrDefault("SCHEDULER_POLL_INTERVAL", time.Second),
		SchedulerBatchSize:    intOrDefault("SCHEDULER_BATCH_SIZE", 10),
		RetryBaseDelay:        durationOrDefault("RETRY_BASE_DELAY", 5*time.Second),
		RequestTimeout:        durationOrDefault("REQUEST_TIMEOUT", 15*time.Second),
		ShutdownTimeout:       durationOrDefault("SHUTDOWN_TIMEOUT", 10*time.Second),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func intOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
