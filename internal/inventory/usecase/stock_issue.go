package usecase

import (
	"cmp"
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// ActionStockIssuePosted is the audit action of posting a stock issue.
const (
	ActionStockIssuePosted = "stock_issue.posted"
	entityStockIssue       = "stock_issue"
)

// IssueLineInput is one part line of a repair order, as the repair order
// events carry it.
type IssueLineInput struct {
	RepairOrderLineID string
	PartID            string
	// Quantity is the base-unit amount; UnitID and EnteredQuantity keep the
	// packaging unit the line was typed in.
	Quantity        string
	UnitID          string
	EnteredQuantity string
}

// CreateStockIssueRequest is the input for InventoryUseCase.CreateStockIssue.
type CreateStockIssueRequest struct {
	GarageID          string
	RepairOrderID     string
	RepairOrderNumber string
	Lines             []IssueLineInput
}

// CreateStockIssue creates the numbered draft stock issue of a repair order
// (FR-INV-20) with one line per part line; the draft reserves the parts.
// A repair order that already has an issue, or has no part lines, is left
// alone (a redelivered event is a no-op). A line whose part no longer
// resolves is skipped, like in the former closing deduction.
func (uc *InventoryUseCase) CreateStockIssue(ctx context.Context, req CreateStockIssueRequest) (*domain.StockIssue, error) {
	const op = "create stock issue"

	lines, err := uc.issueLines(ctx, req)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if len(lines) == 0 {
		return nil, nil //nolint:nilnil // an order without part lines has no stock issue
	}

	garage, err := uc.garages.Get(ctx, req.GarageID)
	if err != nil {
		return nil, toAppError(op, err)
	}

	existing, err := uc.repo.FindStockIssueByRepairOrder(ctx, req.GarageID, req.RepairOrderID, false)
	if err == nil {
		return existing, nil
	}

	if !isNotFound(err) {
		return nil, toAppError(op, err)
	}

	number, err := uc.nextNumber(ctx, req.GarageID, docTypeStockIssue, garage.StockIssueNumberTemplate, garage.Timezone)
	if err != nil {
		return nil, toAppError(op, err)
	}

	issue := &domain.StockIssue{
		ID: uuid.NewString(), GarageID: req.GarageID, Number: number, RepairOrderID: req.RepairOrderID,
		RepairOrderNumber: req.RepairOrderNumber, Status: domain.StockIssueDraft, Lines: lines,
	}

	created, err := uc.repo.CreateStockIssue(ctx, issue)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if created {
		if err := uc.recheckShortage(ctx, req.GarageID, linePartIDs(lines)); err != nil {
			return nil, toAppError(op, err)
		}
	}

	return issue, nil
}

// ResyncStockIssue brings the draft stock issue of a repair order in line with
// the order's part lines after they changed (Release 7.3): lines added,
// changed or removed. An order without an issue gets one. A posted or cancelled
// issue is left alone: the parts have left stock, or the order is gone.
func (uc *InventoryUseCase) ResyncStockIssue(ctx context.Context, req CreateStockIssueRequest) error {
	const op = "resync stock issue"

	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, req.GarageID, req.RepairOrderID, true)
	if err != nil {
		if !isNotFound(err) {
			return toAppError(op, err)
		}

		_, err := uc.CreateStockIssue(ctx, req)

		return err
	}

	if issue.Status != domain.StockIssueDraft {
		return nil
	}

	lines, err := uc.issueLines(ctx, req)
	if err != nil {
		return toAppError(op, err)
	}

	affected := append(linePartIDs(issue.Lines), linePartIDs(lines)...)
	issue.Lines = lines

	if err := uc.repo.ReplaceIssueLines(ctx, issue); err != nil {
		return toAppError(op, err)
	}

	if err := uc.recheckShortage(ctx, req.GarageID, affected); err != nil {
		return toAppError(op, err)
	}

	return nil
}

// CancelStockIssue cancels the draft stock issue of a cancelled repair order,
// which releases its reservation (FR-INV-15); other draft issues waiting for
// the same parts may be covered now. A posted issue is left alone.
func (uc *InventoryUseCase) CancelStockIssue(ctx context.Context, garageID, repairOrderID string) error {
	const op = "cancel stock issue"

	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, garageID, repairOrderID, true)
	if err != nil {
		if isNotFound(err) {
			return nil
		}

		return toAppError(op, err)
	}

	if issue.Status != domain.StockIssueDraft {
		return nil
	}

	issue.Status, issue.IsShort = domain.StockIssueCancelled, false

	if err := uc.repo.SaveStockIssue(ctx, issue); err != nil {
		return toAppError(op, err)
	}

	if err := uc.recheckShortage(ctx, garageID, linePartIDs(issue.Lines)); err != nil {
		return toAppError(op, err)
	}

	return nil
}

// issueLines builds the lines of a new issue from the repair order's part
// lines, dropping those with an unknown part or an unusable quantity.
func (uc *InventoryUseCase) issueLines(ctx context.Context, req CreateStockIssueRequest) ([]domain.StockIssueLine, error) {
	ids := make([]string, 0, len(req.Lines))
	for _, l := range req.Lines {
		ids = append(ids, l.PartID)
	}

	parts, err := uc.partsByID(ctx, req.GarageID, ids)
	if err != nil {
		return nil, err
	}

	lines := make([]domain.StockIssueLine, 0, len(req.Lines))

	for _, in := range req.Lines {
		if _, ok := parts[in.PartID]; !ok || in.RepairOrderLineID == "" {
			continue
		}

		qty, err := domain.ParseQuantity(in.Quantity)
		if err != nil {
			continue
		}

		line := domain.StockIssueLine{
			ID: uuid.NewString(), RepairOrderLineID: in.RepairOrderLineID, PartID: in.PartID, Quantity: qty, EnteredQuantity: qty,
		}

		if in.UnitID != "" && in.EnteredQuantity != "" {
			line.UnitID, line.EnteredQuantity = &in.UnitID, in.EnteredQuantity
		}

		lines = append(lines, line)
	}

	return lines, nil
}

// GetRepairOrderStockIssue returns the repair order's stock issue.
func (uc *InventoryUseCase) GetRepairOrderStockIssue(ctx context.Context, garageID, repairOrderID string) (*domain.StockIssue, error) {
	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, garageID, repairOrderID, false)
	if err != nil {
		return nil, toAppError("get stock issue", err)
	}

	if err := uc.describeIssue(ctx, garageID, issue); err != nil {
		return nil, toAppError("get stock issue", err)
	}

	return issue, nil
}

// StockIssuePosted reports whether the repair order's stock issue is posted,
// so its parts have left stock; an order without an issue is not posted. The
// Repair Order module asks before it lets the order's parts change.
func (uc *InventoryUseCase) StockIssuePosted(ctx context.Context, garageID, repairOrderID string) (bool, error) {
	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, garageID, repairOrderID, false)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}

		return false, toAppError("get stock issue", err)
	}

	return issue.Status == domain.StockIssuePosted, nil
}

// PostStockIssue posts a draft issue now ("issue now"). Any other status is
// refused with domain.ErrStockIssueNotDraft.
func (uc *InventoryUseCase) PostStockIssue(ctx context.Context, garageID, issueID, actorID string) (*domain.StockIssue, error) {
	const op = "post stock issue"

	issue, err := uc.repo.FindStockIssue(ctx, garageID, issueID, true)
	if err != nil {
		return nil, toAppError(op, err)
	}

	if issue.Status != domain.StockIssueDraft {
		return nil, toAppError(op, domain.ErrStockIssueNotDraft)
	}

	if err := uc.post(ctx, issue, actorID); err != nil {
		return nil, toAppError(op, err)
	}

	if err := uc.describeIssue(ctx, garageID, issue); err != nil {
		return nil, toAppError(op, err)
	}

	return issue, nil
}

// postIfDraft posts the issue of a repair order when it is still a draft; an
// order without an issue, or with one already posted, is left alone, so
// redelivered events are no-ops.
func (uc *InventoryUseCase) postIfDraft(ctx context.Context, garageID, repairOrderID string) error {
	issue, err := uc.repo.FindStockIssueByRepairOrder(ctx, garageID, repairOrderID, true)
	if err != nil {
		if isNotFound(err) {
			return nil
		}

		return err
	}

	if issue.Status != domain.StockIssueDraft {
		return nil
	}

	return uc.post(ctx, issue, "")
}

// post writes one OUT movement per part (the quantities of the issue's lines
// summed), stamps each line with the average cost at that moment and marks the
// issue posted. Stock may go negative: the parts have already been used.
func (uc *InventoryUseCase) post(ctx context.Context, issue *domain.StockIssue, actorID string) error {
	quantities := map[string]string{}

	for _, l := range issue.Lines {
		quantities[l.PartID] = domain.Add(quantities[l.PartID], l.Quantity)
	}

	partIDs := make([]string, 0, len(quantities))
	for id := range quantities {
		partIDs = append(partIDs, id)
	}

	slices.Sort(partIDs)

	costs := map[string]*string{}

	for _, id := range partIDs {
		tx := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: issue.GarageID, PartID: id, Type: domain.TransactionOut, Quantity: quantities[id],
			RefType: domain.RefStockIssue, RefID: &issue.ID, CreatedBy: actorRef(actorID),
		}

		if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
			return err
		}

		costs[id] = tx.UnitCost
	}

	for i := range issue.Lines {
		issue.Lines[i].UnitCost = costs[issue.Lines[i].PartID]
	}

	now := uc.now()
	wasShort := issue.IsShort
	issue.Status, issue.PostedAt, issue.PostedBy, issue.UpdatedBy = domain.StockIssuePosted, &now, actorRef(actorID), actorRef(actorID)
	issue.IsShort = false

	if err := uc.repo.SaveStockIssue(ctx, issue); err != nil {
		return err
	}

	if wasShort {
		if err := uc.events.Publish(ctx, EventShortageChanged, issue.GarageID, "", map[string]any{
			keyRepairOrderID: issue.RepairOrderID, keyGarageID: issue.GarageID, "short": false,
		}); err != nil {
			return err
		}
	}

	if err := uc.recheckShortage(ctx, issue.GarageID, partIDs); err != nil {
		return err
	}

	err := uc.audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: issue.GarageID, Action: ActionStockIssuePosted, EntityType: entityStockIssue, EntityID: issue.ID,
		Before: map[string]any{keyStatus: domain.StockIssueDraft},
		After:  map[string]any{keyNumber: issue.Number, keyRepairOrderID: issue.RepairOrderID, keyStatus: issue.Status, keyLines: len(issue.Lines)},
		IP:     middleware.ClientIP(ctx),
	})
	if err != nil {
		return err
	}

	return uc.events.Publish(ctx, EventStockIssued, issue.GarageID, actorID, map[string]any{
		"stock_issue_id": issue.ID, keyGarageID: issue.GarageID, keyRepairOrderID: issue.RepairOrderID,
		keyLines: issuedLines(partIDs, quantities),
	})
}

func issuedLines(partIDs []string, quantities map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(partIDs))

	for _, id := range partIDs {
		out = append(out, map[string]string{"part_id": id, "quantity": quantities[id]})
	}

	slices.SortFunc(out, func(a, b map[string]string) int { return cmp.Compare(a["part_id"], b["part_id"]) })

	return out
}

// describeIssue fills the derived part, unit and poster descriptions.
func (uc *InventoryUseCase) describeIssue(ctx context.Context, garageID string, issue *domain.StockIssue) error {
	partIDs := make([]string, 0, len(issue.Lines))
	unitIDs := []string{}

	for _, l := range issue.Lines {
		partIDs = append(partIDs, l.PartID)

		if l.UnitID != nil {
			unitIDs = append(unitIDs, *l.UnitID)
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

	short := map[string]string{}

	if issue.Status == domain.StockIssueDraft {
		if _, short, err = uc.draftShortages(ctx, garageID, uniq(partIDs)); err != nil {
			return err
		}
	}

	for i := range issue.Lines {
		l := &issue.Lines[i]
		p := parts[l.PartID]
		l.ShortQuantity = orDefault(short[l.ID], "0")
		l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation

		if l.UnitID != nil {
			l.UnitAbbreviation = units[*l.UnitID]
		}
	}

	if issue.PostedBy != nil {
		if issue.PostedByName, err = uc.directory.UserName(ctx, *issue.PostedBy); err != nil {
			return err
		}
	}

	return nil
}
