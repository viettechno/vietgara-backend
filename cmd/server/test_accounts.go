package main

import (
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/testaccounts"
)

// testAccountsFromEnv reads the automated suite's test accounts on a shared
// deployment (staging): TEST_ACCOUNTS_EMAIL_DOMAIN selects them, and their
// e-mail goes to the SMTP server at TEST_ACCOUNTS_SMTP_HOST (a Mailpit the
// suite reads OTPs from) instead of the configured provider. It returns the
// zero domain and mailer unchanged when the domain is not set, the normal
// case.
func testAccountsFromEnv(mailer mail.Sender, from mail.From) (testaccounts.Domain, mail.Sender, error) {
	raw := envOr("TEST_ACCOUNTS_EMAIL_DOMAIN", "")
	if raw == "" {
		return testaccounts.Domain{}, mailer, nil
	}

	domain, err := testaccounts.Parse(raw)
	if err != nil {
		return testaccounts.Domain{}, nil, err
	}

	host := envOr("TEST_ACCOUNTS_SMTP_HOST", "")
	if host == "" {
		return testaccounts.Domain{}, nil, errors.New("TEST_ACCOUNTS_SMTP_HOST is required with TEST_ACCOUNTS_EMAIL_DOMAIN")
	}

	testInbox, err := mail.New(mail.Config{Provider: "smtp", From: from, SMTPHost: host, SMTPPort: envInt("TEST_ACCOUNTS_SMTP_PORT", 1025)})
	if err != nil {
		return testaccounts.Domain{}, nil, err
	}

	slog.Info("test accounts enabled: their e-mail goes to the test inbox and they are purged after TEST_ACCOUNTS_RETENTION",
		"domain", domain.String(), "smtp_host", host)

	return domain, mail.Route(domain.Owns, testInbox, mailer), nil
}
