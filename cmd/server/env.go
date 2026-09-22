package main

import (
	"log/slog"
	"os"
	"time"
)

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required environment variable", "key", key)
		os.Exit(1)
	}

	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}

	return v == "1" || v == "true" || v == "TRUE"
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := envOr(key, fallback.String())

	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		slog.Error("invalid duration environment variable", "key", key, "value", v)
		os.Exit(1)
	}

	return d
}
