// Package mail sends transactional e-mails (FR-NOT-04): e-mail
// verification and password-reset OTPs and staff invitations.
//
// Providers:
//   - smtp: any SMTP server; Mailpit locally and in integration tests.
//   - mailjet: Mailjet Send API v3.1, for staging and production.
//
// Messages are rendered from embedded templates in the recipient's
// language (Vietnamese by default, NFR-US-01).
package mail

import (
	"context"
	"errors"
	"fmt"
)

// Message is one e-mail.
type Message struct {
	ToEmail string
	ToName  string
	Subject string
	Text    string
	HTML    string
	// Tag identifies the template (e.g. "email_verification") for the
	// provider's analytics.
	Tag string
}

// Sender delivers a message.
type Sender interface {
	Send(ctx context.Context, msg Message) error
}

// From is the sender address of every message.
type From struct {
	Email string
	Name  string
}

// ErrInvalidMessage is returned for a message without recipient or body.
var ErrInvalidMessage = errors.New("mail: message needs a recipient, subject and body")

var (
	errMailFromAddressRequired = errors.New("mail: MAIL_FROM_ADDRESS is required")
	errSMTPHostAndPortRequired = errors.New("mail: SMTP_HOST and SMTP_PORT are required")
	errMailjetKeysRequired     = errors.New("mail: MAILJET_API_KEY and MAILJET_SECRET_KEY are required")
	errUnknownMailProvider     = errors.New("mail: unknown MAIL_PROVIDER")
)

func validate(msg Message) error {
	if msg.ToEmail == "" || msg.Subject == "" || (msg.Text == "" && msg.HTML == "") {
		return ErrInvalidMessage
	}

	return nil
}

// Config selects and configures a provider.
type Config struct {
	// Provider is "smtp" or "mailjet".
	Provider string
	From     From

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string

	MailjetAPIKey    string
	MailjetSecretKey string
	// MailjetEndpoint overrides the Send API URL (tests).
	MailjetEndpoint string
}

// New returns the Sender selected by cfg.
func New(cfg Config) (Sender, error) {
	if cfg.From.Email == "" {
		return nil, errMailFromAddressRequired
	}

	switch cfg.Provider {
	case "smtp":
		if cfg.SMTPHost == "" || cfg.SMTPPort == 0 {
			return nil, errSMTPHostAndPortRequired
		}

		return NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.From), nil
	case "mailjet":
		if cfg.MailjetAPIKey == "" || cfg.MailjetSecretKey == "" {
			return nil, errMailjetKeysRequired
		}

		return NewMailjetSender(cfg.MailjetAPIKey, cfg.MailjetSecretKey, cfg.From, cfg.MailjetEndpoint), nil
	default:
		return nil, fmt.Errorf("%w %q (want smtp or mailjet)", errUnknownMailProvider, cfg.Provider)
	}
}
