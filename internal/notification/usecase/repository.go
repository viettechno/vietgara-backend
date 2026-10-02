// Package usecase sends the Notification module's Phase 1 e-mails
// (FR-NOT-04): a registration-confirmation e-mail once an account
// verifies its address, and a license renewal reminder ahead of a
// subscription's period end. Both are triggered by consuming an event
// already published by another module (Event Design), not by a direct
// API call -- this module has no RPCs of its own.
package usecase

import (
	"context"
	"errors"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// Mailer sends one e-mail.
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// AccountInfo is the Identity module's view of an account that
// Notification needs to address an e-mail.
type AccountInfo struct {
	Email    string
	FullName string
	Locale   string
}

// ErrAccountNotFound is returned by AccountReader when the account no
// longer exists; treated as a dropped, non-retryable event rather than a
// handler failure (the account may have been deleted since the
// triggering event was published).
var ErrAccountNotFound = errors.New("account not found")

// AccountReader reads the account an event concerns, when the event
// itself doesn't already carry what's needed to address the e-mail (see
// account.email_verified's payload, which does); implemented by an
// adapter over the Identity module in cmd/server.
type AccountReader interface {
	Get(ctx context.Context, accountID string) (AccountInfo, error)
}

// Repository records what was sent (domain.Log), in the same transaction
// as the triggering event's processed_events row.
type Repository interface {
	Create(ctx context.Context, log *domain.Log) error
}
