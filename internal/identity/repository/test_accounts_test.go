package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestDeleteTestAccounts(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresAccountRepository(nil)

	_, err := repo.DeleteTestAccounts(t.Context(), "", now)
	require.Error(t, err, "an empty domain is refused")

	tx := dbtest.New(t).
		ExpectQuery("right(lower(email)", dbtest.NewRows([]any{"acc-1"}, []any{"acc-2"}), nil).WithArgs(func(args []any) {
		assert.Equal(t, []any{"blackbox.vietgara.test", now}, args)
	}).
		ExpectExec("DELETE FROM accounts", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM accounts", dbtest.Tag(0), errBoom)

	deleted, err := repo.DeleteTestAccounts(tx.Context(t.Context()), "Blackbox.Vietgara.Test", now)
	require.ErrorIs(t, err, errBoom, "a failed account is reported")
	assert.EqualValues(t, 1, deleted, "and does not keep the others")

	failing := dbtest.New(t).ExpectQuery("right(lower(email)", nil, errBoom)
	_, err = repo.DeleteTestAccounts(failing.Context(t.Context()), "blackbox.vietgara.test", now)
	require.ErrorIs(t, err, errBoom)
}
