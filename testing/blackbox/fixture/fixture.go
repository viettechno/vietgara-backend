// Package fixture creates the actors a scenario needs, over the public API
// only. Each actor gets a unique e-mail on the target's test domain, so
// scenarios never collide with each other or with an earlier run against
// the same database, and the target purges them after a day.
package fixture

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	testdata "github.com/viettechno/vietgara-testdata/go"
)

// Password is the password of every user the suite registers.
const Password = "Blackbox#2026"

// Dataset loads the pinned shared test dataset for scenarios that use its
// baseline actors and records.
func Dataset(tb testing.TB) *testdata.Dataset {
	tb.Helper()

	dir := env.Get(tb).TestdataDir
	if dir == "" {
		tb.Fatal("BLACKBOX_TESTDATA_DIR is not set; run tests with make blackbox-test")
	}

	dataset, err := testdata.Load(dir)
	if err != nil {
		tb.Fatalf("load shared test dataset: %v", err)
	}

	return dataset
}

// Actor is a signed-in user.
type Actor struct {
	Email    string
	Password string
	Session  *identityv1.Session
	// API sends the session's access token.
	API *client.Client
}

// Anonymous returns a client with no token.
func Anonymous(tb testing.TB) *client.Client {
	tb.Helper()

	return client.New(env.Get(tb).APIURL)
}

// Mailpit returns the target's test inbox, skipping tb when it has none.
func Mailpit(tb testing.TB) *client.Mailpit {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.MailpitURL == "" {
		tb.Skipf("needs the test inbox: BLACKBOX_MAILPIT_URL is not set for target %s", cfg.Target)
	}

	return client.NewMailpit(cfg.MailpitURL)
}

// GoogleStub returns the stubbed Google. Stubbed target only.
func GoogleStub(tb testing.TB) *client.GoogleStub {
	tb.Helper()
	env.RequireStubbed(tb, "mints Google ID tokens")

	return client.NewGoogleStub(env.Get(tb).GoogleStubURL)
}

var seq atomic.Int64

// UniqueEmail returns an address on the test domain that no earlier run
// has used, labelled for whoever reads the database or Mailpit afterwards.
func UniqueEmail(tb testing.TB, label string) string {
	tb.Helper()

	return fmt.Sprintf("bb.%s.%d.%d@%s", strings.ToLower(label), time.Now().UnixNano(), seq.Add(1), env.Get(tb).EmailDomain)
}

// NewVerifiedOwner registers a fresh user, reads its OTP from the test
// inbox and verifies it: the state every garage scenario starts from.
func NewVerifiedOwner(tb testing.TB, label string) *Actor {
	tb.Helper()

	return newVerifiedOwner(tb, label, identityv1.Locale_LOCALE_UNSPECIFIED)
}

// NewVerifiedOwnerWithLocale is NewVerifiedOwner with an interface
// language. Tenant tests use English so default role and group names are
// deterministic: they are snapshotted in the owner's locale when the
// e-mail is verified and the first garage is created.
func NewVerifiedOwnerWithLocale(tb testing.TB, label string, locale identityv1.Locale) *Actor {
	tb.Helper()

	return newVerifiedOwner(tb, label, locale)
}

func newVerifiedOwner(tb testing.TB, label string, locale identityv1.Locale) *Actor {
	tb.Helper()

	mail := Mailpit(tb)
	email := UniqueEmail(tb, label)
	api := Anonymous(tb)

	registered, err := api.Register(tb.Context(), &identityv1.RegisterRequest{
		Email: email, Password: Password, FullName: "Blackbox " + label, Locale: locale,
	})
	if err != nil {
		tb.Fatalf("register %s: %v", email, err)
	}

	msg, err := mail.WaitForMessage(tb.Context(), email)
	if err != nil {
		tb.Fatalf("verification e-mail: %v", err)
	}

	otp, err := msg.OTP()
	if err != nil {
		tb.Fatal(err)
	}

	verified, err := api.WithToken(registered.GetAccessToken()).VerifyEmail(tb.Context(), otp, registered.GetRefreshToken())
	if err != nil {
		tb.Fatalf("verify %s: %v", email, err)
	}

	return &Actor{Email: email, Password: Password, Session: verified, API: api.WithToken(verified.GetAccessToken())}
}

// Owner returns a verified owner on any target: a fresh one when the
// target has a test inbox, otherwise the pre-provisioned
// BLACKBOX_OWNER_EMAIL user (skipping tb when that is not configured
// either). Scenarios that use it must not change shared state they cannot
// undo, since the user may be shared.
func Owner(tb testing.TB) *Actor {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.MailpitURL != "" {
		return NewVerifiedOwner(tb, "owner")
	}

	if cfg.OwnerEmail == "" || cfg.OwnerPassword == "" {
		tb.Skipf("BLACKBOX_OWNER_EMAIL and BLACKBOX_OWNER_PASSWORD are not set for target %s", cfg.Target)
	}

	api := Anonymous(tb)

	session, err := api.Login(tb.Context(), cfg.OwnerEmail, cfg.OwnerPassword)
	if err != nil {
		tb.Fatalf("sign in %s: %v", cfg.OwnerEmail, err)
	}

	return &Actor{Email: cfg.OwnerEmail, Password: cfg.OwnerPassword, Session: session, API: api.WithToken(session.GetAccessToken())}
}

// RequireAPIError fails t unless err is an API error with status and
// code (the body's error.code; empty to accept any).
func RequireAPIError(tb testing.TB, err error, status int, code string) {
	tb.Helper()

	apiErr := client.AsAPIError(err)
	if apiErr == nil {
		tb.Fatalf("want HTTP %d %s, got %v", status, code, err)

		return
	}

	if apiErr.Status != status || (code != "" && apiErr.Code != code) {
		tb.Fatalf("want HTTP %d %s, got %v", status, code, apiErr)
	}
}
