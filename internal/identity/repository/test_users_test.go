package repository_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/identity/repository"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

func TestDeleteTestUsers(t *testing.T) {
	t.Parallel()

	repo := repository.NewPostgresUserRepository(nil)

	_, err := repo.DeleteTestUsers(t.Context(), "", now)
	require.Error(t, err, "an empty domain is refused")

	tx := dbtest.New(t).
		ExpectQuery("right(lower(email)", dbtest.NewRows([]any{"user-1"}, []any{"user-2"}), nil).WithArgs(func(args []any) {
		assert.Equal(t, []any{"blackbox.vietgara.test", now}, args)
	}).
		ExpectExec("DELETE FROM users", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM users", dbtest.Tag(0), errBoom)

	deleted, err := repo.DeleteTestUsers(tx.Context(t.Context()), "Blackbox.Vietgara.Test", now)
	require.ErrorIs(t, err, errBoom, "a failed user is reported")
	assert.EqualValues(t, 1, deleted, "and does not keep the others")

	failing := dbtest.New(t).ExpectQuery("right(lower(email)", nil, errBoom)
	_, err = repo.DeleteTestUsers(failing.Context(t.Context()), "blackbox.vietgara.test", now)
	require.ErrorIs(t, err, errBoom)
}
