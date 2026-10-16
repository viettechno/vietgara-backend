package partquantity_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

var errBoom = errors.New("boom")

const (
	garageID = "g-1"
	partID   = "p-1"
	unitID   = "u-1"
)

func TestForLine(t *testing.T) {
	t.Parallel()

	part := func() *string { s := partID; return &s }

	t.Run("no unit keeps the quantity as given", func(t *testing.T) {
		t.Parallel()

		got, err := partquantity.ForLine(t.Context(), mocks.NewMockPartQuantityResolver(t), garageID, false, nil, "2.5", "", "ignored")
		require.NoError(t, err)
		assert.Equal(t, partquantity.Line{Quantity: "2.5"}, got)
	})

	t.Run("a unit derives the quantity from the typed amount", func(t *testing.T) {
		t.Parallel()

		r := mocks.NewMockPartQuantityResolver(t)
		unit, entered := unitID, "2"
		r.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(partquantity.Resolved{Base: "48", UnitID: &unit, Entered: &entered}, nil).Once()

		got, err := partquantity.ForLine(t.Context(), r, garageID, true, part(), "999", unitID, "2")
		require.NoError(t, err)
		assert.Equal(t, "48", got.Quantity, "the request's own quantity is never trusted")
		assert.Equal(t, unitID, *got.UnitID)
		assert.Equal(t, "2", *got.Entered)
	})

	t.Run("a unit needs a part line with a part and an amount", func(t *testing.T) {
		t.Parallel()

		r := mocks.NewMockPartQuantityResolver(t)
		empty := ""

		for name, tc := range map[string]struct {
			isPart  bool
			partID  *string
			entered string
			field   string
		}{
			"labor line": {false, part(), "2", "unit_id"},
			"no part":    {true, nil, "2", "unit_id"},
			"blank part": {true, &empty, "2", "unit_id"},
			"no amount":  {true, part(), "", "entered_quantity"},
		} {
			_, err := partquantity.ForLine(t.Context(), r, garageID, tc.isPart, tc.partID, "1", unitID, tc.entered)

			appErr, ok := apperror.As(err)
			require.True(t, ok, name)
			assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code, name)
		}
	})

	t.Run("resolver failure passes through", func(t *testing.T) {
		t.Parallel()

		r := mocks.NewMockPartQuantityResolver(t)
		r.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").Return(partquantity.Resolved{}, errBoom).Once()

		_, err := partquantity.ForLine(t.Context(), r, garageID, true, part(), "1", unitID, "2")
		require.ErrorIs(t, err, errBoom)
	})
}
