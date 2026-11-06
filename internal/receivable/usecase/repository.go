// Package usecase contains the application use cases of the Receivables
// module (HLD module #9): the receivables fed by the events of Billing &
// Settlement, the scheduled payment reminders, the reconciliation and the
// settings (FR-REC-01, 03, 04). Every method that touches a garage's rows
// expects to run inside a garage-scoped transaction (see middleware.Garage)
// so row-level security applies.
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

// ListFilter selects one page of a garage's receivables.
type ListFilter struct {
	CustomerID string
	// OverdueOnly keeps the open receivables past their due date.
	OverdueOnly bool
	// IncludePaid also lists the paid ones.
	IncludePaid bool
	// Query matches the settlement number, the customer name or the plate.
	Query string
	// Today is the calendar day in the garage's timezone that overdue is
	// measured against.
	Today  time.Time
	Limit  int
	Offset int
}

// Summary totals the open receivables a list matched, across all its pages.
type Summary struct {
	OpenCount         int
	OutstandingAmount int64
	OverdueCount      int
	OverdueAmount     int64
}

// MissingSettlement is a settlement that has no receivable.
type MissingSettlement struct {
	GarageID     string
	SettlementID string
}

// LedgerLine is one ledger entry with the customer it belongs to.
type LedgerLine struct {
	Entry        domain.Entry
	CustomerName string
}

// Repository defines the persistence contract of the module. The tables are
// garage-scoped by row-level security.
type Repository interface {
	// GetSettings returns the garage's settings, the defaults when it has
	// none.
	GetSettings(ctx context.Context, garageID string) (domain.Settings, error)
	// SaveSettings inserts or replaces the garage's settings.
	SaveSettings(ctx context.Context, garageID string, s domain.Settings) error
	// AnonymizeCustomer replaces the customer name and the plate copied into
	// the customer's receivables.
	AnonymizeCustomer(ctx context.Context, garageID, customerID string) error

	// FindBySettlement returns the receivable of a settlement without its
	// ledger, or domain.ErrReceivableNotFound.
	FindBySettlement(ctx context.Context, garageID, settlementID string) (*domain.Receivable, error)
	// FindByID returns the receivable with its ledger and reminders, or
	// domain.ErrReceivableNotFound.
	FindByID(ctx context.Context, garageID, receivableID string) (*domain.Receivable, error)
	// Save inserts the receivable, or updates the amounts, status and
	// customer of the one of the same settlement. It fills ID and times.
	Save(ctx context.Context, r *domain.Receivable) error
	// AddEntries inserts the ledger entries that are not there yet, by
	// source key.
	AddEntries(ctx context.Context, garageID, receivableID string, entries []domain.Entry) error
	// List returns one page, most overdue first, and the summary of every
	// open receivable the filter matches.
	List(ctx context.Context, garageID string, f ListFilter) ([]*domain.Receivable, int, Summary, error)

	// ReminderGarages returns the garages with an open receivable that has a
	// customer, across all garages.
	ReminderGarages(ctx context.Context) ([]string, error)
	// OpenWithCustomer returns the garage's open receivables that have a
	// customer and a due date on or before dueOnOrBefore, with their reminders.
	OpenWithCustomer(ctx context.Context, garageID string, dueOnOrBefore time.Time) ([]*domain.Receivable, error)
	// MissingSettlements returns, across all garages, up to limit settlements
	// that are not paid and have no receivable yet: the ones issued before the
	// module existed, or whose event was lost.
	MissingSettlements(ctx context.Context, limit int) ([]MissingSettlement, error)
	// LockSettlement takes a transaction-scoped advisory lock on the
	// settlement, waiting for it, so that two syncs of one settlement run one
	// after the other.
	LockSettlement(ctx context.Context, settlementID string) error
	// TryLock takes a transaction-scoped advisory lock on key, reporting
	// whether it was free.
	TryLock(ctx context.Context, key string) (bool, error)
	// SaveReminder inserts the reminder or updates the one of the same
	// receivable and offset.
	SaveReminder(ctx context.Context, garageID, receivableID string, r *domain.Reminder) error
	// MarkReminded counts one reminder sent at the given time.
	MarkReminded(ctx context.Context, garageID, receivableID string, at time.Time) error

	// BalanceBefore is the ledger's balance of entries before t.
	BalanceBefore(ctx context.Context, garageID string, t time.Time) (int64, error)
	// Entries returns the ledger entries in [from, to), oldest first.
	Entries(ctx context.Context, garageID string, from, to time.Time) ([]LedgerLine, error)
	// LedgerBalance is the balance of the whole ledger and RemainingTotal the
	// sum of the receivables' remaining amounts.
	LedgerBalance(ctx context.Context, garageID string) (int64, error)
	RemainingTotal(ctx context.Context, garageID string) (int64, error)
}

// PaymentView is one payment of a settlement as the module reads it.
type PaymentView struct {
	ID            string
	VoucherNumber string
	Amount        int64
	Method        string
	CreatedAt     time.Time
	VoidedAt      *time.Time
}

// SettlementView is a settlement as Receivables reads it from Billing &
// Settlement, with its customer resolved through the repair order's vehicle.
type SettlementView struct {
	ID                  string
	Number              string
	CustomerID          string
	CustomerName        string
	VehicleLicensePlate string
	// CustomerAnonymized is true when the customer's personal data was erased
	// (PD-06): the receivable then holds placeholders, not the settlement's
	// name and plate.
	CustomerAnonymized bool
	TotalAmount        int64
	PaidAmount         int64
	CreatedAt          time.Time
	Payments           []PaymentView
}

// SettlementReader reads a settlement; implemented by an adapter over the
// Billing & Settlement use case in cmd/server.
type SettlementReader interface {
	Get(ctx context.Context, garageID, settlementID string) (SettlementView, error)
}

// GarageReader gives the garage's timezone; implemented over the Tenant
// module.
type GarageReader interface {
	Timezone(ctx context.Context, garageID string) (string, error)
}

// Reminder is what a payment reminder says.
type Reminder struct {
	GarageID         string
	CustomerID       string
	ReceivableID     string
	SettlementNumber string
	Remaining        int64
	DueDate          time.Time
	DaysOverdue      int
}

// Delivery is what became of a reminder: sent, or skipped with a reason.
type Delivery struct {
	Sent   bool
	Reason string
}

// ReminderSender sends a payment reminder through the Notification module,
// which applies the garage's switch, the customer's opt-out and the
// suppression list. A returned error is a failed send.
type ReminderSender interface {
	SendPaymentReminder(ctx context.Context, r Reminder) (Delivery, error)
}

// PaidOutReader totals the payment-out vouchers issued in [from, to), net of
// voids, for the reconciliation.
type PaidOutReader interface {
	PaidOut(ctx context.Context, garageID string, from, to time.Time) (int64, error)
}

// Transactor runs fn in a transaction scoped to a garage.
type Transactor interface {
	InGarage(ctx context.Context, garageID string, fn func(ctx context.Context) error) error
}

// AuditLogger records audited changes in the caller's transaction (Security
// Architecture Section 7).
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// clock is a source of the current time, replaced in tests.
type clock func() time.Time
