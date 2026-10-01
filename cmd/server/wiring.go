package main

import (
	"context"
	"errors"
	"sort"

	billingDomain "github.com/viettechno/vietgara-backend/internal/billing/domain"
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	identityDomain "github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	inventoryDomain "github.com/viettechno/vietgara-backend/internal/inventory/domain"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
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
	"github.com/viettechno/vietgara-backend/pkg/apperror"
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
	ctx context.Context, garageID, quoteID, vehicleID, customerName, vehiclePlate, number, actorID string,
	lines []quoteDomain.Line,
) (string, error) {
	roLines := make([]repairorderDomain.Line, 0, len(lines))
	for _, l := range lines {
		roLines = append(roLines, repairorderDomain.Line{
			Position: l.Position, ItemType: repairorderDomain.ItemType(l.ItemType), ItemRefID: l.ItemRefID,
			Description: l.Description, Quantity: l.Quantity, UnitPrice: l.UnitPrice, LineTotal: l.LineTotal,
			Status: repairorderDomain.LineNotStarted,
		})
	}

	ro, err := r.uc.CreateFromQuote(ctx, repairorderUsecase.CreateFromQuoteRequest{
		GarageID: garageID, QuoteID: quoteID, ActorID: actorID, VehicleID: vehicleID, CustomerName: customerName,
		VehiclePlate: vehiclePlate, Number: number, Lines: roLines,
	})
	if err != nil {
		return "", err
	}

	return ro.ID, nil
}

// repairOrderVehicles gives the Repair Order module a read-only view of a
// vehicle's customer name and license plate for ad-hoc creation
// (FR-REP-01), without the customer-id cross-check Quote's own adapter
// does (an ad-hoc repair order is created from a vehicle alone) -- kept as
// its own type so the two modules stay decoupled from each other.
type repairOrderVehicles struct {
	vehicles *customerRepository.PostgresVehicleRepository
}

func (v repairOrderVehicles) Find(ctx context.Context, garageID, vehicleID string) (string, string, error) {
	vehicle, err := v.vehicles.FindByID(ctx, garageID, vehicleID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrVehicleNotFound) {
			return "", "", repairorderDomain.ErrVehicleRequired
		}

		return "", "", err
	}

	return vehicle.CustomerName, vehicle.LicensePlate, nil
}

// repairOrderGarages lets the Repair Order module read a garage's
// numbering configuration (FR-TEN-05) without importing Tenant.
type repairOrderGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g repairOrderGarages) Get(ctx context.Context, garageID string) (repairorderUsecase.GarageInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return repairorderUsecase.GarageInfo{}, err
	}

	return repairorderUsecase.GarageInfo{
		OwnerAccountID: garage.OwnerAccountID, Timezone: garage.Timezone,
		RepairOrderNumberTemplate: garage.RepairOrderNumberTemplate,
	}, nil
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

// billingRepairOrders lets the Billing & Settlement module read a closed
// repair order's settlement-relevant data (FR-BIL-01) without importing
// the Repair Order module's use case directly. Repair Order's own Get
// already returns a classified *apperror.AppError, which Billing's
// toAppError passes through unchanged, so no error translation is needed
// here.
type billingRepairOrders struct {
	uc *repairorderUsecase.RepairOrderUseCase
}

func (r billingRepairOrders) Get(ctx context.Context, garageID, repairOrderID string) (billingUsecase.RepairOrderInfo, error) {
	ro, err := r.uc.Get(ctx, garageID, repairOrderID)
	if err != nil {
		return billingUsecase.RepairOrderInfo{}, err
	}

	return billingUsecase.RepairOrderInfo{
		CustomerName: ro.CustomerName, VehicleLicensePlate: ro.VehicleLicensePlate, TotalAmount: ro.TotalAmount,
		Closed: ro.Status == repairorderDomain.StatusClosed,
	}, nil
}

// billingPartners lets the Billing & Settlement module validate and name
// an insurer allocated to a settlement (FR-BIL-02) without importing the
// Customer module directly.
type billingPartners struct {
	partners *customerRepository.PostgresPartnerRepository
}

func (p billingPartners) FindInsurer(ctx context.Context, garageID, partnerID string) (string, error) {
	partner, err := p.partners.FindByID(ctx, garageID, partnerID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrPartnerNotFound) {
			return "", billingDomain.ErrInsurerNotFound
		}

		return "", err
	}

	if partner.Type != customerDomain.PartnerTypeInsurer {
		return "", billingDomain.ErrInsurerNotFound
	}

	return partner.Name, nil
}

// billingGarages lets the Billing & Settlement module read a garage's
// numbering configuration (FR-TEN-05) without importing Tenant.
type billingGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g billingGarages) Get(ctx context.Context, garageID string) (billingUsecase.GarageInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return billingUsecase.GarageInfo{}, err
	}

	return billingUsecase.GarageInfo{
		OwnerAccountID: garage.OwnerAccountID, Timezone: garage.Timezone,
		SettlementNumberTemplate: garage.SettlementNumberTemplate,
	}, nil
}

// documentQuotes lets the Document/Print module render a quote PDF
// (FR-DOC-01) without importing the Quote & Pricing module's use case
// directly; it wraps the repository directly (the same choice
// vehicleHistory above already makes) since it only needs one read.
type documentQuotes struct {
	quotes *quoteRepository.PostgresRepository
}

func (q documentQuotes) Get(ctx context.Context, garageID, quoteID string) (documentUsecase.QuoteInfo, error) {
	quote, err := q.quotes.FindByID(ctx, garageID, quoteID)
	if err != nil {
		if errors.Is(err, quoteDomain.ErrQuoteNotFound) {
			return documentUsecase.QuoteInfo{}, apperror.Wrap(apperror.CodeNotFound, err.Error(), err)
		}

		return documentUsecase.QuoteInfo{}, err
	}

	lines := make([]render.DocumentLine, 0, len(quote.Lines))
	for _, line := range quote.Lines {
		lines = append(lines, render.DocumentLine{
			Description: line.Description, Quantity: line.Quantity, UnitPrice: line.UnitPrice, LineTotal: line.LineTotal,
		})
	}

	return documentUsecase.QuoteInfo{
		Number: quote.Number, Status: string(quote.Status), CustomerName: quote.CustomerName,
		VehiclePlate: quote.VehicleLicensePlate, Notes: quote.Notes, Lines: lines, TotalAmount: quote.TotalAmount,
	}, nil
}

// documentSettlements lets the Document/Print module render a settlement
// receipt PDF (FR-DOC-01) without importing the Billing & Settlement
// module's use case directly.
type documentSettlements struct {
	uc *billingUsecase.SettlementUseCase
}

func (s documentSettlements) Get(ctx context.Context, garageID, settlementID string) (documentUsecase.SettlementInfo, error) {
	settlement, err := s.uc.Get(ctx, garageID, settlementID)
	if err != nil {
		return documentUsecase.SettlementInfo{}, err
	}

	payments := make([]render.PaymentRow, 0, len(settlement.Payments))
	for _, p := range settlement.Payments {
		payments = append(payments, render.PaymentRow{
			Date: p.CreatedAt.Format("2006-01-02"), PayerType: string(p.PayerType), Method: string(p.Method), Amount: p.Amount,
		})
	}

	return documentUsecase.SettlementInfo{
		Number: settlement.Number, Status: string(settlement.Status), CustomerName: settlement.CustomerName,
		VehiclePlate: settlement.VehicleLicensePlate, InsurerName: settlement.InsurerPartnerName,
		TotalAmount: settlement.TotalAmount, CustomerAmount: settlement.CustomerAmount,
		InsurerAmount: settlement.InsurerAmount, Payments: payments,
	}, nil
}

// documentStockMovements lets the Document/Print module render the stock
// movement report as Excel (FR-DOC-02) without importing the Inventory
// module's use case directly. Pages through every matching transaction --
// the report is meant to be complete, unlike the JSON report's own
// paginated screen.
type documentStockMovements struct {
	uc *inventoryUsecase.InventoryUseCase
}

func (s documentStockMovements) List(ctx context.Context, garageID, itemID string) ([]render.StockMovementRow, error) {
	var rows []render.StockMovementRow

	for page := 1; ; page++ {
		result, err := s.uc.ListTransactions(ctx, inventoryUsecase.ListTransactionsRequest{
			PageRequest: inventoryUsecase.PageRequest{Page: page, PageSize: inventoryUsecase.MaxPageSize},
			GarageID:    garageID, ItemID: itemID,
		})
		if err != nil {
			return nil, err
		}

		for _, tx := range result.Items {
			direction := "In"
			reference := "Purchase"

			if tx.Type == inventoryDomain.TransactionOut {
				direction = "Out"
			}

			if tx.RefType == inventoryDomain.RefRepairOrder && tx.RefID != nil {
				reference = "Repair order " + *tx.RefID
			}

			rows = append(rows, render.StockMovementRow{
				Date: tx.CreatedAt.Format("2006-01-02"), SKU: tx.ItemSKU, ItemName: tx.ItemName, Direction: direction,
				Quantity: tx.Quantity, Reference: reference,
			})
		}

		if page*inventoryUsecase.MaxPageSize >= result.Total {
			break
		}
	}

	return rows, nil
}

// documentGarages lets the Document/Print module read a garage's
// letterhead (name, address, phone) without importing Tenant.
type documentGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g documentGarages) Get(ctx context.Context, garageID string) (documentUsecase.GarageHeaderInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return documentUsecase.GarageHeaderInfo{}, err
	}

	info := documentUsecase.GarageHeaderInfo{Name: garage.Name}
	if garage.Address != nil {
		info.Address = *garage.Address
	}

	if garage.Phone != nil {
		info.Phone = *garage.Phone
	}

	return info, nil
}
