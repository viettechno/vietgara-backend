package main

import (
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

var errTestUsersSMTPHostRequired = errors.New("TEST_USERS_SMTP_HOST is required with TEST_USERS_EMAIL_DOMAIN")

// testUsersFromEnv reads the automated suite's test users on a shared
// deployment (staging): TEST_USERS_EMAIL_DOMAIN selects them, and their
// e-mail goes to the SMTP server at TEST_USERS_SMTP_HOST (a Mailpit the
// suite reads OTPs from) instead of the configured provider. It returns the
// zero domain and mailer unchanged when the domain is not set, the normal
// case.
func testUsersFromEnv(mailer mail.Sender, from mail.From) (testusers.Domain, mail.Sender, error) {
	raw := envOr("TEST_USERS_EMAIL_DOMAIN", "")
	if raw == "" {
		return testusers.Domain{}, mailer, nil
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
