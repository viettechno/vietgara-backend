package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
)

// ListFilter selects one page of a garage's suppliers.
type ListFilter struct {
	// Query matches name, tax code, contact person, phone or email.
	Query  string
	Limit  int
	Offset int
}

// Repository defines the persistence contract for suppliers. Every method
// is scoped to one garage and ignores soft-deleted rows.
type Repository interface {
	// Create inserts s; a duplicate name returns domain.ErrSupplierNameTaken.
	Create(ctx context.Context, s *domain.Supplier) error
	// FindByID returns domain.ErrSupplierNotFound when absent.
	FindByID(ctx context.Context, garageID, supplierID string) (*domain.Supplier, error)
	// Update persists the editable fields of s; a duplicate name returns
	// domain.ErrSupplierNameTaken.
	Update(ctx context.Context, s *domain.Supplier) error
	// List returns one page of suppliers ordered by name, and the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Supplier, int, error)
	// SoftDelete returns domain.ErrSupplierNotFound when absent.
	SoftDelete(ctx context.Context, garageID, supplierID, deletedBy string) error
}
