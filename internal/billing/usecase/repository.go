// Package usecase contains the application use cases of the Billing &
// Settlement module: creating a settlement from a closed repair order
// (FR-BIL-01), allocating it between the customer and an insurer
// (FR-BIL-02), and recording payments as they are actually collected
// (FR-BIL-03). ListSettlements also serves FR-REC-01's receivables view
// (outstanding_only). Every method expects to run inside a garage-scoped
// transaction (see middleware.Garage) so Row-Level Security applies.
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/billing/domain"

	"github.com/viettechno/vietgara-backend/internal/platform/audit"
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
	// SaveAllocation persists the customer/insurer split (FR-BIL-02).
	SaveAllocation(ctx context.Context, s *domain.Settlement) error
	// List returns one page of settlements (without payments), newest
	// first, and the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Settlement, int, error)
	// RecordPayment inserts payment against settlementID and atomically
	// applies it to the settlement's running paid total, returning the
	// settlement's fresh amounts (not its payments list) so the caller
	// can derive and persist the new status.
	RecordPayment(ctx context.Context, garageID, settlementID string, payment *domain.Payment) (*domain.Settlement, error)
	// SaveStatus persists a settlement's status alone, after RecordPayment.
	SaveStatus(ctx context.Context, garageID, settlementID string, status domain.SettlementStatus) error
}

// RepairOrderInfo is the Repair Order module's view of a closed repair
// order that Billing & Settlement needs to create a settlement from.
type RepairOrderInfo struct {
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

// PartnerReader validates and names an insurer partner; implemented by an
// adapter over the Customer module in cmd/server.
type PartnerReader interface {
	// FindInsurer returns the partner's display name, or
	// domain.ErrInsurerNotFound when it does not exist, is not visible in
	// the current garage, or is not of type insurer.
	FindInsurer(ctx context.Context, garageID, partnerID string) (name string, err error)
}

// GarageInfo is the Tenant module's view of a garage that Billing &
// Settlement needs: its owner (for License checks) and numbering
// configuration.
type GarageInfo struct {
	OwnerUserID              string
	Timezone                 string
	SettlementNumberTemplate string
}

// GarageReader reads a garage's numbering and owner configuration;
// implemented by an adapter over the Tenant module in cmd/server.
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
