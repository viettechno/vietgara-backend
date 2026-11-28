//go:build !windows

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/viettechno/vietgara-backend/internal/printagent/agent"
)

const serviceSupported = false

var errNoService = errors.New("the service is for Windows: run vietgara-agent run under systemd or launchd instead")

func isService() bool { return false }

func runService() error { return errNoService }

func installService(io.Writer) error { return errNoService }

func uninstallService(io.Writer) error { return errNoService }

func serviceStatus() string { return "—" }

// statePath is $VIETGARA_AGENT_STATE, or the user's configuration directory.
func statePath() string {
	if p := os.Getenv("VIETGARA_AGENT_STATE"); p != "" {
		return p
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}

	return filepath.Join(dir, "vietgara-agent", "state.json")
}

func prepareStateDir(store *agent.Store) error { return store.EnsureDir() }
