package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

func partnerValues(contact string) []any {
	return []any{
		partnerID, garageID, "insurer", "Bao Viet", nil, []byte(contact), new("VIP"),
		nil, nil, testTime, testTime,
	}
}

func testPartner() *domain.Partner {
	return &domain.Partner{
		ID: partnerID, GarageID: garageID, Type: domain.PartnerTypeInsurer, Name: "Bao Viet",
		Contact: domain.PartnerContact{Phone: new("0281234567")},
	}
}

func TestPostgresPartnerRepository_CreateAndUpdate(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresPartnerRepository(pool)
	contactArg := mock.MatchedBy(func(args []any) bool {
		contact, ok := args[5].([]byte)

		return ok && args[2] == "insurer" && string(contact) == `{"phone":"0281234567"}`
	})

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO partners"), contactArg).Return(valuesRow(t, testTime, testTime)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO partners"), mock.Anything).
		Return(errRow(t, uniqueViolation("idx_partners_garage_type_name_unique"))).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE partners SET type"), mock.Anything).Return(valuesRow(t, testTime)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE partners SET type"), mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

	p := testPartner()
	require.NoError(t, repo.Create(t.Context(), p))
	assert.Equal(t, testTime, p.CreatedAt)
	require.ErrorIs(t, repo.Create(t.Context(), testPartner()), domain.ErrPartnerNameTaken)

	require.NoError(t, repo.Update(t.Context(), testPartner()))
	require.ErrorIs(t, repo.Update(t.Context(), testPartner()), domain.ErrPartnerNotFound)
}

func TestPostgresPartnerRepository_FindByID(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresPartnerRepository(pool)

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("FROM partners p WHERE"), []any{garageID, partnerID}).
		Return(valuesRow(t, partnerValues(`{"contact_person":"Mr. Minh"}`)...)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("FROM partners p WHERE"), mock.Anything).
		Return(valuesRow(t, partnerValues(`[`)...)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("FROM partners p WHERE"), mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

	p, err := repo.FindByID(t.Context(), garageID, partnerID)
	require.NoError(t, err)
	assert.Equal(t, domain.PartnerTypeInsurer, p.Type)
	assert.Equal(t, "Mr. Minh", *p.Contact.ContactPerson)
	assert.Equal(t, "VIP", *p.Note)

	_, err = repo.FindByID(t.Context(), garageID, partnerID)
	require.Error(t, err, "corrupt contact JSON")

	_, err = repo.FindByID(t.Context(), garageID, partnerID)
	require.ErrorIs(t, err, domain.ErrPartnerNotFound)

	_, err = repo.FindByID(t.Context(), garageID, "nope")
	require.ErrorIs(t, err, domain.ErrPartnerNotFound)
}

func TestPostgresPartnerRepository_List(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("SELECT count(*)"), []any{garageID, "insurer", "%bao%", "%BAO%"}).
		Return(valuesRow(t, 1)).Once()
	conn.EXPECT().Query(mock.Anything, sqlContaining("ORDER BY lower(p.name)"), []any{garageID, "insurer", "%bao%", "%BAO%", 20, 0}).
		Return(resultRows(t, partnerValues(`{}`)), nil).Once()

	list, total, err := repository.NewPostgresPartnerRepository(pool).List(t.Context(), garageID, usecase.PartnerFilter{
		ListFilter: usecase.ListFilter{Query: "bao", Limit: 20}, Type: domain.PartnerTypeInsurer,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, list, 1)
	assert.Equal(t, "Bao Viet", list[0].Name)
}

func TestPostgresPartnerRepository_SoftDelete(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresPartnerRepository(pool)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE partners SET deleted_at"), []any{garageID, partnerID, new(actorID)}).Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE partners SET deleted_at"), mock.Anything).Return(commandTag("0"), nil).Once()

	require.NoError(t, repo.SoftDelete(t.Context(), garageID, partnerID, actorID))
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, partnerID, actorID), domain.ErrPartnerNotFound)
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, "nope", actorID), domain.ErrPartnerNotFound)
}
