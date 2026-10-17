package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

func TestGarageRepository(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresGarageRepository(nil)
	bad := garageRow()
	bad[15] = []byte("not json")
	badBank := garageRow()
	badBank[7] = []byte("[")

	tx := dbtest.New(t).
		ExpectQueryRow("WHERE g.id = $1", dbtest.Row(garageRow()...)).
		ExpectQueryRow("WHERE g.id = $1", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("WHERE g.id = $1", dbtest.Row(bad...)).
		ExpectQueryRow("WHERE g.id = $1", dbtest.Row(badBank...)).
		ExpectQuery("g.user_id = $1", dbtest.NewRows(garageRow()), nil).
		ExpectQuery("g.user_id = $1", nil, errBoom).
		ExpectQuery("g.user_id = $1", dbtest.NewRows(bad), nil).
		ExpectQueryRow("INSERT INTO garages", dbtest.Row(now, now)).
		ExpectQueryRow("UPDATE garages", dbtest.Row(now)).
		ExpectQueryRow("UPDATE garages", dbtest.ErrRow(pgx.ErrNoRows))
	ctx := tx.Context(t.Context())

	g, err := repo.FindByID(ctx, "g-1")
	require.NoError(t, err)
	assert.Equal(t, "owner", g.OwnerUserID)
	assert.Equal(t, domain.GarageStatusActive, g.Status)
	assert.Len(t, g.OperatingHours, 1)
	assert.Equal(t, "a@b.vn", *g.Email)
	assert.Equal(t, "Gara Co., Ltd", *g.LegalName)
	assert.Equal(t, "VCB", *g.Bank.BankName)
	assert.Equal(t, "001", *g.Bank.AccountNumber)
	assert.Nil(t, g.Bank.AccountHolder)
	assert.Equal(t, "PT-{SEQ:4}", g.PaymentVoucherNumberTemplate)
	assert.Equal(t, "PN-{SEQ:4}", g.GoodsReceiptNumberTemplate)
	assert.Equal(t, "PX-{SEQ:4}", g.StockIssueNumberTemplate)
	assert.Equal(t, domain.StockIssueModeOnStart, g.StockIssueMode)

	_, err = repo.FindByID(ctx, "x")
	require.ErrorIs(t, err, domain.ErrGarageNotFound)
	_, err = repo.FindByID(ctx, "x")
	require.Error(t, err)
	_, err = repo.FindByID(ctx, "x")
	require.Error(t, err, "corrupt bank details")

	list, err := repo.ListForUser(ctx, "owner")
	require.NoError(t, err)
	assert.Len(t, list, 1)

	_, err = repo.ListForUser(ctx, "owner")
	require.ErrorIs(t, err, errBoom)
	_, err = repo.ListForUser(ctx, "owner")
	require.Error(t, err)

	require.NoError(t, repo.Create(ctx, g))
	require.NoError(t, repo.Update(ctx, g))
	require.ErrorIs(t, repo.Update(ctx, g), domain.ErrGarageNotFound)
}

func TestAccessReader(t *testing.T) {
	t.Parallel()

	r := repository.NewPostgresAccessReader(nil)

	tx := dbtest.New(t).
		ExpectQueryRow("FROM garages g", dbtest.Row("user-1", true, nil, []string{})).
		ExpectQueryRow("FROM garages g", dbtest.Row("owner", true, "active", []string{"customer.read"})).
		ExpectQueryRow("FROM garages g", dbtest.Row("owner", true, "inactive", []string{})).
		ExpectQueryRow("FROM garages g", dbtest.Row("owner", true, nil, []string{})).
		ExpectQueryRow("FROM garages g", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM garages g", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("owner_usage", dbtest.Row(2, 7))
	ctx := tx.Context(t.Context())

	owner, err := r.GarageAccess(ctx, "g-1", "user-1")
	require.NoError(t, err)
	assert.True(t, owner.Owner)

	member, err := r.GarageAccess(ctx, "g-1", "user-1")
	require.NoError(t, err)
	assert.False(t, member.Owner)
	assert.Equal(t, []access.Permission{access.CustomerRead}, member.Permissions)

	for range 3 {
		_, err = r.GarageAccess(ctx, "g-1", "user-1")
		require.ErrorIs(t, err, access.ErrNoMembership)
	}

	_, err = r.GarageAccess(ctx, "g-1", "user-1")
	require.ErrorIs(t, err, errBoom)

	garages, staff, err := r.OwnerUsage(ctx, "owner")
	require.NoError(t, err)
	assert.Equal(t, 2, garages)
	assert.Equal(t, 7, staff)
}
