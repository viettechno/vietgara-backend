package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// Audit actions and document type of the return to supplier.
const (
	ActionSupplierReturnCreated   = "supplier_return.created"
	ActionSupplierReturnCancelled = "supplier_return.cancelled"
	entitySupplierReturn          = "supplier_return"

	docTypeSupplierReturn = "supplier_return"
)

// CreateSupplierReturnRequest is the input for InventoryUseCase.CreateSupplierReturn.
type CreateSupplierReturnRequest struct {
	GarageID   string
	ActorID    string
	SupplierID string
	// GoodsReceiptID is the completed receipt the parts came from; empty when
	// unknown.
	GoodsReceiptID   string
	CreditNoteNumber string
	Notes            string
	// CanSetCost is false for a caller without inventory.cost.read: their
	// unit costs are ignored and the default credit applies.
	CanSetCost bool
	Lines      []AdjustmentLineInput
}

// CreateSupplierReturn posts a return to supplier (FR-INV-10): stock falls by
// each line and the credit owed by the supplier is recorded. A line without a
// credit takes the cost of the same part on the linked receipt, else the
// part's average cost. It is refused with domain.ErrInsufficientStock when a
// part is not on hand in that quantity.
func (uc *InventoryUseCase) CreateSupplierReturn(ctx context.Context, req CreateSupplierReturnRequest) (*domain.SupplierReturn, error) {
	const op = "create supplier return"

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.checkSupplier(ctx, req.GarageID, req.SupplierID); err != nil {
		return nil, toAppError(op, err)
	}

	receiptCosts, err := uc.receiptCosts(ctx, req)
	if err != nil {
		return nil, toAppError(op, err)
	}

	lines, err := uc.buildAdjustmentLines(ctx, req.GarageID, creditInputs(req))
	if err != nil {
		return nil, toAppError(op, err)
	}

	balances, err := uc.takeStock(ctx, req.GarageID, lines)
	if err != nil {
		return nil, toAppError(op, err)
	}

	defaultCredits(lines, receiptCosts, balances)

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeSupplierReturn, garage.SupplierReturnNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	s := &domain.SupplierReturn{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, Status: domain.AdjustmentCompleted, SupplierID: req.SupplierID,
		GoodsReceiptID: refOrNil(req.GoodsReceiptID), CreditNoteNumber: req.CreditNoteNumber, Notes: req.Notes, Lines: lines,
		CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	m := movement{garageID: req.GarageID, refType: domain.RefSupplierReturn, refID: s.ID, actorID: req.ActorID}
	if err := uc.takeOutLines(ctx, m, s.Lines); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreateSupplierReturn(ctx, s); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishSupplierReturns(ctx, req.GarageID, s); err != nil {
		return nil, toAppError(op, err)
	}

	err = uc.auditAdjustment(ctx, req.ActorID, req.GarageID, ActionSupplierReturnCreated, entitySupplierReturn, s.ID, nil,
		map[string]any{keyNumber: s.Number, "supplier_id": s.SupplierID, keyStatus: s.Status, keyLines: len(s.Lines), "credit_amount": s.CreditAmount})
	if err != nil {
		return nil, err
	}

	return s, nil
}

// creditInputs returns the lines of the request, with the credits a caller
// without cost access typed removed.
func creditInputs(req CreateSupplierReturnRequest) []AdjustmentLineInput {
	if req.CanSetCost {
		return req.Lines
	}

	inputs := make([]AdjustmentLineInput, len(req.Lines))

	for i, in := range req.Lines {
		in.UnitCost = 0
		inputs[i] = in
	}

	return inputs
}

// defaultCredits gives every line without a credit the cost of the same part
// on the linked receipt, else the part's average cost.
func defaultCredits(lines []domain.AdjustmentLine, receiptCosts map[string]string, balances map[string]Balance) {
	for i := range lines {
		l := &lines[i]
		if l.CreditUnitCost != 0 {
			continue
		}

		base := receiptCosts[l.PartID]
		if base == "" {
			base = balances[l.PartID].AverageCost
		}

		l.CreditUnitCost = domain.CreditPerEnteredUnit(base, l.Quantity, l.EnteredQuantity)
	}
}

// checkSupplier fails with domain.ErrSupplierNotFound when the supplier is not
// one of the garage's.
func (uc *InventoryUseCase) checkSupplier(ctx context.Context, garageID, supplierID string) error {
	if supplierID == "" || uuid.Validate(supplierID) != nil {
		return domain.ErrSupplierNotFound
	}

	names, err := uc.directory.SupplierNames(ctx, garageID, []string{supplierID})
	if err != nil {
		return err
	}

	if _, ok := names[supplierID]; !ok {
		return domain.ErrSupplierNotFound
	}

	return nil
}

// receiptCosts returns the cost per base unit the linked goods receipt paid
// for each part its lines from the supplier name; empty without a link. The
// receipt must be completed and have a line from the supplier.
func (uc *InventoryUseCase) receiptCosts(ctx context.Context, req CreateSupplierReturnRequest) (map[string]string, error) {
	costs := map[string]string{}

	if req.GoodsReceiptID == "" {
		return costs, nil
	}

	r, err := uc.repo.FindGoodsReceipt(ctx, req.GarageID, req.GoodsReceiptID, false)
	if err != nil {
		return nil, err
	}

	if r.Status != domain.GoodsReceiptCompleted {
		return nil, domain.ErrGoodsReceiptNotCompleted
	}

	fromSupplier := false

	for i := range r.Lines {
		l := &r.Lines[i]
		if l.SupplierID == nil || *l.SupplierID != req.SupplierID {
			continue
		}

		fromSupplier = true

		cost, err := lineBaseCost(l)
		if err != nil {
			return nil, err
		}

		if _, seen := costs[l.PartID]; !seen && cost != nil {
			costs[l.PartID] = *cost
		}
	}

	if !fromSupplier {
		return nil, domain.ErrReceiptSupplierMismatch
	}

	return costs, nil
}

// GetSupplierReturn returns one return to supplier with its lines.
func (uc *InventoryUseCase) GetSupplierReturn(ctx context.Context, garageID, returnID string) (*domain.SupplierReturn, error) {
	s, err := uc.repo.FindSupplierReturn(ctx, garageID, returnID, false)
	if err != nil {
		return nil, toAppError("get supplier return", err)
	}

	if err := uc.finishSupplierReturns(ctx, garageID, s); err != nil {
		return nil, toAppError("get supplier return", err)
	}

	return s, nil
}

// ListSupplierReturnsRequest is the input for InventoryUseCase.ListSupplierReturns.
type ListSupplierReturnsRequest struct {
	PageRequest

	GarageID   string
	Query      string
	SupplierID string
	Status     domain.AdjustmentStatus
}

// ListSupplierReturns returns one page of the garage's returns to supplier,
// newest first, without lines.
func (uc *InventoryUseCase) ListSupplierReturns(ctx context.Context, req ListSupplierReturnsRequest) (*Page[*domain.SupplierReturn], error) {
	const op = "list supplier returns"

	page := req.normalize()

	items, total, err := uc.repo.ListSupplierReturns(ctx, req.GarageID, SupplierReturnFilter{
		Query: req.Query, SupplierID: req.SupplierID, Status: req.Status, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.nameSupplierReturns(ctx, req.GarageID, items...); err != nil {
		return nil, toAppError(op, err)
	}

	return &Page[*domain.SupplierReturn]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// CancelSupplierReturn puts the parts of a return back in stock, at the cost
// they left with, and withdraws the credit. A cancelled return cannot be
// cancelled again.
func (uc *InventoryUseCase) CancelSupplierReturn(ctx context.Context, garageID, returnID, actorID string) (*domain.SupplierReturn, error) {
	const op = "cancel supplier return"

	s, err := uc.repo.FindSupplierReturn(ctx, garageID, returnID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if s.Status == domain.AdjustmentCancelled {
		return nil, toAppError(op, domain.ErrSupplierReturnAlreadyCancelled)
	}

	m := movement{garageID: garageID, refType: domain.RefSupplierReturn, refID: s.ID, actorID: actorID}
	if err := uc.putBackLines(ctx, m, s.Lines); err != nil {
		return nil, toAppError(op, err)
	}

	now := uc.now()
	s.Status, s.CancelledAt, s.UpdatedBy = domain.AdjustmentCancelled, &now, actorRef(actorID)

	if err := uc.repo.SaveSupplierReturn(ctx, s); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishSupplierReturns(ctx, garageID, s); err != nil {
		return nil, toAppError(op, err)
	}

	err = uc.auditAdjustment(ctx, actorID, garageID, ActionSupplierReturnCancelled, entitySupplierReturn, s.ID,
		map[string]any{keyStatus: domain.AdjustmentCompleted}, map[string]any{keyNumber: s.Number, keyStatus: s.Status})
	if err != nil {
		return nil, err
	}

	return s, nil
}

// finishSupplierReturns fills the derived descriptions and the credit of
// returns that carry their lines.
func (uc *InventoryUseCase) finishSupplierReturns(ctx context.Context, garageID string, s *domain.SupplierReturn) error {
	if err := uc.describeAdjustmentLines(ctx, garageID, s.Lines); err != nil {
		return err
	}

	if err := uc.nameSupplierReturns(ctx, garageID, s); err != nil {
		return err
	}

	s.CreditAmount = s.Total()

	return nil
}

// nameSupplierReturns fills the supplier, goods receipt and creator names.
func (uc *InventoryUseCase) nameSupplierReturns(ctx context.Context, garageID string, items ...*domain.SupplierReturn) error {
	var supplierIDList, receiptIDs, userIDs []string

	for _, s := range items {
		supplierIDList = append(supplierIDList, s.SupplierID)
		userIDs = append(userIDs, derefAll(s.CreatedBy)...)
		receiptIDs = append(receiptIDs, derefAll(s.GoodsReceiptID)...)
	}

	suppliers, err := uc.supplierNames(ctx, garageID, supplierIDList)
	if err != nil {
		return err
	}

	numbers := map[string]string{}

	if receiptIDs = uniq(receiptIDs); len(receiptIDs) > 0 {
		if numbers, err = uc.repo.GoodsReceiptNumbers(ctx, garageID, receiptIDs); err != nil {
			return err
		}
	}

	users, err := uc.userNames(ctx, userIDs)
	if err != nil {
		return err
	}

	for _, s := range items {
		s.SupplierName, s.CreatedByName = suppliers[s.SupplierID], nameOf(users, s.CreatedBy)

		if s.GoodsReceiptID != nil {
			s.GoodsReceiptNumber = numbers[*s.GoodsReceiptID]
		}
	}

	return nil
}
