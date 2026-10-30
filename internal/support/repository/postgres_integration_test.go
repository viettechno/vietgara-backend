//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/support/domain"
	"github.com/viettechno/vietgara-backend/internal/support/repository"
)

type env struct {
	pg          *pgtest.Postgres
	tx          *platformdb.TxManager
	repo        *repository.Postgres
	owner       string
	staff       string
	stranger    string
	garage      string
	otherGarage string
}

func seedUser(t *testing.T, admin *sql.DB, email string) string {
	t.Helper()

	id := uuid.NewString()
	_, err := admin.ExecContext(t.Context(), `INSERT INTO users (id, email, full_name) VALUES ($1, $2, $2)`, id, email)
	require.NoError(t, err)

	return id
}

func seedGarage(t *testing.T, admin *sql.DB, owner, name string) string {
	t.Helper()

	id := uuid.NewString()
	_, err := admin.ExecContext(t.Context(), `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, $3)`, id, owner, name)
	require.NoError(t, err)

	return id
}

func setup(t *testing.T) *env {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	pool := pg.AppPool(t)
	e := &env{pg: pg, tx: platformdb.NewTxManager(pool), repo: repository.NewPostgres(pool)}
	e.owner = seedUser(t, pg.Admin, "owner@x.test")
	e.staff = seedUser(t, pg.Admin, "staff@x.test")
	e.stranger = seedUser(t, pg.Admin, "stranger@x.test")
	e.garage = seedGarage(t, pg.Admin, e.owner, "Gara One")
	e.otherGarage = seedGarage(t, pg.Admin, e.owner, "Gara Two")

	return e
}

// as runs fn in a transaction scoped to a user and, optionally, a garage.
func (e *env) as(t *testing.T, user, garage string, fn func(ctx context.Context)) {
	t.Helper()

	require.NoError(t, e.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: user, GarageID: garage}, func(ctx context.Context) error {
		fn(ctx)

		return nil
	}))
}

// TestGrantLifecycleUnderRowLevelSecurity covers the visibility rules of
// FR-IAM-11: staff see their own grants, the owner sees the garage's, nobody
// else sees either, and the owner's answer is written in the garage's scope.
func TestGrantLifecycleUnderRowLevelSecurity(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()

	var grant *domain.Grant

	e.as(t, e.staff, "", func(ctx context.Context) {
		grant = domain.NewGrant(e.garage, e.staff, "ticket 42", 2*time.Hour, false, now)
		require.NoError(t, e.repo.Create(ctx, grant))

		open, err := e.repo.HasOpen(ctx, e.garage, e.staff, now)
		require.NoError(t, err)
		assert.True(t, open, "a pending request counts as open")

		mine, err := e.repo.ListByStaff(ctx, e.staff)
		require.NoError(t, err)
		require.Len(t, mine, 1)
		assert.Equal(t, "Gara One", mine[0].GarageName, "staff see the garage name through the directory setting")
		assert.Equal(t, 2*time.Hour, mine[0].TTL)
		assert.Equal(t, domain.StatusPending, mine[0].Status(now))

		_, _, err = e.repo.ActiveGrant(ctx, e.garage, e.staff, now)
		assert.ErrorIs(t, err, domain.ErrGrantNotFound, "pending is not active")
	})

	e.as(t, e.stranger, "", func(ctx context.Context) {
		others, err := e.repo.ListByStaff(ctx, e.staff)
		require.NoError(t, err)
		assert.Empty(t, others, "another user does not see the grant")

		_, err = e.repo.Find(ctx, grant.ID)
		assert.ErrorIs(t, err, domain.ErrGrantNotFound)
	})

	e.as(t, e.owner, e.otherGarage, func(ctx context.Context) {
		list, err := e.repo.ListByGarage(ctx, e.garage)
		require.NoError(t, err)
		assert.Empty(t, list, "the owner scoped to another garage does not see it")
	})

	e.as(t, e.owner, e.garage, func(ctx context.Context) {
		list, err := e.repo.ListByGarage(ctx, e.garage)
		require.NoError(t, err)
		require.Len(t, list, 1)

		found, err := e.repo.Find(ctx, grant.ID)
		require.NoError(t, err)
		require.NoError(t, found.Approve(e.owner, now))
		require.NoError(t, e.repo.Update(ctx, found))
	})

	e.as(t, e.staff, e.garage, func(ctx context.Context) {
		grantID, ownerID, err := e.repo.ActiveGrant(ctx, e.garage, e.staff, now.Add(time.Minute))
		require.NoError(t, err)
		assert.Equal(t, grant.ID, grantID)
		assert.Equal(t, e.owner, ownerID)

		_, _, err = e.repo.ActiveGrant(ctx, e.garage, e.staff, now.Add(3*time.Hour))
		assert.ErrorIs(t, err, domain.ErrGrantNotFound, "expired")
	})

	e.as(t, e.owner, e.garage, func(ctx context.Context) {
		found, err := e.repo.Find(ctx, grant.ID)
		require.NoError(t, err)
		assert.Equal(t, "owner@x.test", found.ApproverName)
		require.NoError(t, found.Revoke(now.Add(time.Minute)))
		require.NoError(t, e.repo.Update(ctx, found))
	})

	e.as(t, e.staff, e.garage, func(ctx context.Context) {
		_, _, err := e.repo.ActiveGrant(ctx, e.garage, e.staff, now.Add(2*time.Minute))
		require.ErrorIs(t, err, domain.ErrGrantNotFound, "revoked")

		open, err := e.repo.HasOpen(ctx, e.garage, e.staff, now.Add(2*time.Minute))
		require.NoError(t, err)
		assert.False(t, open)
	})
}

// TestDirectoryIsOpenOnlyInsideTheSupportQueries shows the garage policy
// stays closed for every other query of the same transaction.
func TestDirectoryIsOpenOnlyInsideTheSupportQueries(t *testing.T) {
	e := setup(t)

	e.as(t, e.staff, "", func(ctx context.Context) {
		entries, total, err := e.repo.Directory(ctx, "gara t", domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, entries, 1)
		assert.Equal(t, "Gara Two", entries[0].Name)
		assert.Equal(t, "owner@x.test", entries[0].OwnerEmail)

		garage, err := e.repo.Garage(ctx, e.garage)
		require.NoError(t, err)
		assert.Equal(t, e.owner, garage.OwnerUserID)

		_, err = e.repo.Garage(ctx, uuid.NewString())
		require.ErrorIs(t, err, domain.ErrGarageNotFound)

		var visible int
		require.NoError(t, platformdb.QuerierFromContext(ctx, nil).QueryRow(ctx, `SELECT count(*) FROM garages`).Scan(&visible))
		assert.Zero(t, visible, "the setting is switched off again")
	})

	e.as(t, e.staff, "", func(ctx context.Context) {
		entries, total, err := e.repo.Directory(ctx, `%`, domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Zero(t, total, "wildcards in the query are literal")
		assert.Empty(t, entries)
	})
}

// TestAccessLogIsAppendOnlyAndScopedToTheGarage covers the owner's view of
// what support read.
func TestAccessLogIsAppendOnlyAndScopedToTheGarage(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()

	var grant *domain.Grant

	e.as(t, e.staff, "", func(ctx context.Context) {
		grant = domain.NewGrant(e.garage, e.staff, "r", time.Hour, true, now)
		require.NoError(t, e.repo.Create(ctx, grant))
	})

	e.as(t, e.staff, e.garage, func(ctx context.Context) {
		for _, entity := range []string{"customer", "settlement"} {
			require.NoError(t, e.repo.InsertEvent(ctx, &domain.AccessEvent{
				GrantID: grant.ID, GarageID: e.garage, StaffUserID: e.staff, Method: "Get" + entity, EntityType: entity, EntityID: uuid.NewString(),
			}))
		}
	})

	e.as(t, e.owner, e.garage, func(ctx context.Context) {
		events, total, err := e.repo.ListEvents(ctx, e.garage, "", domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		require.Len(t, events, 2)
		assert.Equal(t, "staff@x.test", events[0].StaffName)

		one, total, err := e.repo.ListEvents(ctx, e.garage, grant.ID, domain.Page{Page: 1, PageSize: 1})
		require.NoError(t, err)
		assert.Equal(t, 2, total)
		assert.Len(t, one, 1)
	})

	e.as(t, e.owner, e.otherGarage, func(ctx context.Context) {
		events, _, err := e.repo.ListEvents(ctx, e.garage, "", domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Empty(t, events, "another garage's owner sees nothing")
	})

	err := e.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: e.owner, GarageID: e.garage}, func(ctx context.Context) error {
		_, err := platformdb.QuerierFromContext(ctx, nil).Exec(ctx, `UPDATE support_access_events SET method = 'x'`)

		return err
	})
	assert.Error(t, err, "the application role cannot change the log")
}

// TestReadOnlySessionRefusesWrites is the mechanism behind "a support
// session cannot change anything".
func TestReadOnlySessionRefusesWrites(t *testing.T) {
	e := setup(t)

	err := e.tx.WithTransaction(t.Context(), platformdb.Scope{UserID: e.staff, GarageID: e.garage}, func(ctx context.Context) error {
		if err := e.repo.SetReadOnly(ctx); err != nil {
			return err
		}

		_, err := platformdb.QuerierFromContext(ctx, nil).Exec(ctx, `UPDATE garages SET name = 'changed' WHERE id = $1`, e.garage)

		return err
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only transaction")

	var name string
	require.NoError(t, e.pg.Admin.QueryRowContext(t.Context(), `SELECT name FROM garages WHERE id = $1`, e.garage).Scan(&name))
	assert.Equal(t, "Gara One", name)
}

// TestDiagnosticQueries reads the outbox and the notification log of the
// garage's people, and nobody else's.
func TestDiagnosticQueries(t *testing.T) {
	e := setup(t)
	ctx := t.Context()

	_, err := e.pg.Admin.ExecContext(ctx, `
		INSERT INTO outbox_events (id, event_type, garage_id, payload, status, attempts, last_error)
		VALUES ($1, 'quote.approved', $2, '{}', 'failed', 3, 'broker down'), ($3, 'x', $4, '{}', 'pending', 0, NULL)`,
		uuid.NewString(), e.garage, uuid.NewString(), e.otherGarage)
	require.NoError(t, err)

	_, err = e.pg.Admin.ExecContext(ctx, `
		INSERT INTO notification_log (user_id, channel, template, recipient, status)
		VALUES ($1, 'email', 'quote_ready_for_approval', 'owner@x.test', 'sent'), ($2, 'email', 'password_reset', 'stranger@x.test', 'sent')`,
		e.owner, e.stranger)
	require.NoError(t, err)

	// An e-mail the garage itself sends (it has a garage) belongs to the garage's own log.
	_, err = e.pg.Admin.ExecContext(ctx, `
		INSERT INTO notification_log (user_id, garage_id, channel, template, recipient, status, type)
		VALUES ($1, $2, 'email', 'settlement_paid', 'owner@x.test', 'sent', 'settlement_paid')`,
		e.owner, e.garage)
	require.NoError(t, err)

	e.as(t, e.staff, e.garage, func(ctx context.Context) {
		events, total, err := e.repo.ListOutbox(ctx, e.garage, "", domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, events, 1)
		assert.Equal(t, "failed", events[0].Status)
		assert.Equal(t, "broker down", events[0].LastError)

		none, _, err := e.repo.ListOutbox(ctx, e.garage, "published", domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Empty(t, none)

		notifications, total, err := e.repo.ListNotifications(ctx, e.garage, domain.Page{Page: 1, PageSize: 10})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, notifications, 1)
		assert.Equal(t, "quote_ready_for_approval", notifications[0].Template)
	})
}

// TestAStaleAnswerCannotReopenAGrant is the race of an owner approving while
// the grant is revoked: the revoked grant stays revoked.
func TestAStaleAnswerCannotReopenAGrant(t *testing.T) {
	e := setup(t)
	now := time.Now().UTC()

	var stale, grant *domain.Grant

	e.as(t, e.staff, "", func(ctx context.Context) {
		grant = domain.NewGrant(e.garage, e.staff, "r", time.Hour, false, now)
		require.NoError(t, e.repo.Create(ctx, grant))
	})

	e.as(t, e.owner, e.garage, func(ctx context.Context) {
		var err error

		stale, err = e.repo.Find(ctx, grant.ID)
		require.NoError(t, err)

		other, err := e.repo.Find(ctx, grant.ID)
		require.NoError(t, err)
		require.NoError(t, other.Revoke(now))
		require.NoError(t, e.repo.Update(ctx, other))

		require.NoError(t, stale.Approve(e.owner, now))
		require.ErrorIs(t, e.repo.Update(ctx, stale), domain.ErrGrantNotOpen)
	})

	e.as(t, e.owner, e.garage, func(ctx context.Context) {
		found, err := e.repo.Find(ctx, grant.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.StatusRevoked, found.Status(now))
		assert.Nil(t, found.ApprovedAt)
	})
}

// TestDirectoryNeedsATransaction guards the one place the garages policy is opened.
func TestDirectoryNeedsATransaction(t *testing.T) {
	e := setup(t)

	_, err := e.repo.Garage(t.Context(), e.garage)
	require.Error(t, err)
}
