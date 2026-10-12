package main

import (
	"context"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	customerportalHandler "github.com/viettechno/vietgara-backend/internal/customerportal/handler"
	customerportalUsecase "github.com/viettechno/vietgara-backend/internal/customerportal/usecase"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	technicianHandler "github.com/viettechno/vietgara-backend/internal/technician/handler"
	technicianUsecase "github.com/viettechno/vietgara-backend/internal/technician/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// The mobile BFFs (Phase 2, HLD Section 6): the Technician App's (Release
// 2.2) and the Customer App's (Release 2.4). They own no tables; each is
// composed in-process from the modules below.

func buildTechnician(
	tx *platformdb.TxManager, tenant *tenantUsecase.TenantUseCase, repairOrders *repairorderUsecase.RepairOrderUseCase,
) *technicianHandler.Server {
	return technicianHandler.NewServer(technicianUsecase.NewTechnicianUseCase(technicianUsecase.Deps{
		Memberships:  technicianMemberships{tenant: tenant},
		RepairOrders: technicianRepairOrders{uc: repairOrders},
		Tx:           tx,
	}))
}

// technicianMemberships lists the technician's garages with what their role
// allows there.
type technicianMemberships struct {
	tenant *tenantUsecase.TenantUseCase
}

func (m technicianMemberships) ListForUser(ctx context.Context, userID string) ([]technicianUsecase.Membership, error) {
	garages, err := m.tenant.ListGarages(ctx, userID)
	if err != nil {
		return nil, err
	}

	memberships := make([]technicianUsecase.Membership, 0, len(garages))

	for _, g := range garages {
		memberships = append(memberships, technicianUsecase.Membership{
			GarageID:            g.Garage.ID,
			GarageName:          g.Garage.Name,
			Active:              g.Garage.Status == tenantDomain.GarageStatusActive,
			CanReadRepairOrders: g.Access.Owner || g.Access.Has(access.RepairOrderRead),
		})
	}

	return memberships, nil
}

// technicianRepairOrders reads every page of a garage's repair orders with
// a line assigned to the user.
type technicianRepairOrders struct {
	uc *repairorderUsecase.RepairOrderUseCase
}

func (r technicianRepairOrders) ListAssigned(ctx context.Context, garageID, userID string) ([]*repairorderDomain.RepairOrder, error) {
	orders, err := allPages(func(page int) ([]*repairorderDomain.RepairOrder, int, error) {
		result, err := r.uc.List(ctx, repairorderUsecase.ListRepairOrdersRequest{
			PageRequest: repairorderUsecase.PageRequest{Page: page, PageSize: repairorderUsecase.MaxPageSize},
			GarageID:    garageID, TechnicianID: userID,
		})
		if err != nil {
			return nil, 0, err
		}

		return result.Items, result.Total, nil
	})
	if err != nil {
		return nil, err
	}

	// List returns orders without lines, but the work list keeps each
	// order's own lines (FR-TAPP-01): reload them one by one.
	full := make([]*repairorderDomain.RepairOrder, 0, len(orders))
	for _, o := range orders {
		got, err := r.uc.Get(ctx, garageID, o.ID)
		if err != nil {
			return nil, err
		}

		full = append(full, got)
	}

	return full, nil
}

// allPages reads a paginated listing to its end.
func allPages[T any](fetch func(page int) (items []T, total int, err error)) ([]T, error) {
	var all []T

	for page := 1; ; page++ {
		items, total, err := fetch(page)
		if err != nil {
			return nil, err
		}

		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			return all, nil
		}
	}
}

func buildCustomerPortal(
	tx *platformdb.TxManager,
	identity *identityUsecase.IdentityUseCase,
	customers *customerRepository.PostgresCustomerRepository,
	vehicles *customerUsecase.VehicleUseCase,
	garages *tenantRepo.PostgresGarageRepository,
	links *customerUsecase.LinkUseCase,
) *customerportalHandler.Server {
	return customerportalHandler.NewServer(customerportalUsecase.NewPortalUseCase(customerportalUsecase.Deps{
		Users:     portalUsers{identity: identity},
		Customers: portalCustomers{customers: customers},
		Vehicles:  portalVehicles{vehicles: vehicles},
		Garages:   portalGarages{garages: garages},
		Links:     links,
		Tx:        tx,
	}))
}

type portalUsers struct {
	identity *identityUsecase.IdentityUseCase
}

func (a portalUsers) Email(ctx context.Context, userID string) (string, error) {
	user, err := a.identity.GetMe(ctx, userID)
	if err != nil {
		return "", err
	}

	return user.Email, nil
}

type portalCustomers struct {
	customers *customerRepository.PostgresCustomerRepository
}

func (c portalCustomers) FindByEmail(ctx context.Context, email string) ([]customerportalUsecase.CustomerRef, error) {
	found, err := c.customers.FindByEmailAcrossGarages(ctx, email)
	if err != nil {
		return nil, err
	}

	refs := make([]customerportalUsecase.CustomerRef, 0, len(found))
	for _, ref := range found {
		refs = append(refs, customerportalUsecase.CustomerRef{ID: ref.ID, GarageID: ref.GarageID})
	}

	return refs, nil
}

func (c portalCustomers) Get(ctx context.Context, garageID, customerID string) (*customerDomain.Customer, error) {
	return c.customers.FindByID(ctx, garageID, customerID)
}

type portalVehicles struct {
	vehicles *customerUsecase.VehicleUseCase
}

func (v portalVehicles) ListForCustomer(ctx context.Context, garageID, customerID string) ([]*customerDomain.Vehicle, error) {
	return allPages(func(page int) ([]*customerDomain.Vehicle, int, error) {
		result, err := v.vehicles.List(ctx, customerUsecase.ListVehiclesRequest{
			PageRequest: customerUsecase.PageRequest{Page: page, PageSize: customerUsecase.MaxPageSize},
			GarageID:    garageID, CustomerID: customerID,
		})
		if err != nil {
			return nil, 0, err
		}

		return result.Items, result.Total, nil
	})
}

func (v portalVehicles) Get(ctx context.Context, garageID, vehicleID string) (*customerDomain.Vehicle, error) {
	return v.vehicles.Get(ctx, garageID, vehicleID)
}

func (v portalVehicles) History(ctx context.Context, garageID, vehicleID string) ([]customerUsecase.HistoryEntry, error) {
	return v.vehicles.GetHistory(ctx, garageID, vehicleID)
}

type portalGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g portalGarages) Get(ctx context.Context, garageID string) (customerportalUsecase.GarageInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return customerportalUsecase.GarageInfo{}, err
	}

	return customerportalUsecase.GarageInfo{Name: garage.Name, Address: deref(garage.Address), Phone: deref(garage.Phone)}, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
