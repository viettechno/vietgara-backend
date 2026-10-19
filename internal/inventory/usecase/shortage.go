package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

// EventShortageChanged is published when a draft stock issue becomes unable,
// or able again, to be covered by the stock on hand (Release 7.3, FR-INV-15).
// The Repair Order module consumes it to mark the order waiting on parts.
const EventShortageChanged = "stock_issue.shortage_changed"

// draftShortages serves the draft lines that hold one of partIDs, and the
// other parts of the same issues, in reservation order against the stock on
// hand. It returns those lines and the quantity each cannot get.
func (uc *InventoryUseCase) draftShortages(ctx context.Context, garageID string, partIDs []string) ([]domain.DraftLine, map[string]string, error) {
	lines, err := uc.repo.DraftLines(ctx, garageID, partIDs)
	if err != nil || len(lines) == 0 {
		return nil, nil, err
	}

	held := make([]string, 0, len(lines))
	for _, l := range lines {
		held = append(held, l.PartID)
	}

	balances, err := uc.repo.Balances(ctx, garageID, uniq(held))
	if err != nil {
		return nil, nil, err
	}

	onHand := make(map[string]string, len(balances))
	for id, b := range balances {
		onHand[id] = b.OnHand
	}

	return lines, domain.Shortages(lines, onHand), nil
}

// recheckShortage brings the shortage mark of the draft issues that hold one of
// partIDs up to date after the stock on hand or a reservation changed, and
// publishes EventShortageChanged for each mark that flipped.
func (uc *InventoryUseCase) recheckShortage(ctx context.Context, garageID string, partIDs []string) error {
	if len(partIDs) == 0 {
		return nil
	}

	lines, short, err := uc.draftShortages(ctx, garageID, uniq(partIDs))
	if err != nil || len(lines) == 0 {
		return err
	}

	type verdict struct {
		repairOrderID string
		was, now      bool
		affected      bool
	}

	verdicts := map[string]*verdict{}
	order := []string{}

	for _, l := range lines {
		v, ok := verdicts[l.IssueID]
		if !ok {
			v = &verdict{repairOrderID: l.RepairOrderID, was: l.IssueIsShort, affected: l.Affected}
			verdicts[l.IssueID] = v

			order = append(order, l.IssueID)
		}

		if _, isShort := short[l.LineID]; isShort {
			v.now = true
		}
	}

	for _, issueID := range order {
		v := verdicts[issueID]
		if !v.affected || v.was == v.now {
			continue
		}

		if err := uc.markShort(ctx, garageID, issueID, v.repairOrderID, v.now); err != nil {
			return err
		}
	}

	return nil
}

// markShort stores the mark of an issue and tells the Repair Order module.
func (uc *InventoryUseCase) markShort(ctx context.Context, garageID, issueID, repairOrderID string, short bool) error {
	if err := uc.repo.SetIssueShort(ctx, garageID, issueID, short); err != nil {
		return err
	}

	return uc.events.Publish(ctx, EventShortageChanged, garageID, "", map[string]any{
		keyRepairOrderID: repairOrderID, keyGarageID: garageID, "short": short,
	})
}

// recordMovement writes a stock movement and rechecks the draft stock issues
// that hold the part: more stock may cover them, less may leave them short.
func (uc *InventoryUseCase) recordMovement(ctx context.Context, tx *domain.StockTransaction) error {
	if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
		return err
	}

	return uc.recheckShortage(ctx, tx.GarageID, []string{tx.PartID})
}

func linePartIDs(lines []domain.StockIssueLine) []string {
	ids := make([]string, 0, len(lines))
	for _, l := range lines {
		ids = append(ids, l.PartID)
	}

	return ids
}
