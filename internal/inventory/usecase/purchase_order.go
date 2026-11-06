package usecase

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Audit actions, document type and numbering of the purchase order (Release
// 13.0, FR-INV-16).
const (
	ActionPurchaseOrderCreated   = "purchase_order.created"
	ActionPurchaseOrderUpdated   = "purchase_order.updated"
	ActionPurchaseOrderSent      = "purchase_order.sent"
	ActionPurchaseOrderCancelled = "purchase_order.cancelled"
	entityPurchaseOrder          = "purchase_order"

	docTypePurchaseOrder = "purchase_order"

	// PurchaseOrderNumberTemplate numbers purchase orders; like the payment-out
	// voucher it is the same for every garage.
	PurchaseOrderNumberTemplate = "DH-{YYYY}-{SEQ:5}"
)

// SupplierContact is how a purchase order reaches a supplier.
type SupplierContact struct {
	Name  string
	Email string
}

// PurchaseOrderLineInput is one line typed on a purchase order.
type PurchaseOrderLineInput struct {
	// ID matches an existing line of the order on update; empty adds one.
	ID     string
	PartID string
	// UnitID is the packaging unit EnteredQuantity is typed in; empty for
	// the base unit.
	UnitID          string
	EnteredQuantity string
	// UnitCost is the expected VND per entered unit; 0 means unknown.
	UnitCost int64
}

// CreatePurchaseOrderRequest is the input of CreatePurchaseOrder.
type CreatePurchaseOrderRequest struct {
	GarageID string
	ActorID  string
	// CanSetCost is false for a caller without inventory.cost.read: their
	// unit costs are ignored.
	CanSetCost   bool
	SupplierID   string
	ExpectedDate string
	Notes        string
	Lines        []PurchaseOrderLineInput
}

// CreatePurchaseOrder saves a new draft purchase order with a number of its
// own.
func (uc *InventoryUseCase) CreatePurchaseOrder(ctx context.Context, req CreatePurchaseOrderRequest) (*domain.PurchaseOrder, error) {
	const op = "create purchase order"

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	o := &domain.PurchaseOrder{
		ID: uuid.NewString(), GarageID: req.GarageID, Status: domain.PurchaseOrderDraft, SupplierID: req.SupplierID,
		OrderDate: uc.localNow(garage.Timezone).Format(time.DateOnly), Notes: req.Notes,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if err := uc.fillOrder(ctx, o, garage.Timezone, req.ExpectedDate, req.Lines, req.CanSetCost); err != nil {
		return nil, orderError(op, err)
	}

	if o.Number, err = uc.nextNumber(ctx, req.GarageID, docTypePurchaseOrder, PurchaseOrderNumberTemplate, garage.Timezone); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreatePurchaseOrder(ctx, o); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditOrder(ctx, req.ActorID, ActionPurchaseOrderCreated, "", o); err != nil {
		return nil, err
	}

	if err := uc.describeOrders(ctx, req.GarageID, o); err != nil {
		return nil, toAppError(op, err)
	}

	o.TotalAmount = o.Total()

	return o, nil
}

// UpdatePurchaseOrderRequest is the input of UpdatePurchaseOrder: the supplier,
// expected date, notes and lines are replaced.
type UpdatePurchaseOrderRequest struct {
	GarageID     string
	OrderID      string
	ActorID      string
	CanSetCost   bool
	SupplierID   string
	ExpectedDate string
	Notes        string
	Lines        []PurchaseOrderLineInput
}

// UpdatePurchaseOrder replaces a draft order; any other status is refused with
// domain.ErrPurchaseOrderNotDraft.
func (uc *InventoryUseCase) UpdatePurchaseOrder(ctx context.Context, req UpdatePurchaseOrderRequest) (*domain.PurchaseOrder, error) {
	const op = "update purchase order"

	o, err := uc.repo.FindPurchaseOrder(ctx, req.GarageID, req.OrderID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if o.Status != domain.PurchaseOrderDraft {
		return nil, toAppError(op, domain.ErrPurchaseOrderNotDraft)
	}

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	before := o.Status
	existing := o.Lines
	o.SupplierID, o.Notes, o.UpdatedBy = req.SupplierID, req.Notes, actorRef(req.ActorID)

	if err := uc.fillOrder(ctx, o, garage.Timezone, req.ExpectedDate, req.Lines, req.CanSetCost, existing...); err != nil {
		return nil, orderError(op, err)
	}

	if err := uc.repo.SavePurchaseOrder(ctx, o); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditOrder(ctx, req.ActorID, ActionPurchaseOrderUpdated, before, o); err != nil {
		return nil, err
	}

	if err := uc.describeOrders(ctx, req.GarageID, o); err != nil {
		return nil, toAppError(op, err)
	}

	o.TotalAmount = o.Total()

	return o, nil
}

// fillOrder checks and sets the supplier, expected date and lines of an order.
// existing are the lines already stored, for matching by ID.
func (uc *InventoryUseCase) fillOrder(
	ctx context.Context, o *domain.PurchaseOrder, timezone, expected string, inputs []PurchaseOrderLineInput, canSetCost bool,
	existing ...domain.PurchaseOrderLine,
) error {
	if len(inputs) == 0 {
		return domain.ErrNoLines
	}

	if len([]rune(o.Notes)) > domain.MaxNotesLength {
		return domain.ErrNotesTooLong
	}

	if expected != "" {
		d, err := domain.ParseDate(expected)
		if err != nil || d.Format(time.DateOnly) < uc.localNow(timezone).Format(time.DateOnly) {
			return domain.ErrInvalidExpectedDate
		}

		expected = d.Format(time.DateOnly)
	}

	o.ExpectedDate = expected

	contacts, err := uc.directory.SupplierContacts(ctx, o.GarageID, []string{o.SupplierID})
	if err != nil {
		return err
	}

	if _, ok := contacts[o.SupplierID]; !ok {
		return domain.ErrSupplierNotFound
	}

	o.Lines, err = uc.buildOrderLines(ctx, o.GarageID, inputs, existing, canSetCost)

	return err
}

// buildOrderLines turns the typed lines into stored ones: the quantity is
// converted to the base unit and the cost kept or cleared according to the
// caller's cost permission. Without the permission a line keeps the stored
// cost of the line it matches only while its part and unit are unchanged.
func (uc *InventoryUseCase) buildOrderLines(
	ctx context.Context, garageID string, inputs []PurchaseOrderLineInput, existing []domain.PurchaseOrderLine, canSetCost bool,
) ([]domain.PurchaseOrderLine, error) {
	lines := make([]domain.PurchaseOrderLine, 0, len(inputs))
	seen := map[string]bool{}

	for _, in := range inputs {
		if seen[in.PartID] {
			return nil, domain.ErrDuplicatePart
		}

		seen[in.PartID] = true

		line, err := uc.orderLine(ctx, garageID, in)
		if err != nil {
			return nil, err
		}

		line.UnitCost = in.UnitCost
		if !canSetCost {
			line.UnitCost = storedOrderCost(existing, &line)
		}

		if line.UnitCost < 0 {
			return nil, domain.ErrInvalidCost
		}

		lines = append(lines, line)
	}

	return lines, nil
}

// orderLine builds the quantity side of one line and checks its part.
func (uc *InventoryUseCase) orderLine(ctx context.Context, garageID string, in PurchaseOrderLineInput) (domain.PurchaseOrderLine, error) {
	if q, ok := new(big.Rat).SetString(in.EnteredQuantity); !ok || q.Sign() <= 0 {
		return domain.PurchaseOrderLine{}, domain.ErrInvalidQuantity
	}

	part, err := uc.part(ctx, garageID, in.PartID)
	if err != nil {
		return domain.PurchaseOrderLine{}, err
	}

	if !part.IsActive {
		return domain.PurchaseOrderLine{}, domain.ErrPartInactive
	}

	resolved, err := uc.parts.ResolveQuantity(ctx, garageID, part.ID, in.UnitID, in.EnteredQuantity)
	if err != nil {
		return domain.PurchaseOrderLine{}, err
	}

	entered := resolved.Base
	if resolved.Entered != nil {
		entered = *resolved.Entered
	}

	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}

	return domain.PurchaseOrderLine{
		ID: id, PartID: part.ID, UnitID: resolved.UnitID, EnteredQuantity: entered, Quantity: resolved.Base,
	}, nil
}

// storedOrderCost is the cost already stored for the line it replaces, 0 for a
// new line or one whose part or unit changed.
func storedOrderCost(existing []domain.PurchaseOrderLine, line *domain.PurchaseOrderLine) int64 {
	for i := range existing {
		old := &existing[i]
		if old.ID == line.ID && old.PartID == line.PartID && derefOr(old.UnitID) == derefOr(line.UnitID) {
			return old.UnitCost
		}
	}

	return 0
}

func derefOr(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// GetPurchaseOrder returns one order with its lines.
func (uc *InventoryUseCase) GetPurchaseOrder(ctx context.Context, garageID, orderID string) (*domain.PurchaseOrder, error) {
	const op = "get purchase order"

	o, err := uc.repo.FindPurchaseOrder(ctx, garageID, orderID, false)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeOrders(ctx, garageID, o); err != nil {
		return nil, toAppError(op, err)
	}

	o.TotalAmount = o.Total()

	return o, nil
}

// ListPurchaseOrdersRequest is the input of ListPurchaseOrders.
type ListPurchaseOrdersRequest struct {
	PageRequest

	GarageID   string
	Query      string
	SupplierID string
	Status     domain.PurchaseOrderStatus
}

// ListPurchaseOrders returns one page of orders, newest first, without lines.
//
//nolint:dupl // each document is described with its own derived names
func (uc *InventoryUseCase) ListPurchaseOrders(ctx context.Context, req ListPurchaseOrdersRequest) (*Page[*domain.PurchaseOrder], error) {
	const op = "list purchase orders"

	page := req.normalize()

	items, total, err := uc.repo.ListPurchaseOrders(ctx, req.GarageID, PurchaseOrderFilter{
		Query: req.Query, SupplierID: req.SupplierID, Status: req.Status, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeOrders(ctx, req.GarageID, items...); err != nil {
		return nil, toAppError(op, err)
	}

	return &Page[*domain.PurchaseOrder]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// describeOrders fills the derived part, unit and supplier descriptions.
func (uc *InventoryUseCase) describeOrders(ctx context.Context, garageID string, orders ...*domain.PurchaseOrder) error {
	var partIDList, unitIDs, supplierIDList []string

	for _, o := range orders {
		supplierIDList = append(supplierIDList, o.SupplierID)

		for _, l := range o.Lines {
			partIDList = append(partIDList, l.PartID)

			if l.UnitID != nil {
				unitIDs = append(unitIDs, *l.UnitID)
			}
		}
	}

	parts, err := uc.partsByID(ctx, garageID, partIDList)
	if err != nil {
		return err
	}

	units, err := uc.unitAbbreviations(ctx, garageID, uniq(unitIDs))
	if err != nil {
		return err
	}

	contacts := map[string]SupplierContact{}

	if ids := uniq(supplierIDList); len(ids) > 0 {
		if contacts, err = uc.directory.SupplierContacts(ctx, garageID, ids); err != nil {
			return err
		}
	}

	for _, o := range orders {
		o.SupplierName, o.SupplierEmail = contacts[o.SupplierID].Name, contacts[o.SupplierID].Email

		for i := range o.Lines {
			l := &o.Lines[i]
			p := parts[l.PartID]
			l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation

			if l.UnitID != nil {
				l.UnitAbbreviation = units[*l.UnitID]
			}
		}
	}

	return nil
}

// orderError classifies the errors of filling an order: a supplier that is
// not one of the garage's is a 404 of its own.
func orderError(op string, err error) error {
	if errors.Is(err, domain.ErrSupplierNotFound) {
		return apperror.Wrap(apperror.CodeNotFound, err.Error(), err).WithReason(ReasonSupplierNotFound)
	}

	return toAppError(op, err)
}

func (uc *InventoryUseCase) auditOrder(ctx context.Context, actorID, action string, before domain.PurchaseOrderStatus, o *domain.PurchaseOrder) error {
	entry := audit.Entry{
		Actor: actorID, GarageID: o.GarageID, Action: action, EntityType: entityPurchaseOrder, EntityID: o.ID,
		After: map[string]any{keyNumber: o.Number, keyStatus: o.Status, keyLines: len(o.Lines)}, IP: middleware.ClientIP(ctx),
	}

	if before != "" {
		entry.Before = map[string]any{keyStatus: before}
	}

	if err := uc.audit.Record(ctx, entry); err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}
