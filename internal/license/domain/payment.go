package domain

import (
	"crypto/rand"
	"errors"
	"math/big"
	"time"
)

// PaymentKind says what a payment request pays for (FR-LIC-07).
type PaymentKind string

// Valid PaymentKind values.
const (
	// PaymentChange pays for another plan.
	PaymentChange PaymentKind = "change"
	// PaymentRenew pays for the current plan for another period.
	PaymentRenew PaymentKind = "renew"
)

// IsValid reports whether k is a defined kind.
func (k PaymentKind) IsValid() bool { return k == PaymentChange || k == PaymentRenew }

// PaymentStatus is the state of a payment request (LLD 3.3).
type PaymentStatus string

// Valid PaymentStatus values.
const (
	// PaymentAwaitingPayment: quoted, with its reference; not declared yet.
	PaymentAwaitingPayment PaymentStatus = "awaiting_payment"
	// PaymentSubmitted: the owner declared the transfer; waiting for review.
	PaymentSubmitted PaymentStatus = "submitted"
	PaymentApproved  PaymentStatus = "approved"
	PaymentRejected  PaymentStatus = "rejected"
	PaymentCancelled PaymentStatus = "cancelled"
)

// Open reports whether the request still waits for the owner or a reviewer.
func (s PaymentStatus) Open() bool { return s == PaymentAwaitingPayment || s == PaymentSubmitted }

// PaymentInstructions are the platform's payment details as shown with a
// request (FR-LIC-10).
type PaymentInstructions struct {
	BankName             string `json:"bank_name"`
	AccountNumber        string `json:"account_number"`
	AccountHolder        string `json:"account_holder"`
	Branch               string `json:"branch"`
	TransferInstructions string `json:"transfer_instructions"`
	// QRFileID is the QR code's file; the URL is signed when read.
	QRFileID string `json:"qr_file_id,omitempty"`
}

// PlanPaymentRequest is an owner's request to switch plan or renew, paid by
// a manual bank transfer (FR-LIC-07).
type PlanPaymentRequest struct {
	ID             string
	UserID         string
	SubscriptionID string
	Kind           PaymentKind
	PlanID         string
	// Plan is loaded alongside the request when reading.
	Plan      *Plan
	Status    PaymentStatus
	Reference string
	// The quote, fixed when the request starts and honoured at approval.
	QuotedAmount  int64
	CreditApplied int64
	AmountDue     int64
	CreditSurplus int64
	// UnusedCredit is the part of the available credit that is the unused
	// value of the period, earned when the plan changes.
	UnusedCredit int64
	PeriodDays   int
	UnusedDays   int
	Instructions PaymentInstructions
	// The owner's declaration, set when submitted.
	PayerName          string
	PayerBank          string
	PayerAccount       string
	TransferredAmount  *int64
	BankTransactionRef string
	PaidAt             *time.Time
	EvidenceFileID     string
	AmountMismatch     bool
	ReviewNote         string
	RejectionReason    string
	ReviewedBy         string
	ReviewedAt         *time.Time
	SubmittedAt        *time.Time
	CreatedAt          time.Time
}

// CreditEntryKind is why the account credit changed (FR-LIC-09).
type CreditEntryKind string

// Valid CreditEntryKind values.
const (
	// CreditEarned: the unused value of a period, or a downgrade's surplus.
	CreditEarned CreditEntryKind = "earned"
	// CreditApplied: credit used by a payment.
	CreditApplied CreditEntryKind = "applied"
	// CreditAdjustment: a correction by a platform admin.
	CreditAdjustment CreditEntryKind = "adjustment"
)

// CreditEntry is one change of an owner's account credit.
type CreditEntry struct {
	ID                   string
	UserID               string
	Kind                 CreditEntryKind
	Amount               int64
	BalanceAfter         int64
	PlanPaymentRequestID string
	Note                 string
	CreatedBy            string
	CreatedAt            time.Time
}

// PaymentSettings are the platform's payment details (FR-LIC-10).
type PaymentSettings struct {
	BankName             string
	AccountNumber        string
	AccountHolder        string
	Branch               string
	TransferInstructions string
	QRFileID             string
	UpdatedAt            time.Time
}

// Instructions are the details shown with a request.
func (s PaymentSettings) Instructions() PaymentInstructions {
	return PaymentInstructions{
		BankName: s.BankName, AccountNumber: s.AccountNumber, AccountHolder: s.AccountHolder, Branch: s.Branch,
		TransferInstructions: s.TransferInstructions, QRFileID: s.QRFileID,
	}
}

// Quote is what a payment request costs (LLD 3.3).
type Quote struct {
	QuotedAmount  int64
	CreditApplied int64
	AmountDue     int64
	CreditSurplus int64
	PeriodDays    int
	UnusedDays    int
	// UnusedCredit is the value of the unused days of the current period,
	// turned into credit when the plan changes (0 for a renewal).
	UnusedCredit int64
}

const day = 24 * time.Hour

// QuotePayment prices a plan change or a renewal at now (FR-LIC-09):
//
//	periodDays    = days of the current period
//	unusedDays    = whole days left in it (0 once it ended)
//	unusedCredit  = floor(unusedDays / periodDays * period_paid_amount)  // a change only
//	creditApplied = min(unusedCredit + credit_balance, plan.price)
//	amountDue     = plan.price - creditApplied
//	creditSurplus = max(unusedCredit + credit_balance - plan.price, 0)
//
// A renewal does not cut the running period short, so it carries no unused
// credit; only the account balance is applied. The trial paid 0.
func QuotePayment(sub *Subscription, plan *Plan, kind PaymentKind, now time.Time) Quote {
	q := Quote{QuotedAmount: plan.Price}

	periodDays := max(int(sub.CurrentPeriodEnd.Sub(sub.CurrentPeriodStart).Round(day)/day), 1)

	q.PeriodDays = periodDays

	if left := sub.CurrentPeriodEnd.Sub(now); left > 0 {
		q.UnusedDays = min(int(left/day), periodDays)
	}

	if kind == PaymentChange && sub.Status != StatusTrial {
		q.UnusedCredit = int64(q.UnusedDays) * sub.PeriodPaidAmount / int64(periodDays)
	}

	available := q.UnusedCredit + sub.CreditBalance
	q.CreditApplied = min(available, plan.Price)
	q.AmountDue = plan.Price - q.CreditApplied
	q.CreditSurplus = max(available-plan.Price, 0)

	return q
}

// Apply makes the request's plan the subscription's (FR-LIC-08), paid at
// now: a change, or a renewal after the period ended, starts a new period at
// now and reactivates a past-due or suspended subscription; a renewal of a
// running period extends it from its old end. The unused days of a trial are
// lost (the trial paid 0).
func (s *Subscription) Apply(plan *Plan, kind PaymentKind, quotedAmount int64, now time.Time) {
	running := s.Status == StatusActive && s.CurrentPeriodEnd.After(now)

	if kind == PaymentRenew && running {
		s.CurrentPeriodEnd = plan.BillingCycle.PeriodEnd(s.CurrentPeriodEnd)
		s.PeriodPaidAmount += quotedAmount
	} else {
		s.CurrentPeriodStart = now
		s.CurrentPeriodEnd = plan.BillingCycle.PeriodEnd(now)
		s.PeriodPaidAmount = quotedAmount
	}

	s.PlanID, s.Plan = plan.ID, plan
	s.Status = StatusActive
	s.TrialEndAt = nil
	s.GracePeriodEnd = nil
}

// referenceAlphabet leaves out the characters people mistake for each other.
const referenceAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewReference returns a transfer reference such as "VG-7K2M9Q".
func NewReference() string {
	buf := make([]byte, 6)

	for i := range buf {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(referenceAlphabet)))) // crypto/rand never fails (Go 1.24+)
		buf[i] = referenceAlphabet[n.Int64()]
	}

	return "VG-" + string(buf)
}

// Errors of payment requests; their messages reach API responses.
var (
	ErrPaymentRequestNotFound = errors.New("payment request not found")
	ErrRequestAlreadyOpen     = errors.New("a payment request is already waiting for review")
	ErrTrialNotAvailable      = errors.New("the trial can only be used once, when the account is created")
	ErrEvidenceRequired       = errors.New("the evidence of the transfer is required")
	ErrRequestNotOpen         = errors.New("the payment request is not open")
	ErrMismatchNoteRequired   = errors.New("the transferred amount differs from the amount due: a note is required")
	ErrInvalidDeclaration     = errors.New("the transfer declaration is incomplete")
	ErrInvalidAdjustment      = errors.New("the credit adjustment needs a non-zero amount and a note")
	ErrReferenceTaken         = errors.New("the transfer reference is taken")
	ErrInvalidEvidence        = errors.New("the evidence must be a PNG, JPEG, WebP or PDF file of at most 5 MB")
	ErrInvalidQR              = errors.New("the QR code must be a PNG, JPEG or WebP image of at most 1 MB")
)

// Reasons of the errors above.
const (
	ReasonTrialNotAvailable    = "TRIAL_NOT_AVAILABLE"
	ReasonRequestAlreadyOpen   = "REQUEST_ALREADY_OPEN"
	ReasonEvidenceRequired     = "EVIDENCE_REQUIRED"
	ReasonRequestNotOpen       = "REQUEST_NOT_OPEN"
	ReasonMismatchNoteRequired = "AMOUNT_MISMATCH_NOTE_REQUIRED"
)
