package main

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
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

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Error("invalid integer environment variable", "key", key, "value", v)
		os.Exit(1)
	}

	return n
}

func envList(key string) []string {
	var out []string

	for item := range strings.SplitSeq(os.Getenv(key), ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}

	return out
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
