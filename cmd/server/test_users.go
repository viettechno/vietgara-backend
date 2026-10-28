package main

import (
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

var (
	errTestUsersSMTPHostRequired = errors.New("TEST_USERS_SMTP_HOST is required with TEST_USERS_EMAIL_DOMAIN")
	errTestUsersInProduction     = errors.New("TEST_USERS_EMAIL_DOMAIN must not be set when APP_ENV is production")
)

// testUsersFromEnv reads the automated suite's test users on a shared
// deployment (staging): TEST_USERS_EMAIL_DOMAIN selects them, and their
// e-mail goes to the SMTP server at TEST_USERS_SMTP_HOST (a Mailpit the
// suite reads OTPs from) instead of the configured provider. It returns the
// zero domain and mailer unchanged when the domain is not set, the normal
// case. Production (APP_ENV=production) refuses the setting: test users
// route e-mail to a test inbox and are purged, which production must never
// do, even with a staging .env copied by mistake.
func testUsersFromEnv(mailer mail.Sender, from mail.From) (testusers.Domain, mail.Sender, error) {
	raw := envOr("TEST_USERS_EMAIL_DOMAIN", "")
	if raw == "" {
		return testusers.Domain{}, mailer, nil
	}

	if envOr("APP_ENV", "") == "production" {
		return testusers.Domain{}, nil, errTestUsersInProduction
	}

	domain, err := testusers.Parse(raw)
	if err != nil {
		return testusers.Domain{}, nil, err
	}

	host := envOr("TEST_USERS_SMTP_HOST", "")
	if host == "" {
		return testusers.Domain{}, nil, errTestUsersSMTPHostRequired
	}

	testInbox, err := mail.New(mail.Config{Provider: "smtp", From: from, SMTPHost: host, SMTPPort: envInt("TEST_USERS_SMTP_PORT", 1025)})
	if err != nil {
		return testusers.Domain{}, nil, err
	}

	slog.Info("test users enabled: their e-mail goes to the test inbox and they are purged after TEST_USERS_RETENTION",
		"domain", domain.String(), "smtp_host", host)

	return domain, mail.Route(domain.Owns, testInbox, mailer), nil
}
