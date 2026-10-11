package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
)

const profileID = "77777777-7777-7777-7777-777777777777"

func profileValues() []any {
	return []any{
		profileID, garageID, customerID, "company", "Cong ty An Phat", new("0301234567"), new("12 Le Loi"), nil,
		true, true, nil, nil, testTime, testTime,
	}
}

func TestPostgresTaxProfileRepository_Create(t *testing.T) {
	t.Parallel()

	p := func() *domain.TaxProfile {
		return &domain.TaxProfile{ID: profileID, GarageID: garageID, CustomerID: customerID, Kind: domain.TaxProfilePersonal, LegalName: "Binh"}
	}

	t.Run("stores and scans the timestamps", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO customer_tax_profiles"), mock.Anything).
			Return(valuesRow(t, testTime, testTime)).Once()

		got := p()
		require.NoError(t, repository.NewPostgresTaxProfileRepository(pool).Create(t.Context(), got))
		assert.Equal(t, testTime, got.CreatedAt)
	})

	t.Run("a second personal profile is refused", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).
			Return(errRow(t, uniqueViolation("idx_customer_tax_profiles_personal"))).Once()

		err := repository.NewPostgresTaxProfileRepository(pool).Create(t.Context(), p())
		require.ErrorIs(t, err, domain.ErrPersonalProfileTaken)
	})
}

func TestPostgresTaxProfileRepository_Reads(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresTaxProfileRepository(pool)

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("customer_tax_profiles"), []any{garageID, customerID, profileID}).
		Return(valuesRow(t, profileValues()...)).Once()
	conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

	p, err := repo.FindByID(t.Context(), garageID, customerID, profileID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaxProfileCompany, p.Kind)
	assert.True(t, p.IsDefault)

	_, err = repo.FindByID(t.Context(), garageID, customerID, profileID)
	require.ErrorIs(t, err, domain.ErrTaxProfileNotFound)

	_, err = repo.FindByID(t.Context(), garageID, customerID, "not-a-uuid")
	require.ErrorIs(t, err, domain.ErrTaxProfileNotFound)

	conn.EXPECT().Query(mock.Anything, sqlContaining("ORDER BY is_default DESC"), []any{garageID, customerID, true}).
		Return(resultRows(t, profileValues()), nil).Once()

	list, err := repo.List(t.Context(), garageID, customerID, true)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	empty, err := repo.List(t.Context(), garageID, "not-a-uuid", false)
	require.NoError(t, err)
	assert.Empty(t, empty)

	conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom).Once()
	conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(resultRows(t, []any{errBoom}), nil).Once()

	_, err = repo.List(t.Context(), garageID, customerID, false)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.List(t.Context(), garageID, customerID, false)
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresTaxProfileRepository_Writes(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresTaxProfileRepository(pool)
	p := &domain.TaxProfile{ID: profileID, GarageID: garageID, CustomerID: customerID, LegalName: "x"}

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE customer_tax_profiles"), mock.Anything).
		Return(valuesRow(t, testTime)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE customer_tax_profiles"), mock.Anything).
		Return(errRow(t, pgx.ErrNoRows)).Once()

	require.NoError(t, repo.Update(t.Context(), p))
	assert.Equal(t, testTime, p.UpdatedAt)
	require.ErrorIs(t, repo.Update(t.Context(), p), domain.ErrTaxProfileNotFound)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("is_default = FALSE"), []any{garageID, customerID, "keep"}).
		Return(commandTag("1"), nil).Once()
	require.NoError(t, repo.ClearDefault(t.Context(), garageID, customerID, "keep"))

	conn.EXPECT().Exec(mock.Anything, sqlContaining("SET deleted_at"), mock.Anything).Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("SET deleted_at"), mock.Anything).Return(commandTag("0"), nil).Once()
	require.NoError(t, repo.SoftDelete(t.Context(), garageID, customerID, profileID, actorID))
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, customerID, profileID, ""), domain.ErrTaxProfileNotFound)
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, customerID, "bad", ""), domain.ErrTaxProfileNotFound)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("SET is_active"), []any{garageID, customerID, profileID, false, new(actorID)}).
		Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("SET is_active"), mock.Anything).Return(commandTag("0"), nil).Once()
	require.NoError(t, repo.SetActive(t.Context(), garageID, customerID, profileID, false, actorID))
	require.ErrorIs(t, repo.SetActive(t.Context(), garageID, customerID, profileID, true, ""), domain.ErrTaxProfileNotFound)
	require.ErrorIs(t, repo.SetActive(t.Context(), garageID, "bad", profileID, true, ""), domain.ErrTaxProfileNotFound)
}
