package idempotency_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/platform/idempotency"
)

var (
	errBoom = errors.New("boom")
	key     = idempotency.Key{GarageID: "g", UserID: "a", Key: "k"}
)

func TestClaim(t *testing.T) {
	t.Parallel()

	store := idempotency.NewStore(nil)
	responseType, status := "test.Response", 201

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(0), nil).
		ExpectQueryRow("FROM idempotency_keys", dbtest.Row("m", []byte("h"), responseType, []byte("r"), status)).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(0), nil).
		ExpectQueryRow("FROM idempotency_keys", dbtest.Row("m", []byte("h"), nil, nil, nil)).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(0), nil).
		ExpectQueryRow("FROM idempotency_keys", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(0), nil).
		ExpectQueryRow("FROM idempotency_keys", dbtest.ErrRow(errBoom)).
		ExpectExec("INSERT INTO idempotency_keys", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	rec, err := store.Claim(ctx, key, "m", []byte("h"))
	require.NoError(t, err)
	assert.Nil(t, rec, "a new key")

	rec, err = store.Claim(ctx, key, "m", []byte("h"))
	require.NoError(t, err)
	assert.Equal(t, &idempotency.Record{Method: "m", RequestHash: []byte("h"), ResponseType: responseType, Response: []byte("r"), HTTPStatus: status}, rec)

	rec, err = store.Claim(ctx, key, "m", []byte("h"))
	require.NoError(t, err)
	assert.Empty(t, rec.ResponseType, "no response stored")

	_, err = store.Claim(ctx, key, "m", []byte("h"))
	require.ErrorContains(t, err, "expired")

	_, err = store.Claim(ctx, key, "m", []byte("h"))
	require.ErrorIs(t, err, errBoom)

	_, err = store.Claim(ctx, key, "m", []byte("h"))
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteAndPurge(t *testing.T) {
	t.Parallel()

	store := idempotency.NewStore(nil)

	tx := dbtest.New(t).
		ExpectExec("UPDATE idempotency_keys", dbtest.Tag(1), nil).WithArgs(func(args []any) {
		status, ok := args[5].(*int)
		require.True(t, ok)
		assert.Equal(t, 201, *status)
	}).
		ExpectExec("UPDATE idempotency_keys", dbtest.Tag(1), nil).WithArgs(func(args []any) {
		assert.Nil(t, args[5], "the default status is stored as NULL")
	}).
		ExpectExec("UPDATE idempotency_keys", dbtest.Tag(0), errBoom).
		ExpectQueryRow("purge_idempotency_keys", dbtest.Row(int64(3))).
		ExpectQueryRow("purge_idempotency_keys", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	require.NoError(t, store.Complete(ctx, key, idempotency.Record{ResponseType: "t", Response: []byte("r"), HTTPStatus: 201}))
	require.NoError(t, store.Complete(ctx, key, idempotency.Record{ResponseType: "t", Response: []byte("r")}))
	require.ErrorIs(t, store.Complete(ctx, key, idempotency.Record{}), errBoom)

	deleted, err := store.Purge(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, deleted)

	_, err = store.Purge(ctx)
	require.ErrorIs(t, err, errBoom)
}
