// Package partquantity resolves the quantity of a quote or repair-order part
// line (FR-CAT-02): a line stores its quantity in the part's base unit, and
// when the user typed it in a packaging unit, that unit and the typed amount
// are kept beside it. The conversion belongs to the Catalogue module; quote
// and repair-order lines reach it through Resolver, wired in cmd/server.
package partquantity

import (
	"context"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Resolved is a quantity converted to the base unit of its part.
type Resolved struct {
	// Base is the amount in the base unit.
	Base string
	// UnitID and Entered are set when a packaging unit was used.
	UnitID  *string
	Entered *string
	// UnitAbbreviation is the abbreviation of UnitID, for printing.
	UnitAbbreviation string
}

// Resolver converts a quantity typed in unitID (empty for the base unit) to
// the base unit of the part.
type Resolver interface {
	ResolveQuantity(ctx context.Context, garageID, partID, unitID, entered string) (Resolved, error)
}

// Line is the stored form of a line's quantity.
type Line struct {
	// Quantity is the base-unit amount, a decimal string.
	Quantity string
	// UnitID and Entered are nil when the base unit was used.
	UnitID  *string
	Entered *string
	// UnitAbbreviation is the abbreviation of UnitID, "" for the base unit.
	UnitAbbreviation string
}

// ForLine returns the quantity to store for a line. Without a unit the
// quantity is the base amount as given. With a unit, the line must be a part
// line referring to a part and carry the typed amount; the stored quantity is
// derived from it (never trusted from the request, so a line read back and
// sent again cannot drift).
func ForLine(
	ctx context.Context, r Resolver, garageID string, isPart bool, partID *string, quantity, unitID, entered string,
) (Line, error) {
	if unitID == "" {
		return Line{Quantity: quantity}, nil
	}

	if !isPart || partID == nil || *partID == "" {
		return Line{}, apperror.Validation("unit_id", apperror.IssueInvalid, "only a part line that refers to a part can have a unit")
	}

	if entered == "" {
		return Line{}, apperror.Validation("entered_quantity", apperror.IssueRequired, "entered_quantity is required with unit_id")
	}

	resolved, err := r.ResolveQuantity(ctx, garageID, *partID, unitID, entered)
	if err != nil {
		return Line{}, err
	}

	return Line{
		Quantity: resolved.Base, UnitID: resolved.UnitID, Entered: resolved.Entered, UnitAbbreviation: resolved.UnitAbbreviation,
	}, nil
}
