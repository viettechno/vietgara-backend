//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// TestIssueCostsAreTheSnapshotsOfPostedIssues reads the cost of stock issues
// on a real database: quantity times the unit cost snapshot, per repair order
// line, from posted issues only (FR-RPT-05).
func TestIssueCostsAreTheSnapshotsOfPostedIssues(t *testing.T) {
	t.Parallel()

	pg := pgtest.StartMigrated(t)
	ctx := t.Context()
	owner, garage := uuid.NewString(), uuid.NewString()

	_, err := pg.Admin.ExecContext(ctx, `INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@x.test', 'Owner')`, owner)
	require.NoError(t, err)

	_, err = pg.Admin.ExecContext(ctx, `INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'Gara')`, garage, owner)
	require.NoError(t, err)

	posted, draft, none := uuid.NewString(), uuid.NewString(), uuid.NewString()
	lineA, lineB, lineC := uuid.NewString(), uuid.NewString(), uuid.NewString()

	tx, err := pg.Admin.BeginTx(ctx, nil)
	require.NoError(t, err)

	_, err = tx.ExecContext(ctx, `SET LOCAL session_replication_role = replica`)
	require.NoError(t, err)

	issue := func(orderID, status string) string {
		id := uuid.NewString()
		_, execErr := tx.ExecContext(ctx, `INSERT INTO stock_issues (id, garage_id, number, repair_order_id, status) VALUES ($1, $2, $3, $4, $5::stock_issue_status)`,
			id, garage, "PX-"+id[:8], orderID, status)
		require.NoError(t, execErr)

		return id
	}
	issueLine := func(issueID, orderLine, quantity string, cost *string) {
		_, execErr := tx.ExecContext(ctx, `INSERT INTO stock_issue_lines (garage_id, stock_issue_id, repair_order_line_id, part_id, entered_quantity, quantity, unit_cost)
			VALUES ($1, $2, $3, $4, $5::numeric, $5::numeric, $6::numeric)`, garage, issueID, orderLine, uuid.NewString(), quantity, cost)
		require.NoError(t, execErr)
	}

	cost := func(s string) *string { return &s }

	p := issue(posted, "posted")
	issueLine(p, lineA, "2.5", cost("70000.3333")) // 175,000.83325: whole VND, half up
	issueLine(p, lineB, "1", nil)                  // no snapshot: 0

	d := issue(draft, "draft")
	issueLine(d, lineC, "3", nil)

	require.NoError(t, tx.Commit())

	pool := pg.AppPool(t)
	repo := repository.NewPostgresRepository(pool)

	require.NoError(t, platformdb.NewTxManager(pool).WithTransaction(ctx, platformdb.Scope{UserID: owner, GarageID: garage},
		func(ctx context.Context) error {
			got, err := repo.IssueCosts(ctx, garage, []string{posted, draft, none})
			require.NoError(t, err)

			assert.Equal(t, repository.IssueCost{HasIssue: true, Posted: true, ByLine: map[string]int64{lineA: 175_001, lineB: 0}}, got[posted])
			assert.Equal(t, repository.IssueCost{HasIssue: true, ByLine: map[string]int64{}}, got[draft], "a draft issue has no cost yet")

			_, has := got[none]
			assert.False(t, has, "an order with no issue has no entry")

			return nil
		}))
}
