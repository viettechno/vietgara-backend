// Package domain contains the pure business types of the Receivables module
// (HLD module #9): what customers owe a garage, its payment terms and its
// reminder schedule (FR-REC-01, 03, 04).
package domain

import (
	"errors"
	"slices"
	"time"
)

// Status is the collection state of a receivable.
type Status string

// The statuses.
const (
	StatusOpen Status = "open"
	StatusPaid Status = "paid"
)

// EntryKind is one kind of movement of a receivable's ledger.
type EntryKind string

// The ledger entry kinds.
const (
	// EntryCharge is the settlement being issued.
	EntryCharge EntryKind = "charge"
	// EntryPayment is a payment voucher recorded against it.
	EntryPayment EntryKind = "payment"
	// EntryVoid is a payment voucher voided: the amount is owed again.
	EntryVoid EntryKind = "void"
)

// Outcome is what became of a scheduled reminder.
type Outcome string

// The reminder outcomes.
const (
	OutcomeSent    Outcome = "sent"
	OutcomeSkipped Outcome = "skipped"
	OutcomeFailed  Outcome = "failed"
)

// ChannelEmail is the channel of a reminder; push joins with Release 16.0.
const ChannelEmail = "email"

// Skip reasons of a reminder; the first four are the notification module's.
const (
	ReasonDisabled   = "DISABLED"
	ReasonOptedOut   = "OPTED_OUT"
	ReasonSuppressed = "SUPPRESSED"
	ReasonNoEmail    = "NO_EMAIL"
	// ReasonSuperseded marks an earlier offset that a later one replaced
	// because both came due together.
	ReasonSuperseded = "SUPERSEDED"
)

// MaxSendAttempts is how many times a day's reminder is tried before it is
// left as failed.
const MaxSendAttempts = 3

// Entry is one line of a receivable's ledger.
type Entry struct {
	ID   string
	Kind EntryKind
	// Amount is positive; the kind gives the sign.
	Amount int64
	// Reference is the settlement number of a charge, the voucher number of
	// a payment or void.
	Reference string
	// Method is how a payment was paid ("cash", "bank_transfer", "online");
	// empty for a charge.
	Method     string
	OccurredAt time.Time
	// SourceKey makes a ledger entry unique per source: "charge:<settlement>",
	// "payment:<payment>" or "void:<payment>".
	SourceKey string
}

// Reminder is one scheduled reminder and its outcome.
type Reminder struct {
	ID         string
	OffsetDays int
	Channel    string
	Outcome    Outcome
	Reason     string
	Attempts   int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// The texts that replace the name and the plate of a customer whose personal
// data was erased (PD-06); the name is the Customer module's.
const (
	AnonymizedName  = "Khách hàng đã ẩn danh"
	AnonymizedPlate = "ANON"
)

// Anonymized reports whether the receivable's customer was anonymized.
func (r *Receivable) Anonymized() bool { return r.CustomerName == AnonymizedName }

// Receivable is what a customer still owes for one settlement.
type Receivable struct {
	ID               string
	GarageID         string
	SettlementID     string
	SettlementNumber string
	// CustomerID is empty when the settlement's customer could not be found.
	CustomerID          string
	CustomerName        string
	VehicleLicensePlate string
	TotalAmount         int64
	PaidAmount          int64
	// DueDate is a calendar day in the garage's timezone (time of day zero,
	// UTC location).
	DueDate        time.Time
	Status         Status
	RemindersSent  int
	LastReminderAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Entries        []Entry
	Reminders      []Reminder
}

// Remaining is what is still to be collected.
func (r *Receivable) Remaining() int64 { return max(r.TotalAmount-r.PaidAmount, 0) }

// Derive sets Status from the amounts: paid once everything is collected.
func (r *Receivable) Derive() {
	if r.PaidAmount >= r.TotalAmount {
		r.Status = StatusPaid
	} else {
		r.Status = StatusOpen
	}
}

// DaysOverdue is how many days past the due date today is, 0 when the
// receivable is paid or not yet overdue. today is a calendar day in the
// garage's timezone.
func (r *Receivable) DaysOverdue(today time.Time) int {
	if r.Status != StatusOpen {
		return 0
	}

	days := int(today.Sub(r.DueDate).Hours() / hoursPerDay)

	return max(days, 0)
}

// Overdue reports whether the receivable is open and past its due date.
func (r *Receivable) Overdue(today time.Time) bool { return r.DaysOverdue(today) > 0 }

const hoursPerDay = 24

// DueDateFor is the due date of a settlement created at created in the
// garage's location: that calendar day plus the payment terms.
func DueDateFor(created time.Time, loc *time.Location, termsDays int) time.Time {
	y, m, d := created.In(loc).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, termsDays)
}

// Today is the calendar day of now in the garage's location, as a date.
func Today(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.In(loc).Date()

	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Limits of the settings.
const (
	MaxPaymentTermsDays = 365
	MaxReminderOffsets  = 5
	MaxReminderOffset   = 90
	DefaultTermsDays    = 7
)

// Settings are a garage's payment terms and reminder schedule (FR-REC-03).
type Settings struct {
	PaymentTermsDays int
	RemindersEnabled bool
	// ReminderOffsetDays are the days overdue at which a reminder goes out,
	// ascending.
	ReminderOffsetDays []int
}

// DefaultSettings is what a garage that never saved settings has.
func DefaultSettings() Settings {
	return Settings{PaymentTermsDays: DefaultTermsDays, RemindersEnabled: true, ReminderOffsetDays: []int{3, 7}}
}

// Normalize validates the settings and sorts the offsets.
func (s Settings) Normalize() (Settings, error) {
	if s.PaymentTermsDays < 0 || s.PaymentTermsDays > MaxPaymentTermsDays {
		return Settings{}, ErrInvalidTerms
	}

	offsets := slices.Clone(s.ReminderOffsetDays)
	slices.Sort(offsets)

	// Reminders that are on need a day to remind at: an update that leaves the
	// schedule out must not silently turn them into nothing.
	if s.RemindersEnabled && len(offsets) == 0 {
		return Settings{}, ErrInvalidOffsets
	}

	if len(offsets) > MaxReminderOffsets || len(slices.Compact(slices.Clone(offsets))) != len(offsets) {
		return Settings{}, ErrInvalidOffsets
	}

	for _, o := range offsets {
		if o < 1 || o > MaxReminderOffset {
			return Settings{}, ErrInvalidOffsets
		}
	}

	s.ReminderOffsetDays = offsets

	return s, nil
}

// DueOffsets returns the offsets that have been reached by a receivable that
// is daysOverdue days overdue and have no reminder yet, ascending.
func (s Settings) DueOffsets(daysOverdue int, done map[int]bool) []int {
	var due []int

	for _, o := range s.ReminderOffsetDays {
		if o <= daysOverdue && !done[o] {
			due = append(due, o)
		}
	}

	return due
}

// Errors of the module.
var (
	ErrReceivableNotFound = errors.New("receivable not found")
	ErrInvalidTerms       = errors.New("payment terms must be between 0 and 365 days")
	ErrInvalidOffsets     = errors.New("reminders need 1 to 5 distinct days between 1 and 90 to remind at")
	ErrIncompleteSettings = errors.New("payment terms and the reminder switch are required")
	ErrInvalidPeriod      = errors.New("dates must be in the form YYYY-MM-DD and from must be before to")
)

// Reconciliation reconciles a period's receivables with the settlements and
// vouchers behind them (FR-REC-04).
type Reconciliation struct {
	From, To          string
	OpeningBalance    int64
	ChargedAmount     int64
	CollectedAmount   int64
	VoidedAmount      int64
	ClosingBalance    int64
	SettlementCount   int
	PaymentCount      int
	CollectedByMethod []MethodTotal
	PaidOutAmount     int64
	// Matches and Difference compare the ledger's balance today with the
	// receivables' remaining amounts today.
	Matches    bool
	Difference int64
	Lines      []Line
}

// MethodTotal is the amount collected by one payment method.
type MethodTotal struct {
	Method string
	Amount int64
}

// Line is one movement of the period.
type Line struct {
	Date         string
	Kind         EntryKind
	Reference    string
	CustomerName string
	Amount       int64
	Method       string
}

// Balance is the signed effect of an entry on what is owed.
func (e Entry) Balance() int64 {
	if e.Kind == EntryPayment {
		return -e.Amount
	}

	return e.Amount
}

// ParsePeriod parses the half-open period [from, to) of "YYYY-MM-DD" days.
func ParsePeriod(from, to string) (time.Time, time.Time, error) {
	f, err := time.Parse(time.DateOnly, from)
	if err != nil {
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}

	t, err := time.Parse(time.DateOnly, to)
	if err != nil || !f.Before(t) {
		return time.Time{}, time.Time{}, ErrInvalidPeriod
	}

	return f, t, nil
}
