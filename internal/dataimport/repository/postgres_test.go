package repository_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/dataimport/domain"
	"github.com/viettechno/vietgara-backend/internal/dataimport/repository"
	"github.com/viettechno/vietgara-backend/internal/dataimport/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var errBoom = errors.New("boom")

const sum = "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789"

func TestPostgresRepository_Find(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectQueryRow("FROM import_runs", dbtest.Row("run-1", "g-1", "parts", "abcdef", "parts.xlsx", 500, nil, nil)).WithArgs(
		func(args []any) {
			assert.Equal(t, []any{"g-1", "parts", "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}, args)
		}).
		ExpectQueryRow("FROM import_runs", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM import_runs", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())
	repo := repository.NewPostgresRepository(nil)

	run, err := repo.Find(ctx, "g-1", "parts", sum)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "run-1", run.ID)
	assert.Equal(t, 500, run.RowCount)

	run, err = repo.Find(ctx, "g-1", "parts", sum)
	require.NoError(t, err)
	assert.Nil(t, run, "an unseen file is not an error")

	_, err = repo.Find(ctx, "g-1", "parts", sum)
	require.ErrorIs(t, err, errBoom)
}

func TestPostgresRepository_Create(t *testing.T) {
	t.Parallel()

	doc := "doc-1"
	run := &usecase.Run{
		ID: "run-1", GarageID: "g-1", Type: "goods_receipts", FileSHA256: sum, FileName: "gr.xlsx", RowCount: 3, DocumentID: &doc,
	}

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO import_runs", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO import_runs", dbtest.Tag(0), nil).
		ExpectExec("INSERT INTO import_runs", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())
	repo := repository.NewPostgresRepository(nil)

	require.NoError(t, repo.Create(ctx, run))
	require.ErrorIs(t, repo.Create(ctx, run), domain.ErrFileAlreadyImported, "a concurrent confirmation inserts nothing")
	require.ErrorIs(t, repo.Create(ctx, run), errBoom)
}

func TestPostgresRepository_Lock(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(1), nil).WithArgs(func(args []any) {
		assert.Equal(t, []any{"g-1|parts|abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"}, args)
	}).
		ExpectExec("pg_advisory_xact_lock", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())
	repo := repository.NewPostgresRepository(nil)

	require.NoError(t, repo.Lock(ctx, "g-1", "parts", sum))
	require.ErrorIs(t, repo.Lock(ctx, "g-1", "parts", sum), errBoom)
}
