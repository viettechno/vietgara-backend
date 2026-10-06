// Package fixture creates the actors a scenario needs, over the public API
// only. Each actor gets a unique e-mail, so scenarios never collide with
// each other or with an earlier run against the same database.
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
)

// Password is the password of every account the suite registers.
const Password = "Blackbox#2026"

// Actor is a signed-in account.
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

// Mailpit returns the stubbed mailbox. Stubbed target only.
func Mailpit(tb testing.TB) *client.Mailpit {
	tb.Helper()
	env.RequireStubbed(tb, "reads e-mails from Mailpit")

	return client.NewMailpit(env.Get(tb).MailpitURL)
}

// GoogleStub returns the stubbed Google. Stubbed target only.
func GoogleStub(tb testing.TB) *client.GoogleStub {
	tb.Helper()
	env.RequireStubbed(tb, "mints Google ID tokens")

	return client.NewGoogleStub(env.Get(tb).GoogleStubURL)
}

var seq atomic.Int64

// UniqueEmail returns an address no earlier run has used, labelled for
// whoever reads the database or Mailpit afterwards.
func UniqueEmail(label string) string {
	return fmt.Sprintf("bb.%s.%d.%d@vietgara.test", strings.ToLower(label), time.Now().UnixNano(), seq.Add(1))
}

// NewVerifiedOwner registers a fresh account, reads its OTP from Mailpit
// and verifies it: the state every garage scenario starts from. Stubbed
// target only.
func NewVerifiedOwner(tb testing.TB, label string) *Actor {
	tb.Helper()

	mail := Mailpit(tb)
	email := UniqueEmail(label)
	api := Anonymous(tb)

	registered, err := api.Register(tb.Context(), &identityv1.RegisterRequest{
		Email: email, Password: Password, FullName: "Blackbox " + label,
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

// Owner returns a verified owner on any target: a fresh one on the stubbed
// stack, or the pre-provisioned BLACKBOX_OWNER_EMAIL account elsewhere
// (skipping t when it is not configured). Scenarios that use it must not
// change shared state they cannot undo.
func Owner(tb testing.TB) *Actor {
	tb.Helper()

	cfg := env.Get(tb)
	if cfg.Target == env.Stubbed {
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
