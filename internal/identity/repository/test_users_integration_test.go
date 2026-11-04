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

// TestDeleteTestUsersCascades checks, as the RLS-bound application
// role, that purging a test owner removes everything the owner had --
// through foreign-key cascades, which Postgres runs without row security --
// and nothing of real users.
func TestDeleteTestUsersCascades(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	old := time.Now().Add(-48 * time.Hour)

	seed := func(email string, createdAt time.Time) (userID, garageID string) {
		t.Helper()

		userID, garageID = uuid.NewString(), uuid.NewString()

		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, $2, 'x', $3)`, []any{userID, email, createdAt}},
			{`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, []any{garageID, userID}},
			{`INSERT INTO garage_staff (garage_id, user_id) VALUES ($1, $2)`, []any{garageID, userID}},
			{`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'C')`, []any{uuid.NewString(), garageID}},
		} {
			_, err := pg.Admin.ExecContext(ctx, stmt.sql, stmt.args...)
			require.NoError(t, err, stmt.sql)
		}

		return userID, garageID
	}

	staleTest, staleGarage := seed("bb.old@blackbox.vietgara.test", old)
	freshTest, freshGarage := seed("bb.new@blackbox.vietgara.test", time.Now())
	realOwner, realGarage := seed("owner@gmail.com", old)
	lookalike, lookalikeGarage := seed("x@notblackbox.vietgara.test", old)

	repo := repository.NewPostgresUserRepository(pg.AppPool(t))
	cutoff := time.Now().Add(-24 * time.Hour)

	emails, err := repo.ListTestUserEmails(ctx, "blackbox.vietgara.test", cutoff)
	require.NoError(t, err)
	assert.Equal(t, []string{"bb.old@blackbox.vietgara.test"}, emails)

	garageIDs, err := repo.ListTestUserGarageIDs(ctx, "blackbox.vietgara.test", cutoff)
	require.NoError(t, err)
	assert.Equal(t, []string{staleGarage}, garageIDs)

	deleted, err := repo.DeleteTestUsers(ctx, "blackbox.vietgara.test", cutoff)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id = $1`, staleTest))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM garages WHERE id = $1`, staleGarage))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM customers WHERE garage_id = $1`, staleGarage))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM garage_staff WHERE garage_id = $1`, staleGarage))

	for _, id := range []string{freshTest, realOwner, lookalike} {
		assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id = $1`, id), "kept: %s", id)
	}

	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM customers WHERE garage_id = $1`, realGarage))
	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM garages WHERE id = $1`, freshGarage))
	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM garages WHERE id = $1`, lookalikeGarage))
}

// TestDeleteTestUsersClearsWhatRestrictsTheCascade: a garage with a unit, a
// part, the part's packaging unit and a barcode of that packaging (each refers
// to the one before ON DELETE RESTRICT) could not be deleted with its owner,
// because the cascade deleted the unit first.
func TestDeleteTestUsersClearsWhatRestrictsTheCascade(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	old := time.Now().Add(-48 * time.Hour)

	userID, garageID, unitID, partID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, 'bb.stock@blackbox.vietgara.test', 'x', $2)`, []any{userID, old}},
		{`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, []any{garageID, userID}},
		{`INSERT INTO units (id, garage_id, name, abbreviation) VALUES ($1, $2, 'Box', 'bx')`, []any{unitID, garageID}},
		{`INSERT INTO parts (id, garage_id, sku, name, base_unit_id) VALUES ($1, $2, 'P1', 'Part', $3)`, []any{partID, garageID, unitID}},
		{`INSERT INTO part_units (garage_id, part_id, unit_id, factor) VALUES ($1, $2, $3, 2)`, []any{garageID, partID, unitID}},
		{`INSERT INTO part_barcodes (garage_id, part_id, unit_id, barcode) VALUES ($1, $2, $3, '8930001')`, []any{garageID, partID, unitID}},
	} {
		_, err := pg.Admin.ExecContext(ctx, stmt.sql, stmt.args...)
		require.NoError(t, err, stmt.sql)
	}

	repo := repository.NewPostgresUserRepository(pg.AppPool(t))

	deleted, err := repo.DeleteTestUsers(ctx, "blackbox.vietgara.test", time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id = $1`, userID))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM garages WHERE id = $1`, garageID))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM units WHERE garage_id = $1`, garageID))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM parts WHERE garage_id = $1`, garageID))
}

// TestDeleteTestUsersWaitsForTheOwnerOfTheGarage: a test manager who invited
// someone into a test owner's garage cannot be deleted before the garage is
// (staff_invitations.invited_by does not cascade). The manager sorts first, so
// the first pass fails for the manager and the next one deletes it.
func TestDeleteTestUsersWaitsForTheOwnerOfTheGarage(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	old := time.Now().Add(-48 * time.Hour)

	manager := "00000000-0000-4000-8000-000000000001"
	owner := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	invitee, garageID := uuid.NewString(), uuid.NewString()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, 'bb.manager@blackbox.vietgara.test', 'x', $2)`, []any{manager, old}},
		{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, 'bb.owner@blackbox.vietgara.test', 'x', $2)`, []any{owner, old}},
		{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, 'keeper@gmail.com', 'x', $2)`, []any{invitee, old}},
		{`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, []any{garageID, owner}},
		{`INSERT INTO staff_invitations (garage_id, user_id, invited_by, expires_at) VALUES ($1, $2, $3, now() + interval '7 days')`, []any{garageID, invitee, manager}},
	} {
		_, err := pg.Admin.ExecContext(ctx, stmt.sql, stmt.args...)
		require.NoError(t, err, stmt.sql)
	}

	repo := repository.NewPostgresUserRepository(pg.AppPool(t))

	deleted, err := repo.DeleteTestUsers(ctx, "blackbox.vietgara.test", time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 2, deleted)

	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id IN ($1, $2)`, manager, owner))
	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id = $1`, invitee), "a real user is kept")
}

// TestDeleteTestUsersDeletesThePlatformAdminWhoseQRIsInUse: the platform admin uploaded the payment
// QR, the one settings row refers to that file, and no cascade removes the row. Deleting the
// admin deletes the file and the row keeps its details and loses the QR (ON DELETE SET NULL).
func TestDeleteTestUsersDeletesThePlatformAdminWhoseQRIsInUse(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	old := time.Now().Add(-48 * time.Hour)

	admin, fileID := uuid.NewString(), uuid.NewString()

	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (id, email, full_name, created_at) VALUES ($1, 'admin@blackbox.vietgara.test', 'x', $2)`, []any{admin, old}},
		{`INSERT INTO files (id, user_id, kind, entity_type, entity_id, key, content_type, size, sha256) VALUES ($1, $2, 'platform_qr', 'platform', $3, 'qr.png', 'image/png', 1, repeat('a', 64))`, []any{fileID, admin, uuid.NewString()}},
		{`UPDATE platform_payment_settings SET bank_name = 'ACB', qr_file_id = $1 WHERE id = 1`, []any{fileID}},
	} {
		_, err := pg.Admin.ExecContext(ctx, stmt.sql, stmt.args...)
		require.NoError(t, err, stmt.sql)
	}

	repo := repository.NewPostgresUserRepository(pg.AppPool(t))

	deleted, err := repo.DeleteTestUsers(ctx, "blackbox.vietgara.test", time.Now().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)

	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM users WHERE id = $1`, admin))
	assert.Zero(t, count(t, pg.Admin, `SELECT count(*) FROM files WHERE id = $1`, fileID))
	assert.Equal(t, 1, count(t, pg.Admin, `SELECT count(*) FROM platform_payment_settings WHERE id = 1 AND bank_name = 'ACB' AND qr_file_id IS NULL`), "the details stay, the QR is gone")
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&n))

	return n
}
