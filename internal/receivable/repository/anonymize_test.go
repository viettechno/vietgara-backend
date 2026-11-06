package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/receivable/repository"
)

func TestAnonymizeCustomer(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("UPDATE receivables SET customer_name", dbtest.Tag(2), nil).
		ExpectExec("UPDATE receivables SET customer_name", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())
	repo := repository.NewPostgresRepository(nil)

	require.NoError(t, repo.AnonymizeCustomer(ctx, garageID, "c-1"))
	require.ErrorIs(t, repo.AnonymizeCustomer(ctx, garageID, "c-1"), errBoom)
}
