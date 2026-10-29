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
	// Failed records a failed attempt in a transaction of its own: the
	// attempt's own transaction rolls back with the failure, and the log
	// must show it.
	Failed(ctx context.Context, log *domain.Log) error
	// List returns a garage's rows, newest first, and how many match.
	List(ctx context.Context, garageID string, filter LogFilter, page, pageSize int) ([]domain.Log, int, error)
}

// LogFilter narrows ListNotificationLog; empty fields match everything.
type LogFilter struct {
	Type   domain.Type
	Status domain.Status
}

// SettingsRepository keeps a garage's e-mail choices.
type SettingsRepository interface {
	// Get returns the garage's settings, the defaults when it has none.
	Get(ctx context.Context, garageID string) (domain.Settings, error)
	Save(ctx context.Context, garageID string, settings domain.Settings) error
}

// Suppressions holds the addresses that bounced for good or complained.
type Suppressions interface {
	Suppressed(ctx context.Context, email string) (bool, error)
	Suppress(ctx context.Context, email, reason string) error
}

// CustomerInfo is the Customer module's view of the person an e-mail is for.
type CustomerInfo struct {
	ID    string
	Name  string
	Email string
	// Locale is the language to write in: the linked user's, else the
	// customer's own.
	Locale   string
	OptedOut bool
}

// ErrCustomerNotFound is returned by CustomerReader when the customer no
// longer exists; the event is dropped.
var ErrCustomerNotFound = errors.New("customer not found")

// CustomerReader reads customers; implemented over the Customer module in
// cmd/server.
type CustomerReader interface {
	Get(ctx context.Context, garageID, customerID string) (CustomerInfo, error)
	ByVehicle(ctx context.Context, garageID, vehicleID string) (CustomerInfo, error)
	// OptOut records that the customer opted out of non-essential e-mails,
	// in the consent log; it does nothing when already opted out.
	OptOut(ctx context.Context, garageID, customerID string) error
}

// GarageInfo is what an e-mail says about the garage.
type GarageInfo struct {
	Name    string
	Email   string
	Phone   string
	Address string
}

// ErrGarageNotFound is returned by GarageReader when the garage no longer
// exists; the event is dropped.
var ErrGarageNotFound = errors.New("garage not found")

// GarageReader reads a garage; implemented over the Tenant module.
type GarageReader interface {
	Get(ctx context.Context, garageID string) (GarageInfo, error)
}

// StaffReader finds the staff an e-mail is for; implemented over the Tenant
// module.
type StaffReader interface {
	// Recipients returns the garage's owner, the given users who still work
	// at the garage and, when permission is set, the staff whose role holds
	// it, each once.
	Recipients(ctx context.Context, garageID string, userIDs []string, permission string) ([]UserInfo, error)
}

// Receipts renders the receipt PDF of a paid settlement; implemented over
// the Document module.
type Receipts interface {
	SettlementReceipt(ctx context.Context, garageID, settlementID string) (name string, pdf []byte, err error)
}

// Transactor runs fn in a transaction scoped to a garage.
type Transactor interface {
	InGarage(ctx context.Context, garageID string, fn func(ctx context.Context) error) error
}
