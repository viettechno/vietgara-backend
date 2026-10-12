package iam_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-007 (partial): sign-up validation refuses bad input
// and duplicates without creating a user; nothing is e-mailed. The
// field-level messages are web-E2E.
func TestSignUpValidation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	api := fixture.Anonymous(t)
	mail := fixture.Mailpit(t)

	// Short password: refused, and the address stays usable afterwards,
	// proving no user was created.
	shortEmail := fixture.UniqueEmail(t, "iam007")
	_, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: shortEmail, Password: "Qa@1234", FullName: "Blackbox IAM007",
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
	assertNoMail(t, mail, shortEmail, 5*time.Second)

	completed, err := api.Register(ctx, &identityv1.RegisterRequest{
		Email: shortEmail, Password: fixture.Password, FullName: "Blackbox IAM007",
	})
	require.NoError(t, err)
	assert.False(t, completed.GetUser().GetEmailVerified())

	// Invalid e-mail: refused.
	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: "signup.iam007@vietgara", Password: fixture.Password, FullName: "Blackbox IAM007",
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")

	// Taken e-mail in other letter case: refused, e-mails compare
	// ignoring case.
	taken := fixture.NewVerifiedOwner(t, "taken")
	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: strings.ToUpper(taken.Email), Password: fixture.Password, FullName: "Blackbox IAM007",
	})
	fixture.RequireAPIError(t, err, http.StatusConflict, "EMAIL_TAKEN")

	// Taken phone: refused.
	phone := "0900000907"
	phoneOwner := fixture.UniqueEmail(t, "phone-owner")
	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: phoneOwner, Password: fixture.Password, FullName: "Blackbox Phone", Phone: &phone,
	})
	require.NoError(t, err)

	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: fixture.UniqueEmail(t, "iam007"), Password: fixture.Password,
		FullName: "Blackbox IAM007", Phone: &phone,
	})
	fixture.RequireAPIError(t, err, http.StatusConflict, "PHONE_TAKEN")

	// Empty full name: refused.
	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: fixture.UniqueEmail(t, "iam007"), Password: fixture.Password, FullName: "   ",
	})
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

// assertNoMail fails t unless no e-mail reaches address within wait: the
// bounded absence proof for flows that must stay silent.
func assertNoMail(t *testing.T, mail *client.Mailpit, address string, wait time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), wait)
	defer cancel()

	_, err := mail.WaitForMessage(ctx, address)
	if err == nil {
		t.Fatalf("unexpected e-mail to %s", address)
	}

	assert.ErrorIs(t, err, client.ErrNoMessage)
}
