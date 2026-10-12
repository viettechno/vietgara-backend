// Package usecase sends the Notification module's Phase 1 e-mails
// (FR-NOT-04): a registration-confirmation e-mail once a user
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

// UserInfo is the Identity module's view of a user that
// Notification needs to address an e-mail.
type UserInfo struct {
	ID       string
	Email    string
	FullName string
	Locale   string
}

// ErrUserNotFound is returned by UserReader when the user no
// longer exists; treated as a dropped, non-retryable event rather than a
// handler failure (the user may have been deleted since the
// triggering event was published).
var ErrUserNotFound = errors.New("user not found")

// UserReader reads the user an event concerns, when the event
// itself doesn't already carry what's needed to address the e-mail (see
// user.email_verified's payload, which does); implemented by an
// adapter over the Identity module in cmd/server.
type UserReader interface {
	Get(ctx context.Context, userID string) (UserInfo, error)
	// PlatformAdmins returns the platform admins, who are told of payments
	// waiting for review.
	PlatformAdmins(ctx context.Context) ([]UserInfo, error)
}

// Repository records what was sent (domain.Log), in the same transaction
// as the triggering event's processed_events row.
type Repository interface {
	Create(ctx context.Context, log *domain.Log) error
}
