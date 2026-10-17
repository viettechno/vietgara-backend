package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// AdjustmentLineInput is one line typed on a write-off or a return to
// supplier.
type AdjustmentLineInput struct {
	PartID string
	// UnitID is the packaging unit EnteredQuantity is typed in; empty for the
	// base unit.
	UnitID          string
	EnteredQuantity string
	// UnitCost is the credit of a return to supplier, VND per entered unit; 0
	// takes the default. Unused by write-offs.
	UnitCost int64
}

// buildAdjustmentLines turns the typed lines into stored ones: every part must
// exist and each quantity is converted to the base unit by the Catalogue.
func (uc *InventoryUseCase) buildAdjustmentLines(ctx context.Context, garageID string, inputs []AdjustmentLineInput) ([]domain.AdjustmentLine, error) {
	if len(inputs) == 0 {
		return nil, domain.ErrNoLines
	}

	ids := make([]string, 0, len(inputs))
	for _, in := range inputs {
		ids = append(ids, in.PartID)
	}

	parts, err := uc.partsByID(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	lines := make([]domain.AdjustmentLine, 0, len(inputs))

	for i, in := range inputs {
		if _, ok := parts[in.PartID]; !ok {
			return nil, domain.ErrPartNotFound
		}

		resolved, err := uc.parts.ResolveQuantity(ctx, garageID, in.PartID, in.UnitID, in.EnteredQuantity)
		if err != nil {
			return nil, err
		}

		entered := resolved.Base
		if resolved.Entered != nil {
			entered = *resolved.Entered
		}

		if in.UnitCost < 0 {
			return nil, domain.ErrInvalidCost
		}

		lines = append(lines, domain.AdjustmentLine{
			ID: uuid.NewString(), Position: i, PartID: in.PartID, UnitID: resolved.UnitID, EnteredQuantity: entered,
			Quantity: resolved.Base, CreditUnitCost: in.UnitCost,
		})
	}

	return lines, nil
}

// takeStock checks that stock on hand covers the lines (several lines of one
// part add up), locking the balances so the check holds until the movements are
// written, and returns the balances.
func (uc *InventoryUseCase) takeStock(ctx context.Context, garageID string, lines []domain.AdjustmentLine) (map[string]Balance, error) {
	need := map[string]string{}
	ids := []string{}

	for _, l := range lines {
		if _, ok := need[l.PartID]; !ok {
			ids = append(ids, l.PartID)
		}

		need[l.PartID] = domain.Add(need[l.PartID], l.Quantity)
	}

	if err := uc.repo.LockBalances(ctx, garageID, ids); err != nil {
		return nil, err
	}

	balances, err := uc.repo.Balances(ctx, garageID, ids)
	if err != nil {
		return nil, err
	}

	for id, qty := range need {
		if domain.Less(orDefault(balances[id].OnHand, "0"), qty) {
			return nil, domain.ErrInsufficientStock
		}
	}

	return balances, nil
}

// movement is what posts or undoes the lines of a document.
type movement struct {
	garageID string
	refType  domain.RefType
	refID    string
	reason   string
	actorID  string
}

// takeOutLines writes one OUT movement per line and keeps the average cost the
// stock left at on the line.
func (uc *InventoryUseCase) takeOutLines(ctx context.Context, m movement, lines []domain.AdjustmentLine) error {
	for i := range lines {
		l := &lines[i]

		tx := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: m.garageID, PartID: l.PartID, Type: domain.TransactionOut, Quantity: l.Quantity,
			UnitID: l.UnitID, EnteredQuantity: enteredOf(l), RefType: m.refType, RefID: &m.refID, Reason: m.reason,
			CreatedBy: actorRef(m.actorID),
		}

		if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
			return err
		}

		l.AverageCost = tx.UnitCost
	}

	return nil
}

// putBackLines puts the stock of the lines back with one IN movement each, at
// the average cost it left with.
func (uc *InventoryUseCase) putBackLines(ctx context.Context, m movement, lines []domain.AdjustmentLine) error {
	for i := range lines {
		l := &lines[i]

		tx := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: m.garageID, PartID: l.PartID, Type: domain.TransactionIn, Quantity: l.Quantity,
			UnitID: l.UnitID, EnteredQuantity: enteredOf(l), UnitCost: costOrNil(l.AverageCost), RefType: m.refType, RefID: &m.refID,
			Reason: m.reason, CreatedBy: actorRef(m.actorID),
		}

		if err := uc.repo.RecordTransaction(ctx, tx); err != nil {
			return err
		}
	}

	return nil
}

// enteredOf is the typed amount of a line that used a packaging unit.
func enteredOf(l *domain.AdjustmentLine) *string {
	if l.UnitID == nil {
		return nil
	}

	return &l.EnteredQuantity
}

// describeAdjustmentLines fills the derived part and unit descriptions of the
// lines of several documents.
func (uc *InventoryUseCase) describeAdjustmentLines(ctx context.Context, garageID string, docs ...[]domain.AdjustmentLine) error {
	var partIDList, unitIDs []string

	for _, lines := range docs {
		for _, l := range lines {
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

	for _, lines := range docs {
		for i := range lines {
			l := &lines[i]
			p := parts[l.PartID]
			l.PartSKU, l.PartName, l.BaseUnitAbbreviation = p.SKU, p.Name, p.BaseUnitAbbreviation

			if l.UnitID != nil {
				l.UnitAbbreviation = units[*l.UnitID]
			}
		}
	}

	return nil
}

func (uc *InventoryUseCase) auditAdjustment(ctx context.Context, actorID, garageID, action, entity, id string, before, after map[string]any) error {
	err := uc.audit.Record(ctx, audit.Entry{
		Actor: actorID, GarageID: garageID, Action: action, EntityType: entity, EntityID: id, Before: before, After: after,
		IP: middleware.ClientIP(ctx),
	})
	if err != nil {
		return toAppError("audit "+action, err)
	}

	return nil
}
