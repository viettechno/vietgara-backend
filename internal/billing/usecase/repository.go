// Package usecase contains the application use cases of the Billing &
// Settlement module: creating a settlement from a closed repair order
// (FR-BIL-01) and recording payments as they are actually collected
// (FR-BIL-03). ListSettlements also serves FR-REC-01's receivables view
// (outstanding_only). Every method expects to run inside a garage-scoped
// transaction (see middleware.Garage) so Row-Level Security applies.
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// ListFilter selects one page of a garage's settlements.
type ListFilter struct {
	Status domain.SettlementStatus
	// OutstandingOnly lists settlements not yet fully paid (pending or
	// partially_paid) -- FR-REC-01's receivables view. Ignored when
	// Status is set.
	OutstandingOnly bool
	Limit           int
	Offset          int
}

// Repository defines the persistence contract for settlements and their
// payments.
type Repository interface {
	// Create inserts s. ErrAlreadySettled if the repair order already has
	// a settlement (the unique constraint on repair_order_id).
	Create(ctx context.Context, s *domain.Settlement) error
	// FindByID returns the settlement with its payments, or
	// domain.ErrSettlementNotFound.
	FindByID(ctx context.Context, garageID, settlementID string) (*domain.Settlement, error)
	// FindByRepairOrderID returns the repair order's settlement, if any,
	// or domain.ErrSettlementNotFound.
	FindByRepairOrderID(ctx context.Context, garageID, repairOrderID string) (*domain.Settlement, error)
	// List returns one page of settlements (without payments), newest
	// first, and the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Settlement, int, error)
	// RecordPayment inserts payment against settlementID and atomically
	// applies it to the settlement's running paid total, returning the
	// settlement's fresh amounts (not its payments list) so the caller
	// can derive and persist the new status.
	RecordPayment(ctx context.Context, garageID, settlementID string, payment *domain.Payment) (*domain.Settlement, error)
	// VoidPayment marks the payment void with its reason, takes its amount
	// off the settlement's paid total and returns the settlement's fresh
	// amounts (not its payments list). domain.ErrPaymentNotFound when the
	// settlement has no such payment, domain.ErrPaymentAlreadyVoided when
	// it is already void.
	VoidPayment(ctx context.Context, garageID, settlementID, paymentID, reason string) (*domain.Settlement, error)
	// SaveBilling persists who the settlement is billed to and whether an
	// invoice was requested.
	SaveBilling(ctx context.Context, s *domain.Settlement) error
	// SaveStatus persists a settlement's status alone, after RecordPayment.
	SaveStatus(ctx context.Context, garageID, settlementID string, status domain.SettlementStatus) error
}

// RepairOrderInfo is the Repair Order module's view of a closed repair
// order that Billing & Settlement needs to create a settlement from.
type RepairOrderInfo struct {
	// VehicleID is the vehicle the billing party is resolved through.
	VehicleID string
	// BillingParty is who the repair order was billed to, which the
	// settlement starts from.
	BillingParty        billingparty.Party
	CustomerName        string
	VehicleLicensePlate string
	TotalAmount         int64
	Closed              bool
}

// RepairOrderReader reads a repair order's settlement-relevant data;
// implemented by an adapter over the Repair Order module in cmd/server.
// The Repair Order use case already returns a classified
// *apperror.AppError (e.g. not-found), which toAppError passes through
// unchanged, so the adapter needs no error translation of its own.
type RepairOrderReader interface {
	Get(ctx context.Context, garageID, repairOrderID string) (RepairOrderInfo, error)
}

// BillingPartyResolver resolves who a settlement is billed to from the
// customer's tax profiles (FR-CUS-07), through the vehicle's customer, as
// the snapshot the settlement keeps; implemented by an adapter over the
// Customer module in cmd/server.
type BillingPartyResolver interface {
	// Resolve takes the customer's default profile when profileID is nil
	// (retail when there is none), retail when it is empty, and otherwise
	// the named active profile of the customer.
	Resolve(ctx context.Context, garageID, vehicleID string, profileID *string) (billingparty.Party, error)
}

// GarageInfo is the Tenant module's view of a garage that Billing &
// Settlement needs: its owner (for License checks) and numbering
// configuration.
type GarageInfo struct {
	OwnerUserID              string
	Timezone                 string
	SettlementNumberTemplate string
	// VoucherNumberTemplate numbers payment vouchers (FR-BIL-05).
	VoucherNumberTemplate string
	// Issuer is the legal identity a voucher snapshots when issued.
	Issuer issuer.Issuer
}

// GarageReader reads a garage's numbering and owner configuration;
// implemented by an adapter over the Tenant module in cmd/server.
type GarageReader interface {
	Get(ctx context.Context, garageID string) (GarageInfo, error)
}

// CollectorReader names the staff member who collects a payment, for the
// voucher; implemented by an adapter over the Identity module in cmd/server.
type CollectorReader interface {
	// FullName returns the user's display name.
	FullName(ctx context.Context, userID string) (string, error)
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

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// AuditLogger records audited changes in the caller's transaction
// (Security Architecture Section 7, NFR-SE-07).
type AuditLogger interface {
	Record(ctx context.Context, e audit.Entry) error
}

// clock is a source of the current time, replaced in tests.
type clock func() time.Time
