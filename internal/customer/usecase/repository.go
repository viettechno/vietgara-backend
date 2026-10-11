package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
)

// ListFilter selects one page of records matching Query.
type ListFilter struct {
	Query  string
	Limit  int
	Offset int
}

// VehicleFilter narrows a vehicle listing to one customer when CustomerID
// is set.
type VehicleFilter struct {
	ListFilter

	CustomerID string
}

// CustomerRepository defines the persistence contract for customers. Every
// method only sees live (not soft-deleted) rows of the given garage.
type CustomerRepository interface {
	// Create inserts c; a duplicate phone returns domain.ErrPhoneTaken.
	Create(ctx context.Context, c *domain.Customer) error
	// FindByID returns domain.ErrCustomerNotFound when absent.
	FindByID(ctx context.Context, garageID, customerID string) (*domain.Customer, error)
	// Update persists the editable fields of c; a duplicate phone returns
	// domain.ErrPhoneTaken.
	Update(ctx context.Context, c *domain.Customer) error
	// List returns one page of customers, newest first, and the total count.
	List(ctx context.Context, garageID string, filter ListFilter) ([]*domain.Customer, int, error)
	// SoftDelete returns domain.ErrCustomerNotFound when absent.
	SoftDelete(ctx context.Context, garageID, customerID, deletedBy string) error
}

// VehicleRepository defines the persistence contract for vehicles. Every
// method only sees live (not soft-deleted) rows of the given garage.
type VehicleRepository interface {
	// Create inserts v; a duplicate plate returns domain.ErrLicensePlateTaken.
	Create(ctx context.Context, v *domain.Vehicle) error
	// FindByID returns domain.ErrVehicleNotFound when absent.
	FindByID(ctx context.Context, garageID, vehicleID string) (*domain.Vehicle, error)
	// Update persists the editable fields of v; a duplicate plate returns
	// domain.ErrLicensePlateTaken.
	Update(ctx context.Context, v *domain.Vehicle) error
	// List returns one page of vehicles, newest first, and the total count.
	List(ctx context.Context, garageID string, filter VehicleFilter) ([]*domain.Vehicle, int, error)
	// SoftDelete returns domain.ErrVehicleNotFound when absent.
	SoftDelete(ctx context.Context, garageID, vehicleID, deletedBy string) error
	// SoftDeleteByCustomer soft-deletes every vehicle of a customer.
	SoftDeleteByCustomer(ctx context.Context, garageID, customerID, deletedBy string) error
}
