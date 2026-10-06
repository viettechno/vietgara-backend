//go:build integration

package repository_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestDeleteTestAccountsCascades checks, as the RLS-bound application
// role, that purging a test owner removes everything the owner had --
// through foreign-key cascades, which Postgres runs without row security --
// and nothing of real accounts.
func TestDeleteTestAccountsCascades(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	old := time.Now().Add(-48 * time.Hour)

	seed := func(email string, createdAt time.Time) (accountID, garageID string) {
		t.Helper()

		accountID, garageID = uuid.NewString(), uuid.NewString()

		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO accounts (id, email, full_name, created_at) VALUES ($1, $2, 'x', $3)`, []any{accountID, email, createdAt}},
			{`INSERT INTO garages (id, account_id, name) VALUES ($1, $2, 'G')`, []any{garageID, accountID}},
			{`INSERT INTO garage_staff (garage_id, account_id) VALUES ($1, $2)`, []any{garageID, accountID}},
			{`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'C')`, []any{uuid.NewString(), garageID}},
		} {
			_, err := pg.Admin.ExecContext(ctx, stmt.sql, stmt.args...)
			require.NoError(t, err, stmt.sql)
		}

		return accountID, garageID
	}

	staleTest, staleGarage := seed("bb.old@blackbox.vietgara.test", old)
	freshTest, _ := seed("bb.new@blackbox.vietgara.test", time.Now())
	realOwner, realGarage := seed("owner@gmail.com", old)
	lookalike, _ := seed("x@notblackbox.vietgara.test", old)

	repo := repository.NewPostgresAccountRepository(pg.AppPool(t))

	deleted, err := repo.DeleteTestAccounts(ctx, "blackbox.vietgara.test", time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM accounts WHERE id = $1`, staleTest))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM garages WHERE id = $1`, staleGarage))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM customers WHERE garage_id = $1`, staleGarage))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM garage_staff WHERE garage_id = $1`, staleGarage))

	for _, id := range []string{freshTest, realOwner, lookalike} {
		assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM accounts WHERE id = $1`, id), "kept: %s", id)
	}

	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM customers WHERE garage_id = $1`, realGarage))
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&n))

	return n
}
