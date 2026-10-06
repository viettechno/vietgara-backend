// Package env selects the deployment the blackbox suite runs against and
// reads its endpoints from the environment. The scenarios are the same on
// every target; only the ones that need a stub (reading OTP e-mails,
// minting Google ID tokens, creating throwaway accounts) are limited to
// the stubbed target with RequireStubbed.
package env

import (
	"fmt"
	"os"
	"sync"
	"testing"
)

// Target is a deployment the suite can run against.
type Target string

// Targets.
const (
	// Stubbed is the local stack in testing/blackbox/compose.yml: the real
	// backend image behind Kong, real Postgres, Redis and Kafka, and stubs
	// for every third-party service (Mailpit for e-mail, google-stub for
	// Google sign-in).
	Stubbed Target = "stubbed"
	// Staging is the shared staging deployment, with real third parties.
	Staging Target = "staging"
)

// Config is the resolved environment.
type Config struct {
	Target Target
	// APIURL is the public entry point (Kong); the API is under /api/v1.
	APIURL string

	// MailpitURL and GoogleStubURL are set on the stubbed target only.
	MailpitURL    string
	GoogleStubURL string
	// GoogleClientID is the audience the stubbed backend accepts mobile
	// Google ID tokens for (GOOGLE_MOBILE_CLIENT_IDS).
	GoogleClientID string

	// OwnerEmail and OwnerPassword sign in a pre-provisioned, verified
	// account on targets where the suite cannot create one (staging).
	OwnerEmail    string
	OwnerPassword string
}

var (
	once    sync.Once
	loaded  Config
	errLoad error
)

// Get returns the configuration, failing t if it is invalid.
func Get(tb testing.TB) Config {
	tb.Helper()

	once.Do(func() { loaded, errLoad = load() })

	if errLoad != nil {
		tb.Fatalf("blackbox: %v", errLoad)
	}

	return loaded
}

func load() (Config, error) {
	cfg := Config{
		Target:         Target(getenv("BLACKBOX_TARGET", string(Stubbed))),
		APIURL:         getenv("BLACKBOX_API_URL", "http://localhost:18000"),
		MailpitURL:     getenv("BLACKBOX_MAILPIT_URL", "http://localhost:18025"),
		GoogleStubURL:  getenv("BLACKBOX_GOOGLE_STUB_URL", "http://localhost:18090"),
		GoogleClientID: getenv("BLACKBOX_GOOGLE_CLIENT_ID", "blackbox-mobile.apps.googleusercontent.com"),
		OwnerEmail:     os.Getenv("BLACKBOX_OWNER_EMAIL"),
		OwnerPassword:  os.Getenv("BLACKBOX_OWNER_PASSWORD"),
	}

	switch cfg.Target {
	case Stubbed:
	case Staging:
		if os.Getenv("BLACKBOX_API_URL") == "" {
			return Config{}, fmt.Errorf("BLACKBOX_API_URL is required for target %q", cfg.Target)
		}

		cfg.MailpitURL, cfg.GoogleStubURL = "", ""
	default:
		return Config{}, fmt.Errorf("unknown BLACKBOX_TARGET %q (want %q or %q)", cfg.Target, Stubbed, Staging)
	}

	return cfg, nil
}

// RequireStubbed skips t unless the suite runs against the stubbed stack.
// Use it for scenarios that read stubbed side channels or create accounts
// that cannot be cleaned up.
func RequireStubbed(tb testing.TB, why string) {
	tb.Helper()

	if cfg := Get(tb); cfg.Target != Stubbed {
		tb.Skipf("stubbed target only (%s); running against %s", why, cfg.Target)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
