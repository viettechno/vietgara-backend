//go:build integration

package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/idempotency"
)

const method = "/vietgara.billing.v1.SettlementService/CreateSettlement"

var errHandler = errors.New("handler failed")

// TestStoreOnPostgres runs the store as the RLS-bound application role,
// inside transactions scoped like the Garage interceptor's.
//
//nolint:funlen // ordered steps on one seeded database
func TestStoreOnPostgres(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()

	accountID, garageA, garageB := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO accounts (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, accountID)
	require.NoError(t, err)
	_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, account_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageA, garageB, accountID)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	store := idempotency.NewStore(pool)
	txm := platformdb.NewTxManager(pool)
	hash := []byte("hash-1")
	keyA := idempotency.Key{GarageID: garageA, AccountID: accountID, Key: "key-1"}
	scope := func(garageID string) platformdb.Scope {
		return platformdb.Scope{GarageID: garageID, AccountID: accountID}
	}

	t.Run("a new key is claimed and its response stored", func(t *testing.T) {
		err := txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
			rec, err := store.Claim(ctx, keyA, method, hash)
			require.NoError(t, err)
			assert.Nil(t, rec)

			return store.Complete(ctx, keyA, idempotency.Record{ResponseType: "test.Response", Response: []byte("resp"), HTTPStatus: 201})
		})
		require.NoError(t, err)

		err = txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
			rec, err := store.Claim(ctx, keyA, method, hash)
			require.NoError(t, err)
			require.NotNil(t, rec)
			assert.Equal(t, idempotency.Record{
				Method: method, RequestHash: hash, ResponseType: "test.Response", Response: []byte("resp"), HTTPStatus: 201,
			}, *rec)

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("a failed request leaves no key", func(t *testing.T) {
		k := idempotency.Key{GarageID: garageA, AccountID: accountID, Key: "key-failed"}

		err := txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
			_, err := store.Claim(ctx, k, method, hash)
			require.NoError(t, err)

			return errHandler
		})
		require.ErrorIs(t, err, errHandler)

		err = txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
			rec, err := store.Claim(ctx, k, method, hash)
			assert.Nil(t, rec, "the retry runs as a new request")

			return err
		})
		require.NoError(t, err)
	})

	t.Run("a concurrent duplicate waits and replays", func(t *testing.T) {
		k := idempotency.Key{GarageID: garageA, AccountID: accountID, Key: "key-concurrent"}
		claimed, release := make(chan struct{}), make(chan struct{})
		firstDone := make(chan error, 1)

		go func() {
			firstDone <- txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
				if _, err := store.Claim(ctx, k, method, hash); err != nil {
					return err
				}

				close(claimed)
				<-release

				return store.Complete(ctx, k, idempotency.Record{ResponseType: "test.Response", Response: []byte("first")})
			})
		}()

		<-claimed

		secondDone := make(chan *idempotency.Record, 1)

		go func() {
			_ = txm.WithTransaction(ctx, scope(garageA), func(ctx context.Context) error {
				rec, err := store.Claim(ctx, k, method, hash)
				secondDone <- rec

				return err
			})
		}()

		select {
		case <-secondDone:
			t.Fatal("the duplicate must wait for the first request to commit")
		case <-time.After(300 * time.Millisecond):
		}

		close(release)
		require.NoError(t, <-firstDone)

		rec := <-secondDone
		require.NotNil(t, rec)
		assert.Equal(t, []byte("first"), rec.Response)
	})

	t.Run("keys are per garage and invisible across garages", func(t *testing.T) {
		keyB := idempotency.Key{GarageID: garageB, AccountID: accountID, Key: "key-1"}

		err := txm.WithTransaction(ctx, scope(garageB), func(ctx context.Context) error {
			rec, err := store.Claim(ctx, keyB, method, hash)
			assert.Nil(t, rec, "the same key in another garage is a new request")

			return err
		})
		require.NoError(t, err)

		err = txm.WithTransaction(ctx, scope(garageB), func(ctx context.Context) error {
			var visible int
			require.NoError(t, platformdb.QuerierFromContext(ctx, pool).
				QueryRow(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&visible))
			assert.Equal(t, 1, visible, "garage A's keys are hidden by RLS")

			return nil
		})
		require.NoError(t, err)
	})

	t.Run("purge deletes expired keys only", func(t *testing.T) {
		_, err := pg.Admin.ExecContext(ctx, `
			INSERT INTO idempotency_keys (garage_id, account_id, idempotency_key, method, request_hash, created_at)
			VALUES ($1, $2, 'old', $3, '\x00', now() - interval '2 days')`, garageA, accountID, method)
		require.NoError(t, err)

		deleted, err := store.Purge(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 1, deleted)

		var left int
		require.NoError(t, pg.Admin.QueryRowContext(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&left))
		assert.Equal(t, 4, left, "fresh keys stay")
	})
}
