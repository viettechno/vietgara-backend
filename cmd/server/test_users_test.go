package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

func TestTestUsersFromEnv(t *testing.T) {
	from := mail.From{Email: "no-reply@vietgara.test", Name: "VietGara"}

	t.Run("not set: the configured mailer, no test users", func(t *testing.T) {
		t.Setenv("TEST_USERS_EMAIL_DOMAIN", "")
		t.Setenv("APP_ENV", "production")

		domain, sender, err := testUsersFromEnv(nil, from)
		require.NoError(t, err)
		assert.Empty(t, domain.String())
		assert.Nil(t, sender)
	})

	t.Run("set on staging: routed to the test inbox", func(t *testing.T) {
		t.Setenv("TEST_USERS_EMAIL_DOMAIN", "blackbox.vietgara.test")
		t.Setenv("TEST_USERS_SMTP_HOST", "vietgara-mailpit")
		t.Setenv("APP_ENV", "staging")

		domain, sender, err := testUsersFromEnv(nil, from)
		require.NoError(t, err)
		assert.Equal(t, "blackbox.vietgara.test", domain.String())
		assert.NotNil(t, sender)
	})

	t.Run("set in production: refused", func(t *testing.T) {
		t.Setenv("TEST_USERS_EMAIL_DOMAIN", "blackbox.vietgara.test")
		t.Setenv("TEST_USERS_SMTP_HOST", "vietgara-mailpit")
		t.Setenv("APP_ENV", "production")

		_, _, err := testUsersFromEnv(nil, from)
		require.ErrorIs(t, err, errTestUsersInProduction)
	})
}
