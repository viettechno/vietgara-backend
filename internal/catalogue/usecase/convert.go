package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
)

// Quantity is a line quantity as the other modules store it: the base-unit
// amount, plus the packaging unit and the amount the user typed (nil when
// the base unit was used).
type Quantity struct {
	// Base is the amount in the part's base unit, a decimal string.
	Base string
	// UnitID and Entered are set when a packaging unit was used.
	UnitID  *string
	Entered *string
	// UnitAbbreviation is the abbreviation of UnitID, for printing.
	UnitAbbreviation string
}

// ResolveQuantity converts a quantity typed in unitID (empty: the part's base
// unit) to the base unit of the part (FR-CAT-02). It is the one place
// conversion happens, used by stock receipts and by quote and repair-order
// part lines. It refuses an unknown part, a unit that is not one of the
// part's packaging units, and a result that needs more than three decimals.
func (uc *UseCase) ResolveQuantity(ctx context.Context, garageID, partID, unitID, entered string) (Quantity, error) {
	p, err := uc.repo.FindPart(ctx, garageID, partID)
	if err != nil {
		return Quantity{}, toAppError("convert quantity", err)
	}

	if unitID == "" || unitID == p.BaseUnitID {
		base, err := domain.ConvertToBase(entered, "1")
		if err != nil {
			return Quantity{}, toAppError("convert quantity", err)
		}

		return Quantity{Base: base}, nil
	}

	for _, u := range p.PackagingUnits {
		if u.UnitID != unitID {
			continue
		}

		base, err := domain.ConvertToBase(entered, u.Factor)
		if err != nil {
			return Quantity{}, toAppError("convert quantity", err)
		}

		enteredNorm, err := domain.ConvertToBase(entered, "1")
		if err != nil {
			return Quantity{}, toAppError("convert quantity", err)
		}

		return Quantity{Base: base, UnitID: &unitID, Entered: &enteredNorm, UnitAbbreviation: u.UnitAbbreviation}, nil
	}

	return Quantity{}, toAppError("convert quantity", domain.ErrUnitNotForPart)
}

// PartExists reports whether a live part of the garage has this id, for the
// File module's entity check.
func (uc *UseCase) PartExists(ctx context.Context, garageID, partID string) (bool, error) {
	_, err := uc.repo.FindPart(ctx, garageID, partID)
	if err != nil {
		if isAny(err, notFoundErrors) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

// UnitAbbreviations returns the abbreviation of each unit id, for the
// modules that print a quantity with its unit. Unknown ids are skipped.
func (uc *UseCase) UnitAbbreviations(ctx context.Context, garageID string, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))

	for _, id := range ids {
		u, err := uc.repo.FindUnit(ctx, garageID, id)
		if err != nil {
			if isAny(err, notFoundErrors) {
				continue
			}

			return nil, toAppError("find unit", err)
		}

		out[id] = u.Abbreviation
	}

	return out, nil
}
