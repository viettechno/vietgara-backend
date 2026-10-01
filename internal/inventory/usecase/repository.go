// Package usecase contains the application use cases of the Inventory
// module (FRD 3.7): items and their current stock on hand (FR-INV-03),
// stock receipts from suppliers (FR-INV-01), automatic stock deduction
// when a repair order with part lines closes (FR-INV-02), and the stock
// movement report (FR-INV-05). Every method expects to run inside a
// garage-scoped transaction (see middleware.Garage) so Row-Level Security
// applies. See: Coding_Standards.docx Section 2.3, HLD Section 4.
package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// ListFilter selects one page of a garage's items.
type ListFilter struct {
	// Query matches SKU or name.
	Query  string
	Limit  int
	Offset int
}

// TransactionFilter selects one page of a garage's stock transactions
// (FR-INV-05).
type TransactionFilter struct {
	ItemID string
	Type   domain.TransactionType
	Limit  int
	Offset int
}

// Repository defines the persistence contract for items and their stock
// movements. Every method only sees live (not soft-deleted) items of the
// given garage.
type Repository interface {
	CreateItem(ctx context.Context, item *domain.Item) error
	// FindItemByID returns the item with its current quantity on hand, or
	// domain.ErrItemNotFound.
	FindItemByID(ctx context.Context, garageID, itemID string) (*domain.Item, error)
	UpdateItem(ctx context.Context, item *domain.Item) error
	// ListItems returns one page of items with their quantity on hand,
	// newest first.
	ListItems(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Item, int, error)
	SoftDeleteItem(ctx context.Context, garageID, itemID, deletedBy string) error

	// RecordTransaction inserts tx and atomically applies its effect to
	// stock_balance (+quantity for TransactionIn, -quantity for
	// TransactionOut) in the same statement group. A repair-order issue
	// already recorded for (garage, item, repair order) returns
	// domain.ErrAlreadyIssued instead of a duplicate row.
	RecordTransaction(ctx context.Context, tx *domain.StockTransaction) error
	// ListTransactions returns one page of transactions (FR-INV-05),
	// newest first.
	ListTransactions(ctx context.Context, garageID string, filter TransactionFilter) ([]*domain.StockTransaction, int, error)
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, accountID string, payload any) error
}
