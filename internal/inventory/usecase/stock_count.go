package usecase

import (
	"context"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// Audit actions and document type of the stock count.
const (
	ActionStockCountCompleted = "stock_count.completed"
	ActionStockCountCancelled = "stock_count.cancelled"
	ActionStockCountMerged    = "stock_count.merged"
	entityStockCount          = "stock_count"

	docTypeStockCount = "stock_count"
)

// CreateStockCountRequest is the input for InventoryUseCase.CreateStockCount.
type CreateStockCountRequest struct {
	GarageID string
	ActorID  string
	Name     string
	Notes    string
}

// CreateStockCount saves a new, empty draft stock count (FR-INV-12) with a
// number from the garage's numbering template; lines are added afterwards.
func (uc *InventoryUseCase) CreateStockCount(ctx context.Context, req CreateStockCountRequest) (*domain.StockCount, error) {
	const op = "create stock count"

	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > domain.MaxNameLength {
		return nil, toAppError(op, domain.ErrNameTooLong)
	}

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeStockCount, garage.StockCountNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	c := &domain.StockCount{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, Name: name, Notes: req.Notes,
		Status: domain.StockCountDraft, CreatedBy: actorRef(req.ActorID), UpdatedBy: actorRef(req.ActorID),
	}

	if err := uc.repo.CreateStockCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeCounts(ctx, req.GarageID, c); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// GetStockCount returns one stock count with its lines.
func (uc *InventoryUseCase) GetStockCount(ctx context.Context, garageID, countID string) (*domain.StockCount, error) {
	const op = "get stock count"

	c, err := uc.repo.FindStockCount(ctx, garageID, countID, false)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// ListStockCountsRequest is the input for InventoryUseCase.ListStockCounts.
type ListStockCountsRequest struct {
	PageRequest

	GarageID string
	Query    string
	Status   domain.StockCountStatus
}

// ListStockCounts returns one page of the garage's stock counts, newest first,
// without lines.
func (uc *InventoryUseCase) ListStockCounts(ctx context.Context, req ListStockCountsRequest) (*Page[*domain.StockCount], error) {
	page := req.normalize()

	items, total, err := uc.repo.ListStockCounts(ctx, req.GarageID, StockCountFilter{
		Query: req.Query, Status: req.Status, Limit: page.PageSize, Offset: page.offset(),
	})
	if err != nil {
		return nil, toAppError("list stock counts", err)
	}

	if err := uc.describeCounts(ctx, req.GarageID, items...); err != nil {
		return nil, toAppError("list stock counts", err)
	}

	return &Page[*domain.StockCount]{Items: items, Total: total, Page: page.Page, PageSize: page.PageSize}, nil
}

// UpdateStockCountRequest is the input for InventoryUseCase.UpdateStockCount;
// nil fields keep their value.
type UpdateStockCountRequest struct {
	GarageID string
	CountID  string
	ActorID  string
	Name     *string
	Notes    *string
}

// UpdateStockCount changes the name and notes of a draft stock count.
func (uc *InventoryUseCase) UpdateStockCount(ctx context.Context, req UpdateStockCountRequest) (*domain.StockCount, error) {
	const op = "update stock count"

	c, err := uc.draftCount(ctx, req.GarageID, req.CountID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if len([]rune(name)) > domain.MaxNameLength {
			return nil, toAppError(op, domain.ErrNameTooLong)
		}

		c.Name = name
	}

	if req.Notes != nil {
		c.Notes = *req.Notes
	}

	if err := uc.saveCount(ctx, c, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// AddStockCountLinesRequest is the input for InventoryUseCase.AddStockCountLines.
type AddStockCountLinesRequest struct {
	GarageID string
	CountID  string
	ActorID  string
	// PartIDs are parts picked one by one; CategoryID adds the active parts of
	// a category (and its sub-categories) that have a stock balance.
	PartIDs    []string
	CategoryID string
}

// AddStockCountLines adds parts to a draft count, snapshotting the system
// quantity of each; parts already counted are skipped.
func (uc *InventoryUseCase) AddStockCountLines(ctx context.Context, req AddStockCountLinesRequest) (*domain.StockCount, error) {
	const op = "add stock count lines"

	c, err := uc.draftCount(ctx, req.GarageID, req.CountID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	ids, err := uc.partsToCount(ctx, req)
	if err != nil {
		return nil, toAppError(op, err)
	}

	fresh := make([]string, 0, len(ids))

	for _, id := range ids {
		if c.LineByPart(id) < 0 {
			fresh = append(fresh, id)
		}
	}

	if len(fresh) > 0 {
		balances, err := uc.repo.Balances(ctx, req.GarageID, fresh)
		if err != nil {
			return nil, toAppError(op, err)
		}

		next := nextPosition(c.Lines)
		added := make([]domain.StockCountLine, 0, len(fresh))

		for i, id := range fresh {
			bal := balances[id]
			added = append(added, domain.StockCountLine{
				ID: uuid.NewString(), Position: next + i, PartID: id, SystemQuantity: orDefault(bal.OnHand, "0"),
				AverageCost: new(orDefault(bal.AverageCost, "0")),
			})
		}

		if err := uc.repo.InsertStockCountLines(ctx, req.GarageID, c.ID, actorRef(req.ActorID), added); err != nil {
			return nil, toAppError(op, err)
		}

		c.Lines = append(c.Lines, added...)
	}

	if err := uc.saveCount(ctx, c, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// partsToCount resolves the parts an AddStockCountLines request names: the
// listed ones must exist and be active; a category adds the stocked ones.
func (uc *InventoryUseCase) partsToCount(ctx context.Context, req AddStockCountLinesRequest) ([]string, error) {
	ids := uniq(req.PartIDs)

	if len(ids) == 0 && req.CategoryID == "" {
		return nil, domain.ErrNoParts
	}

	if len(ids) > 0 {
		parts, err := uc.partsByID(ctx, req.GarageID, ids)
		if err != nil {
			return nil, err
		}

		for _, id := range ids {
			p, ok := parts[id]
			if !ok {
				return nil, domain.ErrPartNotFound
			}

			if !p.IsActive {
				return nil, domain.ErrPartInactive
			}
		}
	}

	if req.CategoryID == "" {
		return ids, nil
	}

	inCategory, err := uc.stockedPartsOf(ctx, req.GarageID, req.CategoryID)
	if err != nil {
		return nil, err
	}

	for _, id := range inCategory {
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// stockedPartsOf lists the active parts of a category, sub-categories
// included, that have a stock balance or a reservation.
func (uc *InventoryUseCase) stockedPartsOf(ctx context.Context, garageID, categoryID string) ([]string, error) {
	var stocked []string

	query := PartQuery{CategoryID: categoryID, Limit: MaxPageSize}

	for query.Offset = 0; query.Offset < maxLowStockScan; query.Offset += MaxPageSize {
		parts, total, err := uc.parts.SearchParts(ctx, garageID, query)
		if err != nil {
			return nil, err
		}

		if len(parts) > 0 {
			balances, err := uc.repo.Balances(ctx, garageID, partIDs(parts))
			if err != nil {
				return nil, err
			}

			for _, p := range parts {
				if _, ok := balances[p.ID]; ok {
					stocked = append(stocked, p.ID)
				}
			}
		}

		if query.Offset+MaxPageSize >= total {
			break
		}
	}

	return stocked, nil
}

// UpdateStockCountLineRequest is the input for InventoryUseCase.UpdateStockCountLine.
type UpdateStockCountLineRequest struct {
	GarageID string
	CountID  string
	LineID   string
	ActorID  string
	// UnitID is the packaging unit EnteredCounted is typed in; empty for the
	// base unit.
	UnitID string
	// EnteredCounted is the counted quantity; empty marks the line uncounted.
	EnteredCounted string
}

// UpdateStockCountLine types the counted quantity of one line of a draft
// count and takes a fresh snapshot of the part's system quantity.
func (uc *InventoryUseCase) UpdateStockCountLine(ctx context.Context, req UpdateStockCountLineRequest) (*domain.StockCount, error) {
	const op = "update stock count line"

	c, err := uc.draftCount(ctx, req.GarageID, req.CountID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	i := c.LineByID(req.LineID)
	if i < 0 {
		return nil, toAppError(op, domain.ErrStockCountLineNotFound)
	}

	l := &c.Lines[i]

	if strings.TrimSpace(req.EnteredCounted) == "" {
		l.UnitID, l.EnteredCounted, l.Counted, l.CountedAt = nil, nil, nil, nil
	} else if err := uc.countLine(ctx, req, l); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.SaveStockCountLine(ctx, req.GarageID, c.ID, l); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.saveCount(ctx, c, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// countLine records the typed quantity on the line, converted to the base
// unit, with a fresh snapshot of the system quantity and average cost.
func (uc *InventoryUseCase) countLine(ctx context.Context, req UpdateStockCountLineRequest, l *domain.StockCountLine) error {
	entered, err := domain.ParseCountedQuantity(req.EnteredCounted)
	if err != nil {
		return err
	}

	if domain.IsZero(entered) {
		// Nothing counted is nothing in every unit.
		l.UnitID, l.EnteredCounted, l.Counted = nil, new("0"), new("0")
	} else {
		resolved, err := uc.parts.ResolveQuantity(ctx, req.GarageID, l.PartID, req.UnitID, entered)
		if err != nil {
			return err
		}

		l.UnitID, l.Counted = resolved.UnitID, new(resolved.Base)
		l.EnteredCounted = new(resolved.Base)

		if resolved.Entered != nil {
			l.EnteredCounted = resolved.Entered
		}
	}

	balances, err := uc.repo.Balances(ctx, req.GarageID, []string{l.PartID})
	if err != nil {
		return err
	}

	bal := balances[l.PartID]
	l.SystemQuantity, l.AverageCost = orDefault(bal.OnHand, "0"), new(orDefault(bal.AverageCost, "0"))
	l.CountedAt = new(uc.now())

	return nil
}

// DeleteStockCountLine removes one line from a draft count.
func (uc *InventoryUseCase) DeleteStockCountLine(ctx context.Context, garageID, countID, lineID, actorID string) (*domain.StockCount, error) {
	const op = "delete stock count line"

	c, err := uc.draftCount(ctx, garageID, countID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	i := c.LineByID(lineID)
	if i < 0 {
		return nil, toAppError(op, domain.ErrStockCountLineNotFound)
	}

	if err := uc.repo.DeleteStockCountLine(ctx, garageID, countID, lineID); err != nil {
		return nil, toAppError(op, err)
	}

	c.Lines = slices.Delete(c.Lines, i, i+1)

	if err := uc.saveCount(ctx, c, actorID); err != nil {
		return nil, toAppError(op, err)
	}

	return c, nil
}

// MergeStockCountsRequest is the input for InventoryUseCase.MergeStockCounts.
type MergeStockCountsRequest struct {
	GarageID string
	ActorID  string
	// CountIDs are at least two draft counts; the first is kept.
	CountIDs []string
}

// MergeStockCounts folds draft counts into the first one: the lines move over,
// a part counted on several adds up its counted quantities (two people counted
// different places), and the other counts are cancelled.
func (uc *InventoryUseCase) MergeStockCounts(ctx context.Context, req MergeStockCountsRequest) (*domain.StockCount, error) {
	const op = "merge stock counts"

	ids := uniq(req.CountIDs)
	if len(ids) < 2 {
		return nil, toAppError(op, domain.ErrInvalidMerge)
	}

	// Locking in a fixed order keeps two merges from deadlocking.
	byID := make(map[string]*domain.StockCount, len(ids))

	for _, id := range slices.Sorted(slices.Values(ids)) {
		c, err := uc.draftCount(ctx, req.GarageID, id)
		if err != nil {
			return nil, toAppError(op, err)
		}

		byID[id] = c
	}

	target := byID[ids[0]]
	moved := []domain.StockCountLine{}
	changed := map[int]bool{}
	next := nextPosition(target.Lines)

	for _, id := range ids[1:] {
		source := byID[id]

		for _, l := range source.Lines {
			if i := target.LineByPart(l.PartID); i >= 0 {
				mergeLine(&target.Lines[i], l)
				changed[i] = true

				continue
			}

			l.Position = next + len(moved)
			moved = append(moved, l)
		}
	}

	if err := uc.cancelMerged(ctx, byID, ids, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.repo.InsertStockCountLines(ctx, req.GarageID, target.ID, actorRef(req.ActorID), moved); err != nil {
		return nil, toAppError(op, err)
	}

	for i := range changed {
		if err := uc.repo.SaveStockCountLine(ctx, req.GarageID, target.ID, &target.Lines[i]); err != nil {
			return nil, toAppError(op, err)
		}
	}

	target.Lines = append(target.Lines, moved...)

	if err := uc.saveCount(ctx, target, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	err := uc.audit.Record(ctx, audit.Entry{
		Actor: req.ActorID, GarageID: req.GarageID, Action: ActionStockCountMerged, EntityType: entityStockCount, EntityID: target.ID,
		After: map[string]any{keyNumber: target.Number, "merged": len(ids) - 1, keyLines: len(target.Lines)},
		IP:    middleware.ClientIP(ctx),
	})
	if err != nil {
		return nil, toAppError(op, err)
	}

	return target, nil
}

// cancelMerged empties and cancels every merged count but the first.
func (uc *InventoryUseCase) cancelMerged(ctx context.Context, byID map[string]*domain.StockCount, ids []string, actorID string) error {
	target := byID[ids[0]]
	now := uc.now()

	for _, id := range ids[1:] {
		source := byID[id]

		if err := uc.repo.ClearStockCountLines(ctx, source.GarageID, source.ID); err != nil {
			return err
		}

		source.Lines, source.Status, source.CancelledAt, source.UpdatedBy = nil, domain.StockCountCancelled, &now, actorRef(actorID)
		source.Notes = strings.TrimSpace(source.Notes + "\nMerged into " + target.Number)

		if err := uc.repo.SaveStockCount(ctx, source); err != nil {
			return err
		}
	}

	return nil
}

// mergeLine adds the count of src to dst, the line of the same part.
func mergeLine(dst *domain.StockCountLine, src domain.StockCountLine) {
	switch {
	case src.Counted == nil:
		return
	case dst.Counted == nil:
		dst.UnitID, dst.EnteredCounted, dst.Counted = src.UnitID, src.EnteredCounted, src.Counted
		dst.SystemQuantity, dst.AverageCost, dst.CountedAt = src.SystemQuantity, src.AverageCost, src.CountedAt
	default:
		sum := domain.Add(*dst.Counted, *src.Counted)
		dst.UnitID, dst.EnteredCounted, dst.Counted = nil, &sum, &sum

		// The later count saw the later system quantity.
		if src.CountedAt != nil && (dst.CountedAt == nil || src.CountedAt.After(*dst.CountedAt)) {
			dst.SystemQuantity, dst.AverageCost, dst.CountedAt = src.SystemQuantity, src.AverageCost, src.CountedAt
		}
	}
}

// CompleteStockCountRequest is the input for InventoryUseCase.CompleteStockCount.
type CompleteStockCountRequest struct {
	GarageID string
	CountID  string
	ActorID  string
	// SkipUncounted drops the lines nobody counted instead of refusing.
	SkipUncounted bool
}

// CompleteStockCount posts the adjustments of a draft count (FR-INV-12): each
// counted line gets a fresh snapshot of the system quantity and, when it
// differs from the count, one IN or OUT movement that makes stock equal the
// count. The difference is valued at the average cost of the moment, and the
// count cannot be edited afterwards.
func (uc *InventoryUseCase) CompleteStockCount(ctx context.Context, req CompleteStockCountRequest) (*domain.StockCount, error) {
	const op = "complete stock count"

	c, err := uc.draftCount(ctx, req.GarageID, req.CountID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.dropUncounted(ctx, c, req.SkipUncounted); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.postCount(ctx, c, req.ActorID); err != nil {
		return nil, toAppError(op, err)
	}

	now := uc.now()
	c.Status, c.CompletedAt, c.CompletedBy, c.UpdatedBy = domain.StockCountCompleted, &now, actorRef(req.ActorID), actorRef(req.ActorID)

	if err := uc.repo.SaveStockCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditCount(ctx, req.ActorID, ActionStockCountCompleted, domain.StockCountDraft, c); err != nil {
		return nil, err
	}

	return c, nil
}

// dropUncounted refuses a count with uncounted lines, or removes them when
// skip is set; a count left without lines is refused.
func (uc *InventoryUseCase) dropUncounted(ctx context.Context, c *domain.StockCount, skip bool) error {
	counted := make([]domain.StockCountLine, 0, len(c.Lines))

	for _, l := range c.Lines {
		if l.Counted != nil {
			counted = append(counted, l)

			continue
		}

		if !skip {
			return domain.ErrStockCountUncounted
		}

		if err := uc.repo.DeleteStockCountLine(ctx, c.GarageID, c.ID, l.ID); err != nil {
			return err
		}
	}

	if len(counted) == 0 {
		return domain.ErrStockCountEmpty
	}

	c.Lines = counted

	return nil
}

// postCount snapshots the system quantity and average cost of every counted
// line and writes the movement that makes stock equal the count.
func (uc *InventoryUseCase) postCount(ctx context.Context, c *domain.StockCount, actorID string) error {
	ids := make([]string, 0, len(c.Lines))
	for _, l := range c.Lines {
		ids = append(ids, l.PartID)
	}

	if err := uc.repo.LockBalances(ctx, c.GarageID, ids); err != nil {
		return err
	}

	balances, err := uc.repo.Balances(ctx, c.GarageID, ids)
	if err != nil {
		return err
	}

	for i := range c.Lines {
		l := &c.Lines[i]
		bal := balances[l.PartID]
		l.SystemQuantity, l.AverageCost = orDefault(bal.OnHand, "0"), new(orDefault(bal.AverageCost, "0"))

		if err := uc.adjustTo(ctx, c, l, actorID); err != nil {
			return err
		}

		if err := uc.repo.SaveStockCountLine(ctx, c.GarageID, c.ID, l); err != nil {
			return err
		}
	}

	return nil
}

// adjustTo writes the movement that takes the part from the line's system
// quantity to its counted quantity; nothing when they already match.
func (uc *InventoryUseCase) adjustTo(ctx context.Context, c *domain.StockCount, l *domain.StockCountLine, actorID string) error {
	diff, _ := l.Difference()
	if domain.IsZero(diff) {
		return nil
	}

	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: c.GarageID, PartID: l.PartID, Type: domain.TransactionIn, Quantity: diff,
		RefType: domain.RefStockCount, RefID: &c.ID, CreatedBy: actorRef(actorID),
	}

	if domain.Less(*l.Counted, l.SystemQuantity) {
		tx.Type, tx.Quantity = domain.TransactionOut, domain.Subtract(l.SystemQuantity, *l.Counted)
	}

	return uc.repo.RecordTransaction(ctx, tx)
}

// CancelStockCount closes a draft count, or reverses a completed one: the
// adjustments are undone, which is refused with
// domain.ErrInsufficientStockToReverse when stock added by the count has been
// used since. A cancelled count cannot be cancelled again.
func (uc *InventoryUseCase) CancelStockCount(ctx context.Context, garageID, countID, actorID string) (*domain.StockCount, error) {
	const op = "cancel stock count"

	c, err := uc.repo.FindStockCount(ctx, garageID, countID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	before := c.Status

	switch before {
	case domain.StockCountCancelled:
		return nil, toAppError(op, domain.ErrStockCountAlreadyCancelled)
	case domain.StockCountCompleted:
		if err := uc.reverseCount(ctx, c, actorID); err != nil {
			return nil, toAppError(op, err)
		}
	case domain.StockCountDraft:
	}

	now := uc.now()
	c.Status, c.CancelledAt, c.UpdatedBy = domain.StockCountCancelled, &now, actorRef(actorID)

	if err := uc.repo.SaveStockCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.finishCount(ctx, c); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.auditCount(ctx, actorID, ActionStockCountCancelled, before, c); err != nil {
		return nil, err
	}

	return c, nil
}

// reverseCount undoes the adjustment of every line of a completed count,
// after checking that stock on hand still covers what the count added.
func (uc *InventoryUseCase) reverseCount(ctx context.Context, c *domain.StockCount, actorID string) error {
	ids := make([]string, 0, len(c.Lines))
	for _, l := range c.Lines {
		ids = append(ids, l.PartID)
	}

	if err := uc.repo.LockBalances(ctx, c.GarageID, ids); err != nil {
		return err
	}

	balances, err := uc.repo.Balances(ctx, c.GarageID, ids)
	if err != nil {
		return err
	}

	for i := range c.Lines {
		l := &c.Lines[i]
		diff, _ := l.Difference()

		if domain.Less("0", diff) && domain.Less(orDefault(balances[l.PartID].OnHand, "0"), diff) {
			return domain.ErrInsufficientStockToReverse
		}
	}

	for i := range c.Lines {
		l := &c.Lines[i]
		diff, _ := l.Difference()

		if domain.IsZero(diff) {
			continue
		}

		tx := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: c.GarageID, PartID: l.PartID, Type: domain.TransactionOut, Quantity: diff,
			RefType: domain.RefStockCount, RefID: &c.ID, CreatedBy: actorRef(actorID),
		}

		if domain.Less(diff, "0") {
			// The count removed stock: put it back at the cost it left with.
			tx.Type, tx.Quantity, tx.UnitCost = domain.TransactionIn, domain.Subtract("0", diff), costOrNil(l.AverageCost)
		}

		if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
			return err
		}
	}

	return nil
}

// draftCount locks and returns a count that must still be a draft.
func (uc *InventoryUseCase) draftCount(ctx context.Context, garageID, countID string) (*domain.StockCount, error) {
	c, err := uc.repo.FindStockCount(ctx, garageID, countID, true)
	if err != nil {
		return nil, err
	}

	if c.Status != domain.StockCountDraft {
		return nil, domain.ErrStockCountNotDraft
	}

	return c, nil
}

// saveCount persists the header of a changed draft and finishes it for the
// answer.
func (uc *InventoryUseCase) saveCount(ctx context.Context, c *domain.StockCount, actorID string) error {
	c.UpdatedBy = actorRef(actorID)

	if err := uc.repo.SaveStockCount(ctx, c); err != nil {
		return err
	}

	return uc.finishCount(ctx, c)
}

// finishCount fills the derived descriptions and the summary of a count that
// carries its lines.
func (uc *InventoryUseCase) finishCount(ctx context.Context, c *domain.StockCount) error {
	if err := uc.describeCounts(ctx, c.GarageID, c); err != nil {
		return err
	}

	c.Summarise()

	return nil
}

// describeCounts fills the derived part, unit and user descriptions.
func (uc *InventoryUseCase) describeCounts(ctx context.Context, garageID string, counts ...*domain.StockCount) error {
	var partIDList, unitIDs, userIDs []string

	for _, c := range counts {
		userIDs = append(userIDs, derefAll(c.CreatedBy, c.CompletedBy)...)

		for _, l := range c.Lines {
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

	users, err := uc.userNames(ctx, userIDs)
	if err != nil {
		return err
	}

	for _, c := range counts {
		c.CreatedByName, c.CompletedByName = nameOf(users, c.CreatedBy), nameOf(users, c.CompletedBy)

		for i := range c.Lines {
			l := &c.Lines[i]
			p := parts[l.PartID]
			l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation

			if l.UnitID != nil {
				l.UnitAbbreviation = units[*l.UnitID]
			}
		}
	}

	return nil
}

func (uc *InventoryUseCase) auditCount(ctx context.Context, actorID, action string, before domain.StockCountStatus, c *domain.StockCount) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: c.GarageID, Action: action, EntityType: entityStockCount, EntityID: c.ID,
		Before: map[string]any{keyStatus: before},
		After: map[string]any{
			keyNumber: c.Number, keyStatus: c.Status, keyLines: len(c.Lines), "over_lines": c.Tally.Over,
			"short_lines": c.Tally.Short, "total_difference_value": c.TotalDifferenceValue,
		},
		IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}

// nextPosition is the position after the last of the lines.
func nextPosition(lines []domain.StockCountLine) int {
	next := 0

	for _, l := range lines {
		next = max(next, l.Position+1)
	}

	return next
}

// costOrNil returns the cost, nil when it is absent or zero, so that putting
// stock back never drags the average cost towards zero.
func costOrNil(cost *string) *string {
	if cost == nil || domain.IsZero(*cost) {
		return nil
	}

	return cost
}

// derefAll returns the non-nil ids.
func derefAll(ids ...*string) []string {
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		if id != nil {
			out = append(out, *id)
		}
	}

	return out
}

// nameOf returns the name of the user id points to, "" for nil.
func nameOf(names map[string]string, id *string) string {
	if id == nil {
		return ""
	}

	return names[*id]
}

// userNames returns the name per user id, asking the directory once per id.
func (uc *InventoryUseCase) userNames(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}

	for _, id := range uniq(ids) {
		name, err := uc.directory.UserName(ctx, id)
		if err != nil {
			return nil, err
		}

		out[id] = name
	}

	return out, nil
}
