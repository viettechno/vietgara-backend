package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func partWithCase() *domain.Part {
	p := samplePart()
	p.PackagingUnits = []domain.PackagingUnit{
		{UnitID: caseID, Factor: "24", UnitAbbreviation: "ctn"},
		{UnitID: "half", Factor: "0.5", UnitAbbreviation: "half"},
	}

	return p
}

func TestUseCase_ResolveQuantity(t *testing.T) {
	t.Parallel()

	t.Run("the base unit, given or implied", func(t *testing.T) {
		t.Parallel()

		for _, unit := range []string{"", unitID} {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(partWithCase(), nil).Once()

			q, err := uc.ResolveQuantity(t.Context(), garageID, partID, unit, "7.250")
			require.NoError(t, err)
			assert.Equal(t, "7.25", q.Base)
			assert.Nil(t, q.UnitID)
			assert.Nil(t, q.Entered)
		}
	})

	t.Run("a packaging unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(partWithCase(), nil).Once()

		q, err := uc.ResolveQuantity(t.Context(), garageID, partID, caseID, "10.0")
		require.NoError(t, err)
		assert.Equal(t, "240", q.Base)
		assert.Equal(t, caseID, *q.UnitID)
		assert.Equal(t, "10", *q.Entered)
		assert.Equal(t, "ctn", q.UnitAbbreviation)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			unit, entered string
			code          apperror.Code
		}{
			"not a packaging unit of the part": {"other", "1", apperror.CodeInvalidArgument},
			"more than three decimals":         {"half", "0.0001", apperror.CodeInvalidArgument},
			"a result with too many decimals":  {"half", "0.0015", apperror.CodeInvalidArgument},
			"zero":                             {caseID, "0", apperror.CodeInvalidArgument},
			"not a number":                     {caseID, "ten", apperror.CodeInvalidArgument},
			"base unit, zero":                  {"", "0", apperror.CodeInvalidArgument},
		} {
			uc, d := newUseCase(t)
			d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(partWithCase(), nil).Once()

			_, err := uc.ResolveQuantity(t.Context(), garageID, partID, tc.unit, tc.entered)
			assertAppError(t, err, tc.code)

			_ = name
		}

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(nil, domain.ErrPartNotFound).Once()

		_, err := uc.ResolveQuantity(t.Context(), garageID, partID, "", "1")
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

func TestUseCase_PartExists(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, partID).Return(samplePart(), nil).Once()
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, "gone").Return(nil, domain.ErrPartNotFound).Once()
	d.Repo.EXPECT().FindPart(mock.Anything, garageID, "bad").Return(nil, errBoom).Once()

	ok, err := uc.PartExists(t.Context(), garageID, partID)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = uc.PartExists(t.Context(), garageID, "gone")
	require.NoError(t, err)
	assert.False(t, ok)

	_, err = uc.PartExists(t.Context(), garageID, "bad")
	require.ErrorIs(t, err, errBoom)
}

func TestUseCase_UnitAbbreviations(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(sampleUnit(), nil).Once()
	d.Repo.EXPECT().FindUnit(mock.Anything, garageID, "gone").Return(nil, domain.ErrUnitNotFound).Once()

	got, err := uc.UnitAbbreviations(t.Context(), garageID, []string{unitID, "gone"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{unitID: "can"}, got)

	d.Repo.EXPECT().FindUnit(mock.Anything, garageID, unitID).Return(nil, errBoom).Once()

	_, err = uc.UnitAbbreviations(t.Context(), garageID, []string{unitID})
	assertAppError(t, err, apperror.CodeInternal)
}

func TestToAppError(t *testing.T) {
	t.Parallel()

	assertAppError(t, usecase.ToAppError("op", domain.ErrPartNotFound), apperror.CodeNotFound)
	assertAppError(t, usecase.ToAppError("op", errBoom), apperror.CodeInternal)

	pre := apperror.New(apperror.CodePermissionDenied, "no")
	assert.Equal(t, error(pre), usecase.ToAppError("op", pre), "classified errors pass through")
}
