package usecase

import (
	"context"
	"time"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/quote/domain"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// ListFilter selects one page of a garage's quotes.
type ListFilter struct {
	CustomerID string
	VehicleID  string
	// Status narrows to one status; "" lists every status.
	Status domain.Status
	Limit  int
	Offset int
}

// Repository defines the persistence contract for quotes. Every method
// only sees live (not soft-deleted) rows of the given garage.
type Repository interface {
	// Create inserts q and its lines.
	Create(ctx context.Context, q *domain.Quote) error
	// FindByID returns the quote with its lines, or domain.ErrQuoteNotFound.
	FindByID(ctx context.Context, garageID, quoteID string) (*domain.Quote, error)
	// ReplaceLines persists q's scalar fields and replaces its whole line
	// set; only called while the quote is a draft (UpdateQuote).
	ReplaceLines(ctx context.Context, q *domain.Quote) error
	// Save persists q's scalar fields and, for each of q.Lines, its
	// decision and decided_at -- without touching line content. Used by
	// every transition that does not edit lines: SendQuote,
	// RecordQuoteDecision, DecidePublicQuote and ConvertQuote.
	Save(ctx context.Context, q *domain.Quote) error
	// List returns one page of quotes (without lines), newest first, and
	// the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Quote, int, error)
	// SoftDelete returns domain.ErrQuoteNotFound, or domain.ErrNotEditable
	// when the quote is no longer a draft.
	SoftDelete(ctx context.Context, garageID, quoteID, deletedBy string) error
	// FindByPublicToken resolves an approval-link token to the garage and
	// quote it belongs to, bypassing the caller's garage scope (see
	// find_quote_by_public_token, migration 000015): the public flow has
	// no garage_id to scope a transaction with until this call returns.
	// domain.ErrPublicTokenInvalid for an unknown or expired token.
	FindByPublicToken(ctx context.Context, tokenHash string) (garageID, quoteID string, err error)
}

// VehicleReader confirms a vehicle belongs to a customer of the garage and
// reports the names shown on the quote; implemented by an adapter over the
// Customer module in cmd/server.
type VehicleReader interface {
	// Find returns the customer's display name and the vehicle's license
	// plate, or domain.ErrCustomerRequired / domain.ErrVehicleRequired
	// when either does not belong to the garage, or the vehicle belongs
	// to a different customer.
	Find(ctx context.Context, garageID, customerID, vehicleID string) (customerName, licensePlate string, err error)
}

// CustomerReader reads a customer's e-mail, used by Send to decide whether
// the LINK-mode approval link can be e-mailed (per the gara owner's
// choice, "at least notify the customer of what should be fixed" when an
// e-mail is on file); implemented by an adapter over the Customer module.
type CustomerReader interface {
	// Email returns the customer's e-mail on file, or nil when they have
	// none.
	Email(ctx context.Context, garageID, customerID string) (*string, error)
}

// GarageInfo is the Tenant module's view of a garage that Quote & Pricing
// needs: its owner (for License checks), timezone (numbering periods) and
// quote configuration (FR-TEN-05).
type GarageInfo struct {
	OwnerUserID               string
	Timezone                  string
	QuoteNumberTemplate       string
	RepairOrderNumberTemplate string
	DefaultQuoteApprovalMode  domain.ApprovalMode
	// Name is the garage's display name, shown on the public quote page.
	Name string
	// Issuer is the legal identity a quote snapshots (FR-TEN-06).
	Issuer issuer.Issuer
}

// GarageReader reads a garage's numbering configuration; implemented by an
// adapter over the Tenant module in cmd/server.
type GarageReader interface {
	Get(ctx context.Context, garageID string) (GarageInfo, error)
}

// TransactionLimiter enforces the plan's daily transaction limit
// (FR-LIC-02); implemented directly by the License module's
// EntitlementUseCase.
type TransactionLimiter interface {
	ConsumeTransaction(ctx context.Context, ownerUserID string) error
}

// Sequencer hands out document sequence numbers (FR-TEN-05); implemented
// by internal/platform/numbering.Sequencer.
type Sequencer interface {
	Next(ctx context.Context, garageID, docType, period string) (int, error)
}

// BillingPartyResolver resolves who a quote is billed to from the
// customer's tax profiles (FR-CUS-07), as the snapshot the quote keeps;
// implemented by an adapter over the Customer module in cmd/server.
type BillingPartyResolver interface {
	// Resolve takes the customer's default profile when profileID is nil
	// (retail when there is none), retail when it is empty, and otherwise
	// the named active profile of the customer.
	Resolve(ctx context.Context, garageID, customerID string, profileID *string) (billingparty.Party, error)
}

// RepairOrderCreator converts an approved quote into a draft repair order
// (FR-QUO-04); implemented by an adapter over the Repair Order module's
// use case in cmd/server (a narrow view, so Quote never imports it).
type RepairOrderCreator interface {
	// Create returns the new repair order's id, or
	// repairorder domain.ErrAlreadyConverted (the caller maps it).
	Create(
		ctx context.Context, garageID, quoteID, vehicleID, customerName, vehiclePlate, number, actorID string,
		billingParty billingparty.Party, lines []domain.Line,
	) (repairOrderID string, err error)
}

// JobPart is one suggested part of a job, ready to become a quote line.
type JobPart struct {
	PartID      string
	Description string
	// UnitID is the packaging unit the amount is typed in; empty for the base
	// unit.
	UnitID string
	// Quantity is the amount typed in UnitID (or the base unit).
	Quantity    string
	UnitPrice   int64
	NonBillable bool
}

// Job is a catalogue job as the quote needs it.
type Job struct {
	ID         string
	Name       string
	LaborPrice int64
	Parts      []JobPart
}

// JobCatalogue reads a job of the garage's catalogue (FR-CAT-05); implemented
// by an adapter over the Catalogue module in cmd/server. It refuses an
// unknown or inactive job, and leaves out suggestions whose part is
// inactive.
type JobCatalogue interface {
	Job(ctx context.Context, garageID, jobID string) (Job, error)
}

// Mailer delivers e-mails (internal/platform/mail).
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// clock is a source of the current time, replaced in tests.
type clock func() time.Time
