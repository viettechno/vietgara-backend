package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// ActionPartReturned is the audit action of returning repair order parts.
const (
	ActionPartReturned = "repair_order.part_returned"
	entityPartReturn   = "repair_order_part_return"
)

// ReturnRepairOrderPartRequest is the input for InventoryUseCase.ReturnRepairOrderPart.
type ReturnRepairOrderPartRequest struct {
	GarageID      string
	RepairOrderID string
	ActorID       string
	// RepairOrderLineID is the part line the parts are returned from.
	RepairOrderLineID string
	// UnitID is the packaging unit EnteredQuantity is typed in; empty for the
	// base unit.
	UnitID          string
	EnteredQuantity string
	Reason          string
}

// ReturnRepairOrderPart returns unused parts of a repair order to stock
// (FR-INV-11): one IN movement restores the quantity at the cost snapshot of
// the stock issue line, so the average cost is reversed too. The order's stock
// issue must be posted (domain.ErrStockIssueNotPosted: a draft still follows
// the order's lines) and the total returned from a line cannot exceed what it
// issued (domain.ErrReturnExceedsIssued). The repair order itself is unchanged.
func (uc *InventoryUseCase) ReturnRepairOrderPart(ctx context.Context, req ReturnRepairOrderPartRequest) (*domain.RepairOrderPartReturn, error) {
	const op = "return repair order part"

	reason, err := domain.NormalizeReason(req.Reason)
	if err != nil {
		return nil, toAppError(op, err)
	}

	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, req.GarageID, req.RepairOrderID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if issue.Status != domain.StockIssuePosted {
		return nil, toAppError(op, domain.ErrStockIssueNotPosted)
	}

	line := issueLineOf(issue, req.RepairOrderLineID)
	if line == nil {
		return nil, toAppError(op, domain.ErrStockIssueLineNotFound)
	}

	resolved, err := uc.parts.ResolveQuantity(ctx, req.GarageID, line.PartID, req.UnitID, req.EnteredQuantity)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if domain.Less(line.Quantity, domain.Add(line.ReturnedQuantity, resolved.Base)) {
		return nil, toAppError(op, domain.ErrReturnExceedsIssued)
	}

	entered := resolved.Base
	if resolved.Entered != nil {
		entered = *resolved.Entered
	}

	r := &domain.RepairOrderPartReturn{
		ID: uuid.NewString(), GarageID: req.GarageID, RepairOrderID: issue.RepairOrderID, RepairOrderNumber: issue.RepairOrderNumber,
		StockIssueID: issue.ID, StockIssueLineID: line.ID, RepairOrderLineID: line.RepairOrderLineID, PartID: line.PartID,
		UnitID: resolved.UnitID, EnteredQuantity: entered, Quantity: resolved.Base, Reason: reason, UnitCost: costOrNil(line.UnitCost),
		CreatedBy: actorRef(req.ActorID),
	}

	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: req.GarageID, PartID: r.PartID, Type: domain.TransactionIn, Quantity: r.Quantity,
		UnitID: r.UnitID, EnteredQuantity: enteredIf(r.UnitID, r.EnteredQuantity), UnitCost: r.UnitCost,
		RefType: domain.RefRepairOrderReturn, RefID: &r.ID, Reason: reason, CreatedBy: actorRef(req.ActorID),
	}

	if err := uc.recordMovement(ctx, tx); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreatePartReturn(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describePartReturns(ctx, req.GarageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	err = uc.auditAdjustment(ctx, req.ActorID, req.GarageID, ActionPartReturned, entityPartReturn, r.ID, nil, map[string]any{
		keyRepairOrderID: r.RepairOrderID, "repair_order_line_id": r.RepairOrderLineID, "part_id": r.PartID,
		"quantity": r.Quantity, "reason": r.Reason,
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

// ListRepairOrderPartReturns returns the parts returned from a repair order,
// oldest first.
func (uc *InventoryUseCase) ListRepairOrderPartReturns(ctx context.Context, garageID, repairOrderID string) ([]domain.RepairOrderPartReturn, error) {
	const op = "list repair order part returns"

	items, err := uc.repo.ListPartReturns(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	ptrs := make([]*domain.RepairOrderPartReturn, len(items))
	for i := range items {
		ptrs[i] = &items[i]
	}

	if err := uc.describePartReturns(ctx, garageID, ptrs...); err != nil {
		return nil, toAppError(op, err)
	}

	return items, nil
}

// issueLineOf returns the line of the stock issue that came from the repair
// order line, nil when there is none.
func issueLineOf(issue *domain.StockIssue, repairOrderLineID string) *domain.StockIssueLine {
	for i := range issue.Lines {
		if issue.Lines[i].RepairOrderLineID == repairOrderLineID {
			return &issue.Lines[i]
		}
	}

	return nil
}

// enteredIf is the typed amount when a packaging unit was used.
func enteredIf(unitID *string, entered string) *string {
	if unitID == nil {
		return nil
	}

	return &entered
}

// describePartReturns fills the derived part, unit and creator descriptions.
func (uc *InventoryUseCase) describePartReturns(ctx context.Context, garageID string, items ...*domain.RepairOrderPartReturn) error {
	var partIDList, unitIDs, userIDs []string

	for _, r := range items {
		partIDList = append(partIDList, r.PartID)
		userIDs = append(userIDs, derefAll(r.CreatedBy)...)

		if r.UnitID != nil {
			unitIDs = append(unitIDs, *r.UnitID)
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

	users, err := uc.userNames(ctx, userIDs)
	if err != nil {
		return err
	}

	for _, r := range items {
		p := parts[r.PartID]
		r.PartSKU, r.PartName, r.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation
		r.CreatedByName = nameOf(users, r.CreatedBy)

		if r.UnitID != nil {
			r.UnitAbbreviation = units[*r.UnitID]
		}
	}

	return nil
}
