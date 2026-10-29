package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

// LineEdit changes the content of a line (Release 7.3): nil fields keep their
// value. The type of a line never changes. For a line with a packaging unit
// the amount is EnteredQuantity (Quantity is derived from it); UnitID "" moves
// the line back to the base unit.
type LineEdit struct {
	Description     *string
	ItemRefID       *string
	Quantity        *string
	UnitID          *string
	EnteredQuantity *string
	UnitPrice       *int64
}

// UpdateLineRequest is the input for RepairOrderUseCase.UpdateLine; nil
// fields keep their current value.
type UpdateLineRequest struct {
	GarageID              string
	RepairOrderID         string
	LineID                string
	ActorID               string
	Status                *domain.LineStatus
	AssignedTechnicianIDs *[]string
	Edit                  *LineEdit
}

// UpdateLine assigns technicians, updates one line's own progress
// (FR-REP-02/03) and, from Release 7.3, edits the line itself. The repair
// order's overall status (the Kanban column) is derived from its lines
// afterwards. A part line's item, quantity and unit are fixed once the stock
// issue is posted.
func (uc *RepairOrderUseCase) UpdateLine(ctx context.Context, req UpdateLineRequest) (*domain.RepairOrder, error) {
	const op = "update repair order line"

	ro, err := uc.load(ctx, req.GarageID, req.RepairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := checkEditable(ro); err != nil {
		return nil, toAppError(op, err)
	}

	index := lineIndex(ro, req.LineID)
	if index < 0 {
		return nil, toAppError(op, domain.ErrLineNotFound)
	}

	started := ro.WorkStarted()
	was := ro.Status

	if err := applyProgress(&ro.Lines[index], req); err != nil {
		return nil, toAppError(op, err)
	}

	partsChanged := false

	if req.Edit != nil {
		partsChanged, err = uc.applyEdit(ctx, ro, index, *req.Edit)
		if err != nil {
			return nil, toAppError(op, err)
		}
	}

	ro.Recalculate()
	ro.DeriveStatus()
	ro.UpdatedBy = actorRef(req.ActorID)

	if err := uc.saveUpdated(ctx, ro, req.Edit != nil); err != nil {
		return nil, toAppError(op, err)
	}

	if partsChanged {
		if err := uc.publish(ctx, EventRepairOrderLinesChanged, ro); err != nil {
			return nil, toAppError(op, err)
		}
	}

	if err := uc.publishIfCompleted(ctx, was, ro); err != nil {
		return nil, toAppError(op, err)
	}

	// The first move out of not started is the order starting; Inventory may
	// post its stock issue then (FR-INV-20).
	if !started && ro.WorkStarted() {
		if err := uc.publish(ctx, EventRepairOrderStarted, ro); err != nil {
			return nil, toAppError(op, err)
		}
	}

	return ro, nil
}

// applyProgress applies the status and the assignment of a request to a line.
func applyProgress(line *domain.Line, req UpdateLineRequest) error {
	if req.Status != nil {
		if !req.Status.IsValid() {
			return domain.ErrInvalidLineStatus
		}

		line.Status = *req.Status
	}

	if req.AssignedTechnicianIDs != nil {
		line.AssignedTechnicianIDs = *req.AssignedTechnicianIDs
	}

	return nil
}

// saveUpdated saves the order; the content of its lines only when it was
// edited, since a change of progress or assignment leaves the lines as stored.
func (uc *RepairOrderUseCase) saveUpdated(ctx context.Context, ro *domain.RepairOrder, edited bool) error {
	if edited {
		return uc.RepairOrders.ReplaceLines(ctx, ro)
	}

	return uc.RepairOrders.Save(ctx, ro)
}

// AddLineRequest is the input for RepairOrderUseCase.AddLine.
type AddLineRequest struct {
	GarageID      string
	RepairOrderID string
	ActorID       string
	Line          LineInput
}

// AddLine adds a line, not started, to a repair order that is not closed or
// cancelled (Release 7.3). A part line reaches the draft stock issue through
// repair_order.lines_changed, and is refused once that issue is posted.
func (uc *RepairOrderUseCase) AddLine(ctx context.Context, req AddLineRequest) (*domain.RepairOrder, error) {
	const op = "add repair order line"

	ro, err := uc.load(ctx, req.GarageID, req.RepairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := checkEditable(ro); err != nil {
		return nil, toAppError(op, err)
	}

	if len(ro.Lines) >= domain.MaxLines {
		return nil, toAppError(op, domain.ErrTooManyLines)
	}

	line, err := uc.buildLine(ctx, req.GarageID, req.Line, len(ro.Lines)+1)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if consumesStock(line) {
		if err := uc.checkPartsMayChange(ctx, ro); err != nil {
			return nil, toAppError(op, err)
		}
	}

	ro.Lines = append(ro.Lines, line)

	return uc.saveEdited(ctx, ro, req.ActorID, op, consumesStock(line))
}

// DeleteLine removes a line from a repair order that is not closed or
// cancelled (Release 7.3); the last billable line stays. A part line cannot be
// removed once the stock issue is posted.
func (uc *RepairOrderUseCase) DeleteLine(ctx context.Context, garageID, repairOrderID, lineID, actorID string) (*domain.RepairOrder, error) {
	const op = "delete repair order line"

	ro, err := uc.load(ctx, garageID, repairOrderID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := checkEditable(ro); err != nil {
		return nil, toAppError(op, err)
	}

	index := lineIndex(ro, lineID)
	if index < 0 {
		return nil, toAppError(op, domain.ErrLineNotFound)
	}

	if len(ro.Lines) == 1 {
		return nil, toAppError(op, domain.ErrLastLine)
	}

	removed := ro.Lines[index]
	ro.Lines = append(ro.Lines[:index:index], ro.Lines[index+1:]...)

	if !hasBillable(ro.Lines) {
		return nil, toAppError(op, domain.ErrInvalidNonBillable)
	}

	for i := range ro.Lines {
		ro.Lines[i].Position = i + 1
	}

	touchesParts := consumesStock(removed)
	if touchesParts {
		if err := uc.checkPartsMayChange(ctx, ro); err != nil {
			return nil, toAppError(op, err)
		}
	}

	return uc.saveEdited(ctx, ro, actorID, op, touchesParts)
}

// saveEdited recomputes the totals and status of an order whose lines changed,
// saves it and, when a part line was involved, publishes the new part lines.
func (uc *RepairOrderUseCase) saveEdited(ctx context.Context, ro *domain.RepairOrder, actorID, op string, partsChanged bool) (*domain.RepairOrder, error) {
	was := ro.Status

	ro.Recalculate()
	ro.DeriveStatus()
	ro.UpdatedBy = actorRef(actorID)

	if err := uc.RepairOrders.ReplaceLines(ctx, ro); err != nil {
		return nil, toAppError(op, err)
	}

	if partsChanged {
		if err := uc.publish(ctx, EventRepairOrderLinesChanged, ro); err != nil {
			return nil, toAppError(op, err)
		}
	}

	if err := uc.publishIfCompleted(ctx, was, ro); err != nil {
		return nil, toAppError(op, err)
	}

	return ro, nil
}

// applyEdit applies edit to the line at index and reports whether its part,
// quantity or unit changed, which the stock issue must follow.
func (uc *RepairOrderUseCase) applyEdit(ctx context.Context, ro *domain.RepairOrder, index int, edit LineEdit) (bool, error) {
	old := ro.Lines[index]

	in, err := edit.apply(old)
	if err != nil {
		return false, err
	}

	line, err := uc.buildLine(ctx, ro.GarageID, in, old.Position)
	if err != nil {
		return false, err
	}

	// The line keeps its identity and its progress.
	line.ID, line.Status, line.AssignedTechnicianIDs = old.ID, old.Status, old.AssignedTechnicianIDs

	partsChanged := (consumesStock(old) || consumesStock(line)) && !samePart(old, line)
	if partsChanged {
		if err := uc.checkPartsMayChange(ctx, ro); err != nil {
			return false, err
		}
	}

	ro.Lines[index] = line

	return partsChanged, nil
}

// apply returns the input that builds the line as edit changes it. A line
// typed in a packaging unit takes its amount from EnteredQuantity: sending only
// Quantity for it is refused rather than silently ignored.
func (e LineEdit) apply(old domain.Line) (LineInput, error) {
	in := LineInput{
		ItemType: old.ItemType, ItemRefID: old.ItemRefID, Description: old.Description, Quantity: old.Quantity,
		UnitPrice: old.UnitPrice, JobID: old.JobID, NonBillable: old.NonBillable,
	}

	if old.UnitID != nil && old.EnteredQuantity != nil {
		in.UnitID, in.EnteredQuantity = *old.UnitID, *old.EnteredQuantity
	}

	if e.Description != nil {
		in.Description = *e.Description
	}

	if e.ItemRefID != nil {
		in.ItemRefID = e.ItemRefID
	}

	if e.UnitPrice != nil {
		in.UnitPrice = *e.UnitPrice
	}

	return e.applyAmount(in)
}

// applyAmount applies the quantity, unit and entered amount of the edit.
func (e LineEdit) applyAmount(in LineInput) (LineInput, error) {
	if e.UnitID != nil {
		in.UnitID = *e.UnitID
		if in.UnitID == "" {
			in.EnteredQuantity = "" // back to the base unit: the quantity is the base amount
		}
	}

	if e.EnteredQuantity != nil {
		in.EnteredQuantity = *e.EnteredQuantity
	}

	if e.Quantity != nil {
		if in.UnitID != "" && e.EnteredQuantity == nil {
			return LineInput{}, domain.ErrQuantityNeedsEntered
		}

		in.Quantity = *e.Quantity
	}

	return in, nil
}

// consumesStock reports whether a line takes a catalogue part out of stock.
func consumesStock(l domain.Line) bool {
	return l.ItemType == domain.ItemTypePart && l.ItemRefID != nil && *l.ItemRefID != ""
}

// samePart reports whether two versions of a line consume the same part in the
// same amount.
func samePart(a, b domain.Line) bool {
	return equalRef(a.ItemRefID, b.ItemRefID) && a.Quantity == b.Quantity && equalRef(a.UnitID, b.UnitID) &&
		equalRef(a.EnteredQuantity, b.EnteredQuantity)
}

func equalRef(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
}

// checkPartsMayChange refuses a change of the order's parts once the stock
// issue is posted: the parts have left stock.
func (uc *RepairOrderUseCase) checkPartsMayChange(ctx context.Context, ro *domain.RepairOrder) error {
	posted, err := uc.StockIssues.Posted(ctx, ro.GarageID, ro.ID)
	if err != nil {
		return err
	}

	if posted {
		return domain.ErrStockIssuePosted
	}

	return nil
}

// checkEditable refuses a change to a closed order (ErrAlreadyClosed, as
// before) or a cancelled one.
func checkEditable(ro *domain.RepairOrder) error {
	switch ro.Status { //nolint:exhaustive // the other statuses are editable
	case domain.StatusClosed:
		return domain.ErrAlreadyClosed
	case domain.StatusCancelled:
		return domain.ErrNotEditable
	default:
		return nil
	}
}

// load locks the order's row and reads the order, so that the changes to one
// order (a line edit, the shortage mark, a cancel) never overwrite each other.
func (uc *RepairOrderUseCase) load(ctx context.Context, garageID, repairOrderID string) (*domain.RepairOrder, error) {
	if err := uc.RepairOrders.Lock(ctx, garageID, repairOrderID); err != nil {
		return nil, err
	}

	return uc.RepairOrders.FindByID(ctx, garageID, repairOrderID)
}

func lineIndex(ro *domain.RepairOrder, lineID string) int {
	for i := range ro.Lines {
		if ro.Lines[i].ID == lineID {
			return i
		}
	}

	return -1
}

func hasBillable(lines []domain.Line) bool {
	for _, line := range lines {
		if !line.NonBillable {
			return true
		}
	}

	return false
}
