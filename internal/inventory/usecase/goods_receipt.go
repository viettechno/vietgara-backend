package usecase

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/pkg/numbering"
)

// Audit actions and document types of the goods receipt.
const (
	ActionGoodsReceiptCompleted = "goods_receipt.completed"
	ActionGoodsReceiptCancelled = "goods_receipt.cancelled"
	entityGoodsReceipt          = "goods_receipt"

	docTypeGoodsReceipt = "goods_receipt"
	docTypeStockIssue   = "stock_issue"

	// keyStatus is the audit key of a document's status.
	keyStatus = "status"
	// keyLines is the audit key of a document's number of lines.
	keyLines = "lines"
	// keyNumber is the audit key of a document's number.
	keyNumber = "number"
)

// ReceiptLineInput is one line typed on a goods receipt.
type ReceiptLineInput struct {
	// ID matches an existing line of the receipt on update; empty adds one.
	ID     string
	PartID string
	// UnitID is the packaging unit EnteredQuantity is typed in; empty for
	// the base unit.
	UnitID          string
	EnteredQuantity string
	// SupplierID overrides the receipt's default supplier; empty inherits it
	// (and then the part's default supplier).
	SupplierID string
	// UnitCost is VND per entered unit; 0 means "not recorded".
	UnitCost int64
}

// CreateGoodsReceiptRequest is the input for InventoryUseCase.CreateGoodsReceipt.
type CreateGoodsReceiptRequest struct {
	GarageID string
	ActorID  string
	// CanSetCost is false for a caller without inventory.cost.read: their
	// unit costs are ignored.
	CanSetCost         bool
	DefaultSupplierID  string
	ReceiptDate        string
	DeliveryNoteNumber string
	Notes              string
	Lines              []ReceiptLineInput
}

// CreateGoodsReceipt saves a new draft goods receipt (FR-INV-06) with a
// number from the garage's numbering template. Each line's quantity is
// converted to the base unit by the Catalogue and its supplier is the line's,
// else the receipt's default, else the part's default supplier.
func (uc *InventoryUseCase) CreateGoodsReceipt(ctx context.Context, req CreateGoodsReceiptRequest) (*domain.GoodsReceipt, error) {
	const op = "create goods receipt"

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	date, err := uc.receiptDate(req.ReceiptDate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeGoodsReceipt, garage.GoodsReceiptNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	r := &domain.GoodsReceipt{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, Status: domain.GoodsReceiptDraft,
		DefaultSupplierID: refOrNil(req.DefaultSupplierID), ReceiptDate: date,
		DeliveryNoteNumber: req.DeliveryNoteNumber, Notes: req.Notes, CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if r.Lines, err = uc.buildReceiptLines(ctx, req.GarageID, r.DefaultSupplierID, req.Lines, nil, req.CanSetCost); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.checkSuppliers(ctx, req.GarageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.CreateGoodsReceipt(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeReceipts(ctx, req.GarageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.TotalAmount = r.Total()

	return r, nil
}

// UpdateGoodsReceiptRequest is the input for InventoryUseCase.UpdateGoodsReceipt;
// nil fields keep their current value.
type UpdateGoodsReceiptRequest struct {
	GarageID  string
	ReceiptID string
	ActorID   string
	// CanSetCost is false for a caller without inventory.cost.read: the costs
	// of lines they send back keep their stored value.
	CanSetCost bool
	// SetDefaultSupplier says DefaultSupplierID is to be applied; an empty
	// value then clears the default.
	SetDefaultSupplier bool
	DefaultSupplierID  string
	ReceiptDate        *string
	DeliveryNoteNumber *string
	Notes              *string
	// Lines replaces the whole list: lines are matched by ID, a line without
	// an ID is added, and a stored line left out is removed.
	Lines *[]ReceiptLineInput
}

// UpdateGoodsReceipt changes a draft goods receipt; any other status is
// refused with domain.ErrGoodsReceiptNotDraft.
func (uc *InventoryUseCase) UpdateGoodsReceipt(ctx context.Context, req UpdateGoodsReceiptRequest) (*domain.GoodsReceipt, error) {
	const op = "update goods receipt"

	r, err := uc.repo.FindGoodsReceipt(ctx, req.GarageID, req.ReceiptID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if r.Status != domain.GoodsReceiptDraft {
		return nil, toAppError(op, domain.ErrGoodsReceiptNotDraft)
	}

	if req.SetDefaultSupplier {
		r.DefaultSupplierID = refOrNil(req.DefaultSupplierID)
	}

	if req.ReceiptDate != nil {
		date, err := domain.ParseDate(*req.ReceiptDate)
		if err != nil {
			return nil, toAppError(op, err)
		}

		r.ReceiptDate = date.Format(time.DateOnly)
	}

	if req.DeliveryNoteNumber != nil {
		r.DeliveryNoteNumber = *req.DeliveryNoteNumber
	}

	if req.Notes != nil {
		r.Notes = *req.Notes
	}

	if req.Lines != nil {
		if r.Lines, err = uc.buildReceiptLines(ctx, req.GarageID, r.DefaultSupplierID, *req.Lines, r.Lines, req.CanSetCost); err != nil {
			return nil, toAppError(op, err)
		}
	}

	if err := uc.checkSuppliers(ctx, req.GarageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.UpdatedBy = actorRef(req.ActorID)

	if err := uc.repo.SaveGoodsReceipt(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeReceipts(ctx, req.GarageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.TotalAmount = r.Total()

	return r, nil
}

// GetGoodsReceipt returns one goods receipt with its lines.
func (uc *InventoryUseCase) GetGoodsReceipt(ctx context.Context, garageID, receiptID string) (*domain.GoodsReceipt, error) {
	r, err := uc.repo.FindGoodsReceipt(ctx, garageID, receiptID, false)
	if err != nil {
		return nil, toAppError("get goods receipt", err)
	}

	if err := uc.describeReceipts(ctx, garageID, r); err != nil {
		return nil, toAppError("get goods receipt", err)
	}

	r.TotalAmount = r.Total()

	return r, nil
}

// ListGoodsReceiptsRequest is the input for InventoryUseCase.ListGoodsReceipts.
type ListGoodsReceiptsRequest struct {
	PageRequest

	GarageID   string
	Query      string
	SupplierID string
	Status     domain.GoodsReceiptStatus
}

// ListGoodsReceipts returns one page of the garage's goods receipts, newest
// first, without lines.
func (uc *InventoryUseCase) ListGoodsReceipts(ctx context.Context, req ListGoodsReceiptsRequest) (*Page[*domain.GoodsReceipt], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListGoodsReceipts(ctx, req.GarageID, GoodsReceiptFilter{
		Query: req.Query, SupplierID: req.SupplierID, Status: req.Status, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list goods receipts", err)
	}

	if err := uc.describeReceipts(ctx, req.GarageID, items...); err != nil {
		return nil, toAppError("list goods receipts", err)
	}

	return &Page[*domain.GoodsReceipt]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// CompleteGoodsReceipt posts a draft receipt (FR-INV-06): one stock movement
// per line, stamped with the line's supplier, which updates stock, the
// weighted-average cost and the part's last cost from that supplier. It is
// refused with domain.ErrSupplierRequired while a line has no supplier.
func (uc *InventoryUseCase) CompleteGoodsReceipt(ctx context.Context, garageID, receiptID, actorID string) (*domain.GoodsReceipt, error) {
	const op = "complete goods receipt"

	r, err := uc.repo.FindGoodsReceipt(ctx, garageID, receiptID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if r.Status != domain.GoodsReceiptDraft {
		return nil, toAppError(op, domain.ErrGoodsReceiptNotDraft)
	}

	if len(r.Lines) == 0 {
		return nil, toAppError(op, domain.ErrNoLines)
	}

	for i := range r.Lines {
		if r.Lines[i].SupplierID == nil {
			return nil, toAppError(op, domain.ErrSupplierRequired)
		}
	}

	if err := uc.activeParts(ctx, garageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	for i := range r.Lines {
		if err := uc.receiveLine(ctx, r, &r.Lines[i], actorID); err != nil {
			return nil, toAppError(op, err)
		}
	}

	now := uc.now()
	r.Status, r.CompletedAt, r.UpdatedBy = domain.GoodsReceiptCompleted, &now, actorRef(actorID)

	if err := uc.repo.SaveGoodsReceipt(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditReceipt(ctx, actorID, ActionGoodsReceiptCompleted, domain.GoodsReceiptDraft, r); err != nil {
		return nil, err
	}

	if err := uc.describeReceipts(ctx, garageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.TotalAmount = r.Total()

	return r, nil
}

// receiveLine writes the IN movement of one receipt line and remembers the
// cost as the supplier's last cost for the part.
func (uc *InventoryUseCase) receiveLine(ctx context.Context, r *domain.GoodsReceipt, l *domain.GoodsReceiptLine, actorID string) error {
	cost, err := lineBaseCost(l)
	if err != nil {
		return err
	}

	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: r.GarageID, PartID: l.PartID, Type: domain.TransactionIn, Quantity: l.Quantity,
		UnitID: l.UnitID, EnteredQuantity: enteredRef(l), UnitCost: cost, RefType: domain.RefGoodsReceipt, RefID: &r.ID,
		SupplierID: l.SupplierID, CreatedBy: actorRef(actorID),
	}

	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		return err
	}

	if cost != nil {
		return uc.repo.RecordLastCost(ctx, r.GarageID, l.PartID, *l.SupplierID, *cost)
	}

	return nil
}

// CancelGoodsReceipt closes a draft receipt, or reverses a completed one:
// every movement is undone and the average cost unwound. Reversing is
// refused with domain.ErrInsufficientStockToReverse when stock already used
// would take a part below zero. A cancelled receipt cannot be cancelled again.
func (uc *InventoryUseCase) CancelGoodsReceipt(ctx context.Context, garageID, receiptID, actorID string) (*domain.GoodsReceipt, error) {
	const op = "cancel goods receipt"

	r, err := uc.repo.FindGoodsReceipt(ctx, garageID, receiptID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	before := r.Status

	switch before {
	case domain.GoodsReceiptCancelled:
		return nil, toAppError(op, domain.ErrGoodsReceiptAlreadyCancelled)
	case domain.GoodsReceiptCompleted:
		if err := uc.reverseReceipt(ctx, r, actorID); err != nil {
			return nil, toAppError(op, err)
		}
	case domain.GoodsReceiptDraft:
	}

	now := uc.now()
	r.Status, r.CancelledAt, r.UpdatedBy = domain.GoodsReceiptCancelled, &now, actorRef(actorID)

	if err := uc.repo.SaveGoodsReceipt(ctx, r); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditReceipt(ctx, actorID, ActionGoodsReceiptCancelled, before, r); err != nil {
		return nil, err
	}

	if err := uc.describeReceipts(ctx, garageID, r); err != nil {
		return nil, toAppError(op, err)
	}

	r.TotalAmount = r.Total()

	return r, nil
}

// reverseReceipt writes the OUT movement that undoes each line of a completed
// receipt, after checking that stock on hand still covers them.
func (uc *InventoryUseCase) reverseReceipt(ctx context.Context, r *domain.GoodsReceipt, actorID string) error {
	need := map[string]string{}

	for _, l := range r.Lines {
		need[l.PartID] = domain.Add(need[l.PartID], l.Quantity)
	}

	partIDs := make([]string, 0, len(need))
	for id := range need {
		partIDs = append(partIDs, id)
	}

	balances, err := uc.repo.Balances(ctx, r.GarageID, partIDs)
	if err != nil {
		return err
	}

	for id, qty := range need {
		if domain.Less(orDefault(balances[id].OnHand, "0"), qty) {
			return domain.ErrInsufficientStockToReverse
		}
	}

	for i := range r.Lines {
		l := &r.Lines[i]

		cost, err := lineBaseCost(l)
		if err != nil {
			return err
		}

		tx := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: r.GarageID, PartID: l.PartID, Type: domain.TransactionOut, Quantity: l.Quantity,
			UnitID: l.UnitID, EnteredQuantity: enteredRef(l), UnitCost: cost, RefType: domain.RefGoodsReceipt, RefID: &r.ID,
			SupplierID: l.SupplierID, CreatedBy: actorRef(actorID),
		}

		if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
			return err
		}
	}

	return nil
}

// buildReceiptLines turns the typed lines into stored ones: the quantity is
// converted to the base unit, the supplier resolved and the cost kept or
// cleared according to the caller's cost permission. existing are the lines
// already stored, for matching by ID.
func (uc *InventoryUseCase) buildReceiptLines(
	ctx context.Context, garageID string, defaultSupplier *string, inputs []ReceiptLineInput,
	existing []domain.GoodsReceiptLine, canSetCost bool,
) ([]domain.GoodsReceiptLine, error) {
	ids := make([]string, 0, len(inputs))
	for _, in := range inputs {
		ids = append(ids, in.PartID)
	}

	parts, err := uc.parts.Parts(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]PartInfo, len(parts))
	for _, p := range parts {
		byID[p.ID] = p
	}

	partDefaults, err := uc.repo.DefaultSuppliers(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	lines := make([]domain.GoodsReceiptLine, 0, len(inputs))

	for _, in := range inputs {
		part, ok := byID[in.PartID]
		if !ok {
			return nil, domain.ErrPartNotFound
		}

		if !part.IsActive {
			return nil, domain.ErrPartInactive
		}

		line, err := uc.receiptLine(ctx, garageID, part, in)
		if err != nil {
			return nil, err
		}

		line.SupplierID = firstRef(in.SupplierID, defaultSupplier, partDefaults[part.ID])
		line.UnitCost = in.UnitCost

		if !canSetCost {
			line.UnitCost = storedCost(existing, in.ID)
		}

		if line.UnitCost < 0 {
			return nil, domain.ErrInvalidCost
		}

		lines = append(lines, line)
	}

	return lines, nil
}

// receiptLine builds the quantity side of one line.
func (uc *InventoryUseCase) receiptLine(ctx context.Context, garageID string, part PartInfo, in ReceiptLineInput) (domain.GoodsReceiptLine, error) {
	resolved, err := uc.parts.ResolveQuantity(ctx, garageID, part.ID, in.UnitID, in.EnteredQuantity)
	if err != nil {
		return domain.GoodsReceiptLine{}, err
	}

	entered := resolved.Base
	if resolved.Entered != nil {
		entered = *resolved.Entered
	}

	id := in.ID
	if id == "" {
		id = uuid.NewString()
	}

	return domain.GoodsReceiptLine{
		ID: id, PartID: part.ID, UnitID: resolved.UnitID, EnteredQuantity: entered, Quantity: resolved.Base,
	}, nil
}

// storedCost is the cost already stored for the line id, 0 for a new line.
func storedCost(existing []domain.GoodsReceiptLine, id string) int64 {
	if i := slices.IndexFunc(existing, func(l domain.GoodsReceiptLine) bool { return l.ID == id && id != "" }); i >= 0 {
		return existing[i].UnitCost
	}

	return 0
}

// checkSuppliers fails with domain.ErrSupplierNotFound when the receipt names
// a supplier that is not one of the garage's.
func (uc *InventoryUseCase) checkSuppliers(ctx context.Context, garageID string, r *domain.GoodsReceipt) error {
	ids := supplierIDs(r)
	if len(ids) == 0 {
		return nil
	}

	names, err := uc.directory.SupplierNames(ctx, garageID, ids)
	if err != nil {
		return err
	}

	for _, id := range ids {
		if _, ok := names[id]; !ok {
			return domain.ErrSupplierNotFound
		}
	}

	return nil
}

// activeParts refuses to complete a receipt with a part that was deactivated
// or removed since the draft was saved.
func (uc *InventoryUseCase) activeParts(ctx context.Context, garageID string, r *domain.GoodsReceipt) error {
	ids := make([]string, 0, len(r.Lines))
	for _, l := range r.Lines {
		ids = append(ids, l.PartID)
	}

	parts, err := uc.parts.Parts(ctx, garageID, ids)
	if err != nil {
		return err
	}

	active := map[string]bool{}
	for _, p := range parts {
		active[p.ID] = p.IsActive
	}

	for _, id := range ids {
		known, ok := active[id]
		if !ok {
			return domain.ErrPartNotFound
		}

		if !known {
			return domain.ErrPartInactive
		}
	}

	return nil
}

// describeReceipts fills the derived part, unit and supplier descriptions.
func (uc *InventoryUseCase) describeReceipts(ctx context.Context, garageID string, receipts ...*domain.GoodsReceipt) error {
	var partIDs, unitIDs, supplierIDList []string

	for _, r := range receipts {
		supplierIDList = append(supplierIDList, supplierIDs(r)...)

		for _, l := range r.Lines {
			partIDs = append(partIDs, l.PartID)

			if l.UnitID != nil {
				unitIDs = append(unitIDs, *l.UnitID)
			}
		}
	}

	parts, err := uc.partsByID(ctx, garageID, partIDs)
	if err != nil {
		return err
	}

	units, err := uc.unitAbbreviations(ctx, garageID, uniq(unitIDs))
	if err != nil {
		return err
	}

	names, err := uc.supplierNames(ctx, garageID, supplierIDList)
	if err != nil {
		return err
	}

	for _, r := range receipts {
		if r.DefaultSupplierID != nil {
			r.DefaultSupplierName = names[*r.DefaultSupplierID]
		}

		for i := range r.Lines {
			l := &r.Lines[i]
			p := parts[l.PartID]
			l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation

			if l.UnitID != nil {
				l.UnitAbbreviation = units[*l.UnitID]
			}

			if l.SupplierID != nil {
				l.SupplierName = names[*l.SupplierID]
			}
		}
	}

	return nil
}

func (uc *InventoryUseCase) partsByID(ctx context.Context, garageID string, ids []string) (map[string]PartInfo, error) {
	ids = uniq(ids)
	out := make(map[string]PartInfo, len(ids))

	if len(ids) == 0 {
		return out, nil
	}

	parts, err := uc.parts.Parts(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	for _, p := range parts {
		out[p.ID] = p
	}

	return out, nil
}

func (uc *InventoryUseCase) supplierNames(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	ids = uniq(ids)
	if len(ids) == 0 {
		return map[string]string{}, nil
	}

	return uc.directory.SupplierNames(ctx, garageID, ids)
}

// receiptDate validates date, or returns today in timezone when empty.
func (uc *InventoryUseCase) receiptDate(date, timezone string) (string, error) {
	if date == "" {
		return uc.localNow(timezone).Format(time.DateOnly), nil
	}

	d, err := domain.ParseDate(date)
	if err != nil {
		return "", err
	}

	return d.Format(time.DateOnly), nil
}

// localNow is the current time in the garage's time zone.
func (uc *InventoryUseCase) localNow(timezone string) time.Time {
	now := uc.now()
	if loc, err := time.LoadLocation(timezone); err == nil {
		now = now.In(loc)
	}

	return now
}

// nextNumber reserves and renders the next document number of docType from
// the garage's numbering template (FR-TEN-05), in the garage's timezone.
func (uc *InventoryUseCase) nextNumber(ctx context.Context, garageID, docType, template, timezone string) (string, error) {
	now := uc.localNow(timezone)

	seq, err := uc.sequences.Next(ctx, garageID, docType, numbering.Period(template, now))
	if err != nil {
		return "", err
	}

	return numbering.Render(template, now, seq), nil
}

func (uc *InventoryUseCase) auditReceipt(ctx context.Context, actorID, action string, before domain.GoodsReceiptStatus, r *domain.GoodsReceipt) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: r.GarageID, Action: action, EntityType: entityGoodsReceipt, EntityID: r.ID,
		Before: map[string]any{keyStatus: before},
		After:  map[string]any{keyNumber: r.Number, keyStatus: r.Status, "total_amount": r.Total(), keyLines: len(r.Lines)},
		IP:     middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}

// lineBaseCost is the line's cost per base unit as a decimal string, nil when
// no cost was recorded.
func lineBaseCost(l *domain.GoodsReceiptLine) (*string, error) {
	if l.UnitCost <= 0 {
		return nil, nil //nolint:nilnil // no cost recorded is not an error
	}

	cost, err := domain.CostPerBaseUnit(l.UnitCost, l.EnteredQuantity, l.Quantity)
	if err != nil {
		return nil, err
	}

	return &cost, nil
}

// enteredRef is the typed amount of a line that used a packaging unit.
func enteredRef(l *domain.GoodsReceiptLine) *string {
	if l.UnitID == nil {
		return nil
	}

	return &l.EnteredQuantity
}

func supplierIDs(r *domain.GoodsReceipt) []string {
	var ids []string

	if r.DefaultSupplierID != nil {
		ids = append(ids, *r.DefaultSupplierID)
	}

	for _, l := range r.Lines {
		if l.SupplierID != nil {
			ids = append(ids, *l.SupplierID)
		}
	}

	return uniq(ids)
}

func uniq(ids []string) []string {
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}

	return out
}

// refOrNil returns a pointer to s, nil for "".
func refOrNil(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// firstRef returns the first non-empty candidate, nil when there is none.
func firstRef(first string, others ...any) *string {
	if first != "" {
		return &first
	}

	for _, o := range others {
		switch v := o.(type) {
		case *string:
			if v != nil && *v != "" {
				return v
			}
		case string:
			if v != "" {
				return &v
			}
		}
	}

	return nil
}
