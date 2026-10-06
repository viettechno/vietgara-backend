// Command google is the stubbed Google for the blackbox suite's stubbed
// target. It serves the ID-token JWKS and a mint endpoint (see package
// idtoken) on ADDR, default :8080. `google healthcheck` probes a running
// instance, for the image's HEALTHCHECK.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/viettechno/vietgara-backend/testing/blackbox/stubs/google/idtoken"
)

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck(addr))
	}

	handler, err := idtoken.NewServer()
	if err != nil {
		slog.Error("google stub", "err", err)
		os.Exit(1)
	}

	slog.Info("google stub listening", "addr", addr)

	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("google stub", "err", err)
		os.Exit(1)
	}
}

func healthcheck(addr string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The stub probes its own listener on loopback; addr is its own ADDR.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1"+addr+"/healthz", http.NoBody) //nolint:gosec // G704: fixed loopback host
	if err != nil {
		return 1
	}

	resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: fixed loopback host
	if err != nil {
		return 1
	}

	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 1
	}

	return 0
}
