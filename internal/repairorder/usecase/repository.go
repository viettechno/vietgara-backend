// Package usecase contains the application use cases of the Repair Order
// module: creating a repair order, either from an approved quote
// (CreateFromQuote, called by the Quote module through a narrow interface,
// see cmd/server) or directly for ad-hoc work (Create, FR-REP-01);
// assigning technicians and updating line progress (FR-REP-02/03); and
// closing a repair order (FR-REP-06), which publishes repair_order.closed
// for Inventory to deduct stock (FR-INV-02). Every method expects to run
// inside a garage-scoped transaction (see middleware.Garage) so Row-Level
// Security applies.
package usecase

import (
	"context"
	"time"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
)

// ListFilter selects one page of a garage's repair orders (the Kanban
// board's data).
type ListFilter struct {
	VehicleID string
	Status    domain.RepairOrderStatus
	// TechnicianID narrows to repair orders with a line assigned to this
	// technician (UI Design Spec 3.3's "Filter: Technician").
	TechnicianID string
	Limit        int
	Offset       int
}

// Repository defines the persistence contract for repair orders. Every
// method only sees live (not soft-deleted) rows of the given garage.
type Repository interface {
	// Create inserts ro and its lines; a quote already converted returns
	// domain.ErrAlreadyConverted (the unique constraint on quote_id).
	Create(ctx context.Context, ro *domain.RepairOrder) error
	// Lock takes the order's row lock until the transaction ends, so changes
	// to one order run one after the other; domain.ErrRepairOrderNotFound
	// when there is none.
	Lock(ctx context.Context, garageID, repairOrderID string) error
	// FindByID returns the repair order with its lines, or
	// domain.ErrRepairOrderNotFound.
	FindByID(ctx context.Context, garageID, repairOrderID string) (*domain.RepairOrder, error)
	// Save persists ro's scalar fields and each line's status and
	// assigned technicians, without touching line content (description,
	// quantity, price).
	Save(ctx context.Context, ro *domain.RepairOrder) error
	// ReplaceLines persists ro like Save and also the content of its lines:
	// lines no longer in ro.Lines are deleted, the others are inserted or
	// updated in place (their ids stay).
	ReplaceLines(ctx context.Context, ro *domain.RepairOrder) error
	// List returns one page of repair orders (without lines), newest
	// first, and the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.RepairOrder, int, error)
	// ListForVehicle returns a vehicle's repair orders, newest first, with
	// no lines (FR-CUS-03 only needs the summary).
	ListForVehicle(ctx context.Context, garageID, vehicleID string) ([]*domain.RepairOrder, error)
	// ExistsForVehicles reports whether any live repair order belongs to
	// one of the vehicles (the delete-or-deactivate rule, FR-CAT-07).
	ExistsForVehicles(ctx context.Context, garageID string, vehicleIDs []string) (bool, error)
	// LiveVehicleIDs returns which of the vehicles a live repair order
	// belongs to.
	LiveVehicleIDs(ctx context.Context, garageID string, vehicleIDs []string) ([]string, error)
}

// VehicleReader confirms a vehicle belongs to the garage and reports the
// names shown on a repair order; implemented by an adapter over the
// Customer module in cmd/server (the same shape as Quote's, kept as its
// own type so the two modules stay decoupled from each other).
type VehicleReader interface {
	// Find returns the vehicle's owning customer's display name and the
	// vehicle's license plate, or domain.ErrVehicleRequired when it does
	// not belong to the garage.
	Find(ctx context.Context, garageID, vehicleID string) (customerName, licensePlate string, err error)
}

// GarageInfo is the Tenant module's view of a garage that Repair Order
// needs: its owner (for License checks) and numbering configuration.
type GarageInfo struct {
	OwnerUserID               string
	Timezone                  string
	RepairOrderNumberTemplate string
	// Issuer is the legal identity a repair order snapshots (FR-TEN-06).
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

// StockIssues is the Repair Order module's view of the Inventory's stock
// issue; implemented by an adapter in cmd/server.
type StockIssues interface {
	// Posted reports whether the repair order's stock issue is posted, so its
	// parts have left stock. An order without an issue is not posted.
	Posted(ctx context.Context, garageID, repairOrderID string) (bool, error)
	// Short reports whether the order's draft stock issue cannot be covered by
	// the stock on hand right now.
	Short(ctx context.Context, garageID, repairOrderID string) (bool, error)
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction. Close publishes repair_order.closed here; the Inventory
// module deducts stock for the part lines (FR-INV-02) as that event's
// consumer, asynchronously -- not through a direct interface from this
// module, so closing a repair order never fails because of an
// inventory-side problem (Event Design).
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// clock is a source of the current time, replaced in tests.
type clock func() time.Time
