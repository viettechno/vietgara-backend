package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/file/domain"
	"github.com/viettechno/vietgara-backend/internal/file/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

const (
	garageID = "11111111-1111-4111-8111-111111111111"
	fileID   = "22222222-2222-4222-8222-222222222222"
	actorID  = "33333333-3333-4333-8333-333333333333"
)

var (
	now     = time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func fileRow() []any {
	return []any{fileID, garageID, "logo", "garage", garageID, "garages/g/logo/g/f.png", "image/png", int64(40), "abc", true, nil, now}
}

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	f := &domain.File{ID: fileID, GarageID: garageID, Kind: domain.KindLogo, EntityType: "garage", EntityID: garageID, Key: "k"}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO files", dbtest.Row(now)).
		ExpectQueryRow("INSERT INTO files", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.Create(ctx, f))
	assert.Equal(t, now, f.CreatedAt)
	require.ErrorIs(t, repo.Create(ctx, f), errBoom)
}

func TestPostgresRepository_Find(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectQueryRow("AND id = $2", dbtest.Row(fileRow()...)).
		ExpectQueryRow("AND id = $2", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("AND key = $2", dbtest.Row(fileRow()...)).
		ExpectQueryRow("AND key = $2", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("AND key = $2", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	f, err := repo.FindByID(ctx, garageID, fileID)
	require.NoError(t, err)
	assert.Equal(t, domain.KindLogo, f.Kind)
	assert.Equal(t, "garages/g/logo/g/f.png", f.Key)
	assert.True(t, f.IsActive)

	_, err = repo.FindByID(ctx, garageID, fileID)
	require.ErrorIs(t, err, domain.ErrFileNotFound)

	_, err = repo.FindByID(ctx, garageID, "nope")
	require.ErrorIs(t, err, domain.ErrFileNotFound, "not a uuid")

	_, err = repo.FindByKey(ctx, garageID, "k")
	require.NoError(t, err)

	_, err = repo.FindByKey(ctx, garageID, "k")
	require.ErrorIs(t, err, domain.ErrFileNotFound)

	_, err = repo.FindByKey(ctx, garageID, "k")
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_SoftDeleteAndSetActive(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	tx := dbtest.New(t).
		ExpectExec("SET deleted_at", dbtest.Tag(1), nil).
		WithArgs(func(args []any) { require.Equal(t, []any{garageID, fileID, new(actorID)}, args) }).
		ExpectExec("SET deleted_at", dbtest.Tag(0), nil).
		WithArgs(func(args []any) { require.Equal(t, (*string)(nil), args[2]) }).
		ExpectExec("SET deleted_at", dbtest.Tag(0), errBoom).
		ExpectExec("SET is_active", dbtest.Tag(1), nil).
		WithArgs(func(args []any) { require.Equal(t, []any{garageID, fileID, false, new(actorID)}, args) }).
		ExpectExec("SET is_active", dbtest.Tag(0), nil)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.SoftDelete(ctx, garageID, fileID, actorID))
	require.ErrorIs(t, repo.SoftDelete(ctx, garageID, fileID, ""), domain.ErrFileNotFound)
	require.ErrorIs(t, repo.SoftDelete(ctx, garageID, fileID, actorID), errBoom)
	require.ErrorIs(t, repo.SoftDelete(ctx, garageID, "nope", actorID), domain.ErrFileNotFound)

	require.NoError(t, repo.SetActive(ctx, garageID, fileID, false, actorID))
	require.ErrorIs(t, repo.SetActive(ctx, garageID, fileID, true, actorID), domain.ErrFileNotFound)
	require.ErrorIs(t, repo.SetActive(ctx, garageID, "nope", true, actorID), domain.ErrFileNotFound)
}
