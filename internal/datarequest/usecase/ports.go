// Package usecase serves the personal-data requests of a garage's customers
// and the public page for people without the app (FR-CUS-05, PD-06).
package usecase

import (
	"context"
	"encoding/json"
	"time"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// Counts is what an anonymization changed.
type Counts struct {
	Customers   int
	Vehicles    int
	TaxProfiles int
}

// CustomerRef identifies one garage's customer record.
type CustomerRef struct {
	ID       string
	GarageID string
}

// Customers is the Customer module as this one needs it; implemented in
// cmd/server.
type Customers interface {
	// Check returns the customer's error when it does not exist in the garage
	// or was anonymized already.
	Check(ctx context.Context, garageID, customerID string) error
	// Export returns what the garage holds about the customer, as JSON.
	Export(ctx context.Context, garageID, customerID string) (json.RawMessage, error)
	// Anonymize erases the customer's personal data.
	Anonymize(ctx context.Context, garageID, customerID, actorID string) (Counts, error)
	// ByEmail returns the customer records, in any garage, held under the
	// address.
	ByEmail(ctx context.Context, email string) ([]CustomerRef, error)
}

// DocumentNumbers are the numbers of the documents a customer has.
type DocumentNumbers struct {
	Quotes          []string `json:"quotes"`
	RepairOrders    []string `json:"repairOrders"`
	Settlements     []string `json:"settlements"`
	PaymentVouchers []string `json:"paymentVouchers"`
}

// Documents lists the documents of a customer; implemented in cmd/server over
// the modules that own them.
type Documents interface {
	Numbers(ctx context.Context, garageID, customerID string) (DocumentNumbers, error)
}

// Names names staff and garages.
type Names interface {
	UserNames(ctx context.Context, ids []string) (map[string]string, error)
	GarageName(ctx context.Context, garageID string) (string, error)
}

// Repository stores the requests of a garage.
type Repository interface {
	Create(ctx context.Context, r *domain.Request) error
	// FindForUpdate returns the request locked, or domain.ErrRequestNotFound.
	FindForUpdate(ctx context.Context, garageID, requestID string) (*domain.Request, error)
	Find(ctx context.Context, garageID, requestID string) (*domain.Request, error)
	// Save persists the status, outcome and completion of a request.
	Save(ctx context.Context, r *domain.Request) error
	List(ctx context.Context, garageID string, f Filter) ([]*domain.Request, int, error)
	// ClearDetails empties the details of every request of the customer.
	ClearDetails(ctx context.Context, garageID, customerID string) error
}

// Filter selects one page of a garage's requests.
type Filter struct {
	CustomerID string
	Type       domain.Type
	Status     domain.Status
	Limit      int
	Offset     int
}

// Confirmations stores the public requests that wait for their link. They
// belong to no garage.
type Confirmations interface {
	CreateConfirmation(ctx context.Context, c *domain.Confirmation) error
	CountByEmail(ctx context.Context, email string, since time.Time) (int, error)
	CountBySource(ctx context.Context, sourceHash string, since time.Time) (int, error)
	// Consume deletes and returns the unexpired confirmation with this token
	// hash, or domain.ErrTokenInvalid.
	Consume(ctx context.Context, tokenHash string, now time.Time) (*domain.Confirmation, error)
	// PurgeExpired deletes the confirmations whose link expired before now.
	PurgeExpired(ctx context.Context, now time.Time) (int, error)
}

// Transactor runs fn in a transaction scoped to a garage.
type Transactor interface {
	InGarage(ctx context.Context, garageID string, fn func(ctx context.Context) error) error
}

// Mailer delivers an e-mail.
type Mailer interface {
	Send(ctx context.Context, msg mail.Message) error
}

// Page is one page of requests.
type Page struct {
	Items    []*domain.Request
	Total    int
	Page     int
	PageSize int
}
