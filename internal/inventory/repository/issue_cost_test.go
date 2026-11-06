package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestPostgresRepository_IssueCosts(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)
	ids := []string{"ro-1", "ro-2"}

	tx := dbtest.New(t).
		ExpectQuery("FROM stock_issues WHERE", dbtest.NewRows([]any{"ro-1", true}, []any{"ro-2", false}), nil).
		ExpectQuery("FROM stock_issue_lines l", dbtest.NewRows([]any{"ro-1", "l-1", int64(160_000)}), nil).
		ExpectQuery("FROM stock_issues WHERE", nil, errBoom).
		ExpectQuery("FROM stock_issues WHERE", dbtest.NewRows([]any{"ro-1", true}).WithScanError(errBoom), nil).
		ExpectQuery("FROM stock_issues WHERE", dbtest.NewRows([]any{"ro-1", true}), nil).
		ExpectQuery("FROM stock_issue_lines l", nil, errBoom).
		ExpectQuery("FROM stock_issues WHERE", dbtest.NewRows([]any{"ro-1", true}), nil).
		ExpectQuery("FROM stock_issue_lines l", dbtest.NewRows([]any{"ro-1", "l-1", int64(1)}).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	got, err := repo.IssueCosts(ctx, garageID, ids)
	require.NoError(t, err)
	assert.Equal(t, map[string]repository.IssueCost{
		"ro-1": {HasIssue: true, Posted: true, ByLine: map[string]int64{"l-1": 160_000}},
		"ro-2": {HasIssue: true, ByLine: map[string]int64{}},
	}, got)

	for range 4 {
		_, err = repo.IssueCosts(ctx, garageID, ids)
		require.ErrorIs(t, err, errBoom)
	}
}

func TestPostgresRepository_IssueCosts_PostedBetweenTheQueries(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresRepository(nil)

	tx := dbtest.New(t).
		ExpectQuery("FROM stock_issues WHERE", dbtest.NewRows(), nil).
		ExpectQuery("FROM stock_issue_lines l", dbtest.NewRows([]any{"ro-1", "l-1", int64(5)}), nil)

	got, err := repo.IssueCosts(tx.Context(t.Context()), garageID, []string{"ro-1"})
	require.NoError(t, err)
	assert.Equal(t, repository.IssueCost{HasIssue: true, Posted: true, ByLine: map[string]int64{"l-1": 5}}, got["ro-1"])
}
