package main

import (
	"context"
	"errors"
	"sort"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	identityDomain "github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	licenseDomain "github.com/viettechno/vietgara-backend/internal/license/domain"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	quoteDomain "github.com/viettechno/vietgara-backend/internal/quote/domain"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	quoteUsecase "github.com/viettechno/vietgara-backend/internal/quote/usecase"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

// The adapters below connect modules through narrow interfaces owned by the
// consuming module, so no module imports another (ADR-001: modules stay
// splittable into services).

// accountDirectory gives the Tenant module a read-only view of Identity
// accounts.
type accountDirectory struct {
	accounts *identityRepo.PostgresAccountRepository
}

func (a accountDirectory) FindByEmail(ctx context.Context, email string) (*tenantUsecase.AccountSummary, error) {
	normalized, ok := identityDomain.NormalizeEmail(email)
	if !ok {
		return nil, tenantDomain.ErrAccountNotFound
	}

	return summaryOrNotFound(a.accounts.FindByEmail(ctx, normalized))
}

func (a accountDirectory) FindByPhone(ctx context.Context, phone string) (*tenantUsecase.AccountSummary, error) {
	normalized, ok := identityDomain.NormalizePhone(phone)
	if !ok {
		return nil, tenantDomain.ErrAccountNotFound
	}

	return summaryOrNotFound(a.accounts.FindByPhone(ctx, normalized))
}

func (a accountDirectory) FindByID(ctx context.Context, id string) (*tenantUsecase.AccountSummary, error) {
	return summaryOrNotFound(a.accounts.FindByID(ctx, id))
}

func (a accountDirectory) ListByIDs(ctx context.Context, ids []string) (map[string]*tenantUsecase.AccountSummary, error) {
	accounts, err := a.accounts.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*tenantUsecase.AccountSummary, len(accounts))
	for _, acc := range accounts {
		result[acc.ID] = toSummary(acc)
	}

	return result, nil
}

func summaryOrNotFound(acc *identityDomain.Account, err error) (*tenantUsecase.AccountSummary, error) {
	if err != nil {
		if errors.Is(err, identityDomain.ErrAccountNotFound) {
			return nil, tenantDomain.ErrAccountNotFound
		}

		return nil, err
	}

	return toSummary(acc), nil
}

func toSummary(acc *identityDomain.Account) *tenantUsecase.AccountSummary {
	return &tenantUsecase.AccountSummary{
		ID:            acc.ID,
		FullName:      acc.FullName,
		Email:         acc.Email,
		Phone:         acc.Phone,
		Locale:        string(acc.Locale.OrDefault()),
		Active:        acc.Status == identityDomain.AccountStatusActive,
		EmailVerified: acc.EmailVerified(),
	}
}

// limitChecker lets the Tenant module enforce License plan limits.
type limitChecker struct {
	entitlements *licenseUsecase.EntitlementUseCase
}

func (l limitChecker) CheckLimit(ctx context.Context, ownerID string, limit tenantUsecase.Limit, current int) error {
	return l.entitlements.CheckLimit(ctx, ownerID, licenseDomain.LimitType(limit), current)
}

// garageOwners lets the License module find a garage's owner.
type garageOwners struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g garageOwners) GarageOwner(ctx context.Context, garageID string) (string, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return "", err
	}

	return garage.OwnerAccountID, nil
}

// quoteCustomers gives the Quote module a read-only view of Customer &
// Vehicle data: the names shown on a quote, cross-checked so a vehicle
// must belong to the customer given alongside it, and the customer's
// e-mail (SendQuote's LINK-mode notification).
type quoteCustomers struct {
	customers *customerRepository.PostgresCustomerRepository
	vehicles  *customerRepository.PostgresVehicleRepository
}

func (q quoteCustomers) Find(ctx context.Context, garageID, customerID, vehicleID string) (string, string, error) {
	vehicle, err := q.vehicles.FindByID(ctx, garageID, vehicleID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrVehicleNotFound) {
			return "", "", quoteDomain.ErrVehicleRequired
		}

		return "", "", err
	}

	if vehicle.CustomerID != customerID {
		return "", "", quoteDomain.ErrCustomerRequired
	}

	return vehicle.CustomerName, vehicle.LicensePlate, nil
}

func (q quoteCustomers) Email(ctx context.Context, garageID, customerID string) (*string, error) {
	customer, err := q.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrCustomerNotFound) {
			return nil, nil //nolint:nilnil // an unknown customer has no e-mail to notify; Find already validated the id
		}

		return nil, err
	}

	return customer.Email, nil
}

// quoteGarages lets the Quote module read a garage's numbering and
// approval-mode configuration (FR-TEN-05) without importing Tenant.
type quoteGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g quoteGarages) Get(ctx context.Context, garageID string) (quoteUsecase.GarageInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return quoteUsecase.GarageInfo{}, err
	}

	mode := quoteDomain.ApprovalModeLink
	if garage.DefaultQuoteApprovalMode == tenantDomain.QuoteApprovalModeManual {
		mode = quoteDomain.ApprovalModeManual
	}

	return quoteUsecase.GarageInfo{
		OwnerAccountID: garage.OwnerAccountID, Timezone: garage.Timezone, Name: garage.Name,
		QuoteNumberTemplate: garage.QuoteNumberTemplate, RepairOrderNumberTemplate: garage.RepairOrderNumberTemplate,
		DefaultQuoteApprovalMode: mode,
	}, nil
}

// quoteRepairOrders lets the Quote module convert an approved quote into a
// draft repair order (FR-QUO-04) without importing the Repair Order
// module's use case directly.
type quoteRepairOrders struct {
	uc *repairorderUsecase.RepairOrderUseCase
}

func (r quoteRepairOrders) Create(
	ctx context.Context, garageID, quoteID, vehicleID, number, actorID string, lines []quoteDomain.Line,
) (string, error) {
	roLines := make([]repairorderDomain.Line, 0, len(lines))
	for _, l := range lines {
		roLines = append(roLines, repairorderDomain.Line{
			Position: l.Position, ItemType: repairorderDomain.ItemType(l.ItemType), ItemRefID: l.ItemRefID,
			Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, LineTotal: l.LineTotal,
		})
	}

	ro, err := r.uc.CreateFromQuote(ctx, repairorderUsecase.CreateFromQuoteRequest{
		GarageID: garageID, QuoteID: quoteID, ActorID: actorID, VehicleID: vehicleID, Number: number, Lines: roLines,
	})
	if err != nil {
		return "", err
	}

	return ro.ID, nil
}

// vehicleHistory gives the Customer module a read-only view of a
// vehicle's quotes and repair orders (FR-CUS-03), across the modules that
// own them, without Customer importing either directly.
type vehicleHistory struct {
	quotes       *quoteRepository.PostgresRepository
	repairOrders *repairorderUsecase.RepairOrderUseCase
}

func (h vehicleHistory) History(ctx context.Context, garageID, vehicleID string) ([]customerUsecase.HistoryEntry, error) {
	quotes, _, err := h.quotes.List(ctx, garageID, quoteUsecase.ListFilter{VehicleID: vehicleID, Limit: maxHistoryEntries})
	if err != nil {
		return nil, err
	}

	repairOrders, err := h.repairOrders.ListForVehicle(ctx, garageID, vehicleID)
	if err != nil {
		return nil, err
	}

	entries := make([]customerUsecase.HistoryEntry, 0, len(quotes)+len(repairOrders))
	for _, q := range quotes {
		entries = append(entries, customerUsecase.HistoryEntry{
			Type: customerUsecase.HistoryEntryQuote, EntityID: q.ID, Number: q.Number, Status: string(q.Status),
			TotalAmount: q.TotalAmount, OccurredAt: q.CreatedAt,
		})
	}

	for _, ro := range repairOrders {
		entries = append(entries, customerUsecase.HistoryEntry{
			Type: customerUsecase.HistoryEntryRepairOrder, EntityID: ro.ID, Number: ro.Number, Status: string(ro.Status),
			TotalAmount: ro.TotalAmount, OccurredAt: ro.CreatedAt,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].OccurredAt.After(entries[j].OccurredAt) })

	return entries, nil
}

// maxHistoryEntries bounds the vehicle history read (FR-CUS-03 shows the
// "complete" history, but a single vehicle realistically never approaches
// this many quotes).
const maxHistoryEntries = 1000
