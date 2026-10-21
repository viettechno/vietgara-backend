package main

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultAppBaseURL is the local Kong entry point (KONG_PROXY_PORT).
const defaultAppBaseURL = "http://localhost:8000"

// appBaseURL is the public URL of this deployment (local, staging or
// production). The web admin and the API share its origin behind Kong, so it
// is also the Google OAuth origin and the base of e-mailed links.
func appBaseURL() string {
	return envURL("APP_BASE_URL", defaultAppBaseURL)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required environment variable", "key", key)
		os.Exit(1)
	}

	return v
}

// minSigningKeyLen is the shortest JWT signing key accepted: 32 bytes, the
// size of the HMAC-SHA256 output (`openssl rand -hex 32` gives 64 characters).
const minSigningKeyLen = 32

// mustSigningKey returns the JWT signing key, refusing to start with one too
// short to resist guessing.
func mustSigningKey(key string) []byte {
	v := mustEnv(key)
	if len(v) < minSigningKeyLen {
		slog.Error("environment variable is too short for a signing key", "key", key, "min_length", minSigningKeyLen)
		os.Exit(1)
	}

	return []byte(v)
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

// envURL reads an absolute http(s) URL and drops any trailing slash.
func envURL(key, fallback string) string {
	v := envOr(key, fallback)

	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		slog.Error("invalid URL environment variable", "key", key, "value", v)
		os.Exit(1)
	}

	return strings.TrimSuffix(v, "/")
}
