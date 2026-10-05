// Package usecase is the Customer BFF (Release 2.4, HLD Section 6). The
// caller is a VietGara account (created by Google sign-in, FR-CAPP-01)
// matched to garages' customer records by its verified e-mail; it reads
// only those records, their vehicles and the vehicles' history, each in
// the record's own garage scope. It owns no tables.
package usecase

import (
	"cmp"
	"context"
	"errors"
	"slices"

	customerdomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerusecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// CustomerRef identifies one garage's customer record.
type CustomerRef struct {
	ID       string
	GarageID string
}

// GarageInfo is what the Customer App shows of a garage.
type GarageInfo struct {
	Name    string
	Address string
	Phone   string
}

// Accounts reads the signed-in account's verified e-mail (Identity).
type Accounts interface {
	Email(ctx context.Context, accountID string) (string, error)
}

// Customers finds customer records (Customer & Vehicle). FindByEmail
// searches every garage; Get runs inside the record's garage scope.
type Customers interface {
	FindByEmail(ctx context.Context, email string) ([]CustomerRef, error)
	Get(ctx context.Context, garageID, customerID string) (*customerdomain.Customer, error)
}

// Vehicles reads vehicles and their history (Customer & Vehicle), inside
// the garage's scope.
type Vehicles interface {
	ListForCustomer(ctx context.Context, garageID, customerID string) ([]*customerdomain.Vehicle, error)
	Get(ctx context.Context, garageID, vehicleID string) (*customerdomain.Vehicle, error)
	History(ctx context.Context, garageID, vehicleID string) ([]customerusecase.HistoryEntry, error)
}

// Garages reads a garage's public details (Tenant), inside its scope.
type Garages interface {
	Get(ctx context.Context, garageID string) (GarageInfo, error)
}

// Transactor switches the transaction's Row-Level Security scope for fn.
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// Deps are the use case's collaborators.
type Deps struct {
	Accounts  Accounts
	Customers Customers
	Vehicles  Vehicles
	Garages   Garages
	Tx        Transactor
}

// PortalUseCase serves the Customer App.
type PortalUseCase struct {
	Deps
}

// NewPortalUseCase wires the use case.
func NewPortalUseCase(deps Deps) *PortalUseCase {
	return &PortalUseCase{Deps: deps}
}

// MyGarage is a garage where the person is a customer, with that customer
// record's vehicles.
type MyGarage struct {
	GarageID     string
	Garage       GarageInfo
	CustomerID   string
	CustomerName string
	Vehicles     []*customerdomain.Vehicle
}

// ErrVehicleNotFound reports a vehicle that does not exist, or is not on
// one of the person's customer records.
var ErrVehicleNotFound = errors.New("vehicle not found")

// ListMyVehicles returns the person's vehicles grouped by garage, by garage
// name (FR-CAPP-02).
func (uc *PortalUseCase) ListMyVehicles(ctx context.Context, accountID string) ([]MyGarage, error) {
	refs, err := uc.myRecords(ctx, accountID)
	if err != nil {
		return nil, err
	}

	result := make([]MyGarage, 0, len(refs))

	for _, ref := range refs {
		var mine MyGarage

		err := uc.Tx.WithTransaction(ctx, scope(ref.GarageID, accountID), func(ctx context.Context) error {
			garage, err := uc.Garages.Get(ctx, ref.GarageID)
			if err != nil {
				return err
			}

			customer, err := uc.Customers.Get(ctx, ref.GarageID, ref.ID)
			if err != nil {
				return err
			}

			vehicles, err := uc.Vehicles.ListForCustomer(ctx, ref.GarageID, ref.ID)
			if err != nil {
				return err
			}

			mine = MyGarage{
				GarageID: ref.GarageID, Garage: garage,
				CustomerID: customer.ID, CustomerName: customer.FullName, Vehicles: vehicles,
			}

			return nil
		})
		if err != nil {
			return nil, apperror.Internal("list the customer's vehicles", err)
		}

		result = append(result, mine)
	}

	slices.SortStableFunc(result, func(a, b MyGarage) int {
		return cmp.Or(cmp.Compare(a.Garage.Name, b.Garage.Name), cmp.Compare(a.CustomerName, b.CustomerName))
	})

	return result, nil
}

// GetMyVehicleHistory returns a vehicle's quotes, repair orders and
// settlements, newest first (FR-CAPP-02, FR-CUS-03) -- only for a vehicle
// on one of the person's customer records in that garage.
func (uc *PortalUseCase) GetMyVehicleHistory(
	ctx context.Context, accountID, garageID, vehicleID string,
) ([]customerusecase.HistoryEntry, error) {
	refs, err := uc.myRecords(ctx, accountID)
	if err != nil {
		return nil, err
	}

	mine := map[string]bool{}

	for _, ref := range refs {
		if ref.GarageID == garageID {
			mine[ref.ID] = true
		}
	}

	if len(mine) == 0 {
		return nil, notFound()
	}

	var history []customerusecase.HistoryEntry

	err = uc.Tx.WithTransaction(ctx, scope(garageID, accountID), func(ctx context.Context) error {
		vehicle, err := uc.Vehicles.Get(ctx, garageID, vehicleID)
		if errors.Is(err, customerdomain.ErrVehicleNotFound) || (err == nil && !mine[vehicle.CustomerID]) {
			return ErrVehicleNotFound
		}

		if err != nil {
			return err
		}

		history, err = uc.Vehicles.History(ctx, garageID, vehicleID)

		return err
	})
	if errors.Is(err, ErrVehicleNotFound) {
		return nil, notFound()
	}

	if err != nil {
		return nil, apperror.Internal("read the vehicle's history", err)
	}

	return history, nil
}

// myRecords returns the customer records carrying the account's e-mail.
func (uc *PortalUseCase) myRecords(ctx context.Context, accountID string) ([]CustomerRef, error) {
	email, err := uc.Accounts.Email(ctx, accountID)
	if err != nil {
		return nil, apperror.Internal("read the account's e-mail", err)
	}

	refs, err := uc.Customers.FindByEmail(ctx, email)
	if err != nil {
		return nil, apperror.Internal("find the account's customer records", err)
	}

	return refs, nil
}

func scope(garageID, accountID string) platformdb.Scope {
	return platformdb.Scope{GarageID: garageID, AccountID: accountID}
}

func notFound() error {
	return apperror.New(apperror.CodeNotFound, ErrVehicleNotFound.Error())
}
