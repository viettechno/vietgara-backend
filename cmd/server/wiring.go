package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	billingDomain "github.com/viettechno/vietgara-backend/internal/billing/domain"
	billingRepository "github.com/viettechno/vietgara-backend/internal/billing/repository"
	billingUsecase "github.com/viettechno/vietgara-backend/internal/billing/usecase"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/document/render"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	fileDomain "github.com/viettechno/vietgara-backend/internal/file/domain"
	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	identityDomain "github.com/viettechno/vietgara-backend/internal/identity/domain"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	inventoryDomain "github.com/viettechno/vietgara-backend/internal/inventory/domain"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	licenseDomain "github.com/viettechno/vietgara-backend/internal/license/domain"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	notificationUsecase "github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteDomain "github.com/viettechno/vietgara-backend/internal/quote/domain"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	quoteUsecase "github.com/viettechno/vietgara-backend/internal/quote/usecase"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	reportingUsecase "github.com/viettechno/vietgara-backend/internal/reporting/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/billingparty"
	emailpkg "github.com/viettechno/vietgara-backend/pkg/email"
	"github.com/viettechno/vietgara-backend/pkg/issuer"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
	phonepkg "github.com/viettechno/vietgara-backend/pkg/phone"
)

// The adapters below connect modules through narrow interfaces owned by the
// consuming module, so no module imports another (ADR-001: modules stay
// splittable into services).

// userDirectory gives the Tenant module a read-only view of Identity
// users.
type userDirectory struct {
	users *identityRepo.PostgresUserRepository
}

func (a userDirectory) FindByEmail(ctx context.Context, email string) (*tenantUsecase.UserSummary, error) {
	normalized, err := emailpkg.Normalize(email)
	if err != nil {
		return nil, tenantDomain.ErrUserNotFound
	}

	return summaryOrNotFound(a.users.FindByEmail(ctx, normalized))
}

func (a userDirectory) FindByPhone(ctx context.Context, phone string) (*tenantUsecase.UserSummary, error) {
	normalized, err := phonepkg.Normalize(phone)
	if err != nil {
		return nil, tenantDomain.ErrUserNotFound
	}

	return summaryOrNotFound(a.users.FindByPhone(ctx, normalized))
}

func (a userDirectory) FindByID(ctx context.Context, id string) (*tenantUsecase.UserSummary, error) {
	return summaryOrNotFound(a.users.FindByID(ctx, id))
}

func (a userDirectory) ListByIDs(ctx context.Context, ids []string) (map[string]*tenantUsecase.UserSummary, error) {
	users, err := a.users.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*tenantUsecase.UserSummary, len(users))
	for _, user := range users {
		result[user.ID] = toSummary(user)
	}

	return result, nil
}

func summaryOrNotFound(user *identityDomain.User, err error) (*tenantUsecase.UserSummary, error) {
	if err != nil {
		if errors.Is(err, identityDomain.ErrUserNotFound) {
			return nil, tenantDomain.ErrUserNotFound
		}

		return nil, err
	}

	return toSummary(user), nil
}

func toSummary(user *identityDomain.User) *tenantUsecase.UserSummary {
	return &tenantUsecase.UserSummary{
		ID:            user.ID,
		FullName:      user.FullName,
		Email:         user.Email,
		Phone:         user.Phone,
		Locale:        string(user.Locale.OrDefault()),
		Active:        user.Status == identityDomain.UserStatusActive,
		EmailVerified: user.EmailVerified(),
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

	return garage.OwnerUserID, nil
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
		OwnerUserID: garage.OwnerUserID, Timezone: garage.Timezone, Name: garage.Name,
		QuoteNumberTemplate: garage.QuoteNumberTemplate, RepairOrderNumberTemplate: garage.RepairOrderNumberTemplate,
		DefaultQuoteApprovalMode: mode, Issuer: issuerOf(garage),
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
	billingParty billingparty.Party, lines []quoteDomain.Line,
) (string, error) {
	roLines := make([]repairorderDomain.Line, 0, len(lines))
	for _, l := range lines {
		roLines = append(roLines, repairorderDomain.Line{
			Position: l.Position, ItemType: repairorderDomain.ItemType(l.ItemType), ItemRefID: l.ItemRefID,
			Description: l.Description, Quantity: l.Quantity, UnitID: l.UnitID, EnteredQuantity: l.EnteredQuantity,
			UnitPrice: l.UnitPrice, LineTotal: l.LineTotal, Status: repairorderDomain.LineNotStarted,
		})
	}

	ro, err := r.uc.CreateFromQuote(ctx, repairorderUsecase.CreateFromQuoteRequest{
		GarageID: garageID, QuoteID: quoteID, ActorID: actorID, VehicleID: vehicleID, CustomerName: customerName,
		VehiclePlate: vehiclePlate, Number: number, Lines: roLines, BillingParty: billingParty,
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
		OwnerUserID: garage.OwnerUserID, Timezone: garage.Timezone,
		RepairOrderNumberTemplate: garage.RepairOrderNumberTemplate, Issuer: issuerOf(garage),
	}, nil
}

// issuerOf is the garage's legal identity (FR-TEN-06) for a document to
// snapshot: the legal name and address, falling back to the trading ones.
func issuerOf(garage *tenantDomain.Garage) issuer.Issuer {
	return issuer.Issuer{
		Name:           firstNonEmpty(deref(garage.LegalName), garage.Name),
		Address:        firstNonEmpty(deref(garage.LegalAddress), deref(garage.Address)),
		Phone:          deref(garage.Phone),
		TaxCode:        deref(garage.TaxCode),
		Representative: deref(garage.Representative),
		BankName:       deref(garage.Bank.BankName),
		AccountNumber:  deref(garage.Bank.AccountNumber),
		AccountHolder:  deref(garage.Bank.AccountHolder),
		LogoKey:        deref(garage.LogoKey),
	}
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

// customerUsage tells the Customer module whether live quotes or repair
// orders still refer to a customer or a vehicle (the delete-or-deactivate
// rule, FR-CAT-07), without Customer importing either module. A settlement
// belongs to a repair order, so repair orders cover settlements too.
type customerUsage struct {
	quotes       *quoteRepository.PostgresRepository
	repairOrders *repairorderUsecase.RepairOrderUseCase
}

func (u customerUsage) CustomerInUse(ctx context.Context, garageID, customerID string, vehicleIDs []string) (bool, error) {
	return lifecycle.AnyInUse(ctx,
		func(ctx context.Context) (bool, error) {
			return u.quotes.ExistsLive(ctx, garageID, customerID, vehicleIDs)
		},
		func(ctx context.Context) (bool, error) {
			return u.repairOrders.HasLiveForVehicles(ctx, garageID, vehicleIDs)
		},
	)
}

func (u customerUsage) VehicleInUse(ctx context.Context, garageID, vehicleID string) (bool, error) {
	return u.CustomerInUse(ctx, garageID, "", []string{vehicleID})
}

// VehiclesInUse answers VehicleInUse for a page of vehicles in two queries:
// the vehicle ids a live quote or repair order refers to (a settlement
// belongs to a repair order).
func (u customerUsage) VehiclesInUse(ctx context.Context, garageID string, vehicleIDs []string) (map[string]bool, error) {
	used := map[string]bool{}

	if len(vehicleIDs) == 0 {
		return used, nil
	}

	quoted, err := u.quotes.LiveVehicleIDs(ctx, garageID, vehicleIDs)
	if err != nil {
		return nil, err
	}

	ordered, err := u.repairOrders.LiveVehicleIDs(ctx, garageID, vehicleIDs)
	if err != nil {
		return nil, err
	}

	for _, id := range slices.Concat(quoted, ordered) {
		used[id] = true
	}

	return used, nil
}

// taxProfileUsage tells the Customer module whether a quote or settlement
// was billed to a tax profile (the delete-or-deactivate rule, FR-CAT-07),
// without Customer importing either module. It is filled in once the
// modules are built, since they in turn resolve billing parties through the
// Customer module.
type taxProfileUsage struct {
	quotes      *quoteRepository.PostgresRepository
	settlements *billingRepository.PostgresRepository
}

func (u *taxProfileUsage) TaxProfileInUse(ctx context.Context, garageID, profileID string) (bool, error) {
	return lifecycle.AnyInUse(ctx,
		func(ctx context.Context) (bool, error) { return u.quotes.BilledToTaxProfile(ctx, garageID, profileID) },
		func(ctx context.Context) (bool, error) {
			return u.settlements.BilledToTaxProfile(ctx, garageID, profileID)
		},
	)
}

// quoteBillingParties lets the Quote module resolve who a quote is billed
// to from the customer's tax profiles (FR-CUS-07).
type quoteBillingParties struct {
	profiles *customerUsecase.TaxProfileUseCase
}

func (b quoteBillingParties) Resolve(ctx context.Context, garageID, customerID string, profileID *string) (billingparty.Party, error) {
	return b.profiles.Resolve(ctx, garageID, customerID, profileID)
}

// billingBillingParties lets the Billing module resolve who a settlement is
// billed to; a repair order knows its vehicle, whose customer owns the
// profiles.
type billingBillingParties struct {
	profiles *customerUsecase.TaxProfileUseCase
	vehicles *customerRepository.PostgresVehicleRepository
}

func (b billingBillingParties) Resolve(ctx context.Context, garageID, vehicleID string, profileID *string) (billingparty.Party, error) {
	vehicle, err := b.vehicles.FindByID(ctx, garageID, vehicleID)
	if err != nil {
		return billingparty.Party{}, err
	}

	return b.profiles.Resolve(ctx, garageID, vehicle.CustomerID, profileID)
}

// supplierUsage tells the Supplier module whether documents refer to a
// supplier. Nothing does until goods receipts arrive (Phase 7), so a
// supplier is always deleted for now; the goods receipt check is added here.
type supplierUsage struct{}

func (supplierUsage) SupplierInUse(context.Context, string, string) (bool, error) { return false, nil }

// tenantLogos lets the Tenant module keep garage logos in the file registry
// without importing the File module.
type tenantLogos struct {
	files *fileUsecase.UseCase
	store storage.Store
}

func (l tenantLogos) StoreLogo(ctx context.Context, garageID, actorID string, content []byte, contentType string) (string, error) {
	key, err := l.files.StoreLogo(ctx, garageID, actorID, content, contentType)
	if err != nil {
		if errors.Is(err, fileDomain.ErrInvalidFile) {
			return "", tenantDomain.ErrInvalidLogo
		}

		return "", err
	}

	return key, nil
}

// Discard removes the object of an upload whose registration rolled back.
func (l tenantLogos) Discard(ctx context.Context, key string) { _ = l.store.Delete(ctx, key) }

func (l tenantLogos) Retire(ctx context.Context, garageID, actorID, key string) error {
	return l.files.RetireByKey(ctx, garageID, actorID, key)
}

func (l tenantLogos) PublicURL(key string) string { return l.files.PublicURL(key) }

// fileUsage tells the File module whether a business row still refers to a
// file (the delete-or-deactivate rule, FR-CAT-07): today the garage's logo.
type fileUsage struct {
	garages *tenantRepo.PostgresGarageRepository
	parts   *catalogueRepository.PostgresRepository
}

func (u fileUsage) FileInUse(ctx context.Context, garageID string, f *fileDomain.File) (bool, error) {
	if f.Kind == fileDomain.KindPartImage {
		return u.parts.FileAttached(ctx, garageID, f.ID)
	}

	g, err := u.garages.FindByID(ctx, garageID)
	if err != nil {
		return false, err
	}

	return g.LogoKey != nil && *g.LogoKey == f.Key, nil
}

// fileEntities tells the File module whether the entity a file is uploaded
// for exists in the garage. The garage's own files are checked by the use
// case; each other kind (a part image today, a repair photo later) adds its
// case here, over the module that owns the entity. The Catalogue is set once
// it is built, since it in turn stores its images through the File module.
type fileEntities struct {
	parts *catalogueUsecase.UseCase
}

func (e *fileEntities) EntityExists(ctx context.Context, garageID, entityType, entityID string) (bool, error) {
	if entityType == fileDomain.EntityPart && e.parts != nil {
		return e.parts.PartExists(ctx, garageID, entityID)
	}

	return false, nil
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
		VehicleID: ro.VehicleID, BillingParty: ro.BillingParty, CustomerName: ro.CustomerName, VehicleLicensePlate: ro.VehicleLicensePlate, TotalAmount: ro.TotalAmount,
		Closed: ro.Status == repairorderDomain.StatusClosed,
	}, nil
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
		OwnerUserID: garage.OwnerUserID, Timezone: garage.Timezone,
		SettlementNumberTemplate: garage.SettlementNumberTemplate,
		VoucherNumberTemplate:    garage.PaymentVoucherNumberTemplate,
		Issuer:                   issuerOf(garage),
	}, nil
}

// billingCollectors lets the Billing & Settlement module name the staff
// member who collects a payment on its voucher without importing Identity.
type billingCollectors struct {
	users *identityRepo.PostgresUserRepository
}

func (c billingCollectors) FullName(ctx context.Context, userID string) (string, error) {
	if userID == "" {
		return "", nil
	}

	user, err := c.users.FindByID(ctx, userID)
	if err != nil {
		return "", err
	}

	return user.FullName, nil
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
			Description: line.Description, Quantity: documentQuantity(line.Quantity, line.EnteredQuantity, line.UnitAbbreviation),
			UnitPrice: line.UnitPrice, LineTotal: line.LineTotal,
		})
	}

	return documentUsecase.QuoteInfo{
		Number: quote.Number, Status: string(quote.Status), CustomerName: quote.CustomerName,
		VehiclePlate: quote.VehicleLicensePlate, Notes: quote.Notes, Lines: lines, TotalAmount: quote.TotalAmount,
		Issuer: quote.Issuer, BillingParty: quote.BillingParty,
	}, nil
}

// documentQuantity is how a line's quantity prints: the base-unit amount the
// price applies to, with what was typed when it was another unit
// ("240 (10 case)").
func documentQuantity(quantity string, entered *string, unit string) string {
	if entered == nil || unit == "" {
		return quantity
	}

	return quantity + " (" + *entered + " " + unit + ")"
}

// documentRepairOrders lets the Document/Print module render a repair
// order PDF (FR-DOC-01) without importing the Repair Order module's use
// case directly. Repair Order's own Get already returns a classified
// *apperror.AppError, which this module's toAppError passes through
// unchanged, so no error translation is needed here (the same choice
// documentSettlements below makes).
type documentRepairOrders struct {
	uc *repairorderUsecase.RepairOrderUseCase
}

func (r documentRepairOrders) Get(ctx context.Context, garageID, repairOrderID string) (documentUsecase.RepairOrderInfo, error) {
	ro, err := r.uc.Get(ctx, garageID, repairOrderID)
	if err != nil {
		return documentUsecase.RepairOrderInfo{}, err
	}

	lines := make([]render.DocumentLine, 0, len(ro.Lines))
	for _, line := range ro.Lines {
		lines = append(lines, render.DocumentLine{
			Description: line.Description, Quantity: documentQuantity(line.Quantity, line.EnteredQuantity, line.UnitAbbreviation),
			UnitPrice: line.UnitPrice, LineTotal: line.LineTotal,
		})
	}

	return documentUsecase.RepairOrderInfo{
		Number: ro.Number, Status: string(ro.Status), CustomerName: ro.CustomerName, VehiclePlate: ro.VehicleLicensePlate,
		Lines: lines, TotalAmount: ro.TotalAmount, Issuer: ro.Issuer, BillingParty: ro.BillingParty,
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
	vouchers := make([]documentUsecase.VoucherInfo, 0, len(settlement.Payments))

	for _, p := range settlement.Payments {
		vouchers = append(vouchers, documentUsecase.VoucherInfo{
			ID: p.ID, Number: p.VoucherNumber, Date: p.CreatedAt.In(vietnamTime).Format("02/01/2006"),
			PayerName: p.PayerName, PayerAddress: p.PayerAddress, Reason: p.Reason, Method: voucherMethod(p.Method),
			CollectorName: p.CollectorName, Amount: p.Amount, Voided: p.Voided(), VoidReason: p.VoidReason, Issuer: p.Issuer,
		})

		// A voided payment is not money received.
		if p.Voided() {
			continue
		}

		payments = append(payments, render.PaymentRow{
			Date: p.CreatedAt.Format("2006-01-02"), Method: string(p.Method), Amount: p.Amount,
		})
	}

	return documentUsecase.SettlementInfo{
		Number: settlement.Number, Status: string(settlement.Status), CustomerName: settlement.CustomerName,
		VehiclePlate: settlement.VehicleLicensePlate, TotalAmount: settlement.TotalAmount,
		PaidAmount: settlement.PaidAmount, Payments: payments, Vouchers: vouchers, Issuer: settlement.Issuer,
		BillingParty: settlement.BillingParty,
	}, nil
}

// vietnamTime is the zone a voucher's date is printed in.
var vietnamTime = time.FixedZone("ICT", 7*60*60)

// voucherMethod names a payment method on the Vietnamese voucher.
func voucherMethod(m billingDomain.PaymentMethod) string {
	switch m {
	case billingDomain.MethodCash:
		return "Tiền mặt"
	case billingDomain.MethodBankTransfer:
		return "Chuyển khoản"
	case billingDomain.MethodOnline:
		return "Thanh toán trực tuyến"
	default:
		return string(m)
	}
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
			GarageID:    garageID, PartID: itemID,
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

			row := render.StockMovementRow{
				Date: tx.CreatedAt.Format("2006-01-02"), SKU: tx.PartSKU, ItemName: tx.PartName, Direction: direction,
				Quantity: tx.Quantity, BaseUnit: tx.BaseUnitAbbreviation, Reference: reference,
			}

			if tx.EnteredQuantity != nil {
				row.EnteredQuantity, row.EnteredUnit = *tx.EnteredQuantity, tx.UnitAbbreviation
			}

			rows = append(rows, row)
		}

		if page*inventoryUsecase.MaxPageSize >= result.Total {
			break
		}
	}

	return rows, nil
}

// documentRevenue lets the Document/Print module render the revenue report as
// Excel (FR-DOC-02) without importing the Reporting module's use case.
type documentRevenue struct {
	uc *reportingUsecase.ReportingUseCase
}

func (r documentRevenue) Revenue(
	ctx context.Context, garageID, period string, from, to time.Time,
) (documentUsecase.RevenueInfo, error) {
	report, err := r.uc.GetRevenueReport(ctx, reportingUsecase.GetRevenueReportRequest{
		GarageID: garageID, Period: reportingUsecase.Period(period), From: from, To: to,
	})
	if err != nil {
		return documentUsecase.RevenueInfo{}, err
	}

	rows := make([]render.RevenueRow, 0, len(report.Points))
	for _, p := range report.Points {
		rows = append(rows, render.RevenueRow{Period: p.PeriodLabel, Amount: p.Amount})
	}

	return documentUsecase.RevenueInfo{Points: rows, TotalAmount: report.TotalAmount}, nil
}

// documentGarages lets the Document/Print module read a garage's
// letterhead -- the legal identity (FR-TEN-06) and the logo -- without
// importing Tenant or the file store.
type documentGarages struct {
	garages *tenantRepo.PostgresGarageRepository
	store   storage.Store
}

func (g documentGarages) Get(ctx context.Context, garageID string) (documentUsecase.GarageHeaderInfo, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return documentUsecase.GarageHeaderInfo{}, err
	}

	info := documentUsecase.GarageHeaderInfo{
		Name:           firstNonEmpty(deref(garage.LegalName), garage.Name),
		Address:        firstNonEmpty(deref(garage.LegalAddress), deref(garage.Address)),
		Phone:          deref(garage.Phone),
		TaxCode:        deref(garage.TaxCode),
		Representative: deref(garage.Representative),
		Bank: strings.Join(nonEmpty(
			deref(garage.Bank.BankName), deref(garage.Bank.AccountNumber), deref(garage.Bank.AccountHolder)), " - "),
	}

	if garage.LogoKey != nil {
		// A logo that cannot be read leaves the letterhead without it.
		logo, err := g.store.Get(ctx, *garage.LogoKey)
		if err != nil {
			slog.Warn("document: reading the logo failed", "garage_id", garageID, "error", err)
		} else {
			info.Logo = logo
		}
	}

	return info, nil
}

// Logo reads a logo by its object key; the garage's own current logo or the
// one a document snapshotted.
func (g documentGarages) Logo(ctx context.Context, key string) ([]byte, error) {
	return g.store.Get(ctx, key)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))

	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}

// reportingSettlements lets the Reporting module read a garage's revenue
// (FR-RPT-01) without importing Billing & Settlement's use case; it wraps
// the repository directly (the same choice documentQuotes above already
// makes) since it only needs one read with no business rule attached.
type reportingSettlements struct {
	settlements *billingRepository.PostgresRepository
}

var reportingTruncUnits = map[reportingUsecase.Period]string{
	reportingUsecase.PeriodDay: "day", reportingUsecase.PeriodMonth: "month",
	reportingUsecase.PeriodQuarter: "quarter", reportingUsecase.PeriodYear: "year",
}

func (s reportingSettlements) RevenueByPeriod(
	ctx context.Context, garageID string, period reportingUsecase.Period, from, to time.Time,
) ([]reportingUsecase.RevenuePoint, error) {
	buckets, err := s.settlements.RevenueByPeriod(ctx, garageID, reportingTruncUnits[period], from, to)
	if err != nil {
		return nil, err
	}

	points := make([]reportingUsecase.RevenuePoint, 0, len(buckets))
	for _, b := range buckets {
		points = append(points, reportingUsecase.RevenuePoint{PeriodLabel: revenuePeriodLabel(period, b.PeriodStart), Amount: b.Amount})
	}

	return points, nil
}

// notificationUsers lets the Notification module address a reminder
// e-mail by user id (FR-NOT-04) without importing Identity's use case;
// it wraps the repository directly (the same choice userDirectory
// above already makes), translating the raw repository's not-found
// sentinel into this module's own.
type notificationUsers struct {
	users *identityRepo.PostgresUserRepository
}

// PlatformAdmins returns the platform admins.
func (a notificationUsers) PlatformAdmins(ctx context.Context) ([]notificationUsecase.UserInfo, error) {
	admins, err := a.users.ListPlatformAdmins(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]notificationUsecase.UserInfo, 0, len(admins))
	for _, u := range admins {
		out = append(out, notificationUsecase.UserInfo{
			ID: u.ID, Email: u.Email, FullName: u.FullName, Locale: string(u.Locale.OrDefault()),
		})
	}

	return out, nil
}

func (a notificationUsers) Get(ctx context.Context, userID string) (notificationUsecase.UserInfo, error) {
	user, err := a.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, identityDomain.ErrUserNotFound) {
			return notificationUsecase.UserInfo{}, notificationUsecase.ErrUserNotFound
		}

		return notificationUsecase.UserInfo{}, err
	}

	return notificationUsecase.UserInfo{
		Email: user.Email, FullName: user.FullName, Locale: string(user.Locale.OrDefault()),
	}, nil
}

// revenuePeriodLabel formats a bucket's start for its requested
// granularity: "2026-09-30" (day), "2026-09" (month), "2026-Q3" (quarter),
// "2026" (year).
func revenuePeriodLabel(period reportingUsecase.Period, start time.Time) string {
	switch period {
	case reportingUsecase.PeriodDay:
		return start.Format("2006-01-02")
	case reportingUsecase.PeriodMonth:
		return start.Format("2006-01")
	case reportingUsecase.PeriodQuarter:
		return fmt.Sprintf("%d-Q%d", start.Year(), (int(start.Month())-1)/3+1)
	case reportingUsecase.PeriodYear:
		return start.Format("2006")
	default:
		return start.Format("2006-01-02")
	}
}
