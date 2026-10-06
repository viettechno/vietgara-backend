package repository_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/repository"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
)

func customerValues(consent string) []any {
	phone := "0912345678"

	return []any{
		customerID, garageID, "Tran Binh", &phone, nil, nil, []byte(consent),
		nil, nil, testTime, testTime, 2,
	}
}

func TestPostgresCustomerRepository_Create(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		row     func(t *testing.T) pgx.Row
		wantErr error
	}{
		{name: "scans back timestamps", row: func(t *testing.T) pgx.Row {
			t.Helper()

			return valuesRow(t, testTime, testTime)
		}},
		{name: "maps the phone index", row: func(t *testing.T) pgx.Row {
			t.Helper()

			return errRow(t, uniqueViolation("idx_customers_garage_phone_unique"))
		}, wantErr: domain.ErrPhoneTaken},
		{name: "passes other errors through", row: func(t *testing.T) pgx.Row {
			t.Helper()

			return errRow(t, errBoom)
		}, wantErr: errBoom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			conn, pool := newPool(t)
			conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO customers"), mock.Anything).Return(tt.row(t)).Once()

			c := &domain.Customer{ID: customerID, GarageID: garageID, FullName: "Tran Binh"}
			err := repository.NewPostgresCustomerRepository(pool).Create(t.Context(), c)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testTime, c.CreatedAt)
		})
	}
}

func TestPostgresCustomerRepository_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("maps the row", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("FROM customers c WHERE"), []any{garageID, customerID}).
			Return(valuesRow(t, customerValues(`[{"granted":true,"recorded_at":"2026-09-23T10:00:00Z","recorded_by":"acc-1"}]`)...)).Once()

		c, err := repository.NewPostgresCustomerRepository(pool).FindByID(t.Context(), garageID, customerID)
		require.NoError(t, err)
		assert.Equal(t, "Tran Binh", c.FullName)
		assert.Equal(t, "0912345678", *c.Phone)
		assert.Nil(t, c.Email)
		assert.Equal(t, 2, c.VehicleCount)
		assert.Equal(t, []domain.ConsentRecord{{Granted: true, RecordedAt: testTime, RecordedBy: "acc-1"}}, c.ConsentLog)
	})

	t.Run("no row is not found", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

		_, err := repository.NewPostgresCustomerRepository(pool).FindByID(t.Context(), garageID, customerID)
		require.ErrorIs(t, err, domain.ErrCustomerNotFound)
	})

	t.Run("a malformed id is not found without querying", func(t *testing.T) {
		t.Parallel()

		_, pool := newPool(t)
		_, err := repository.NewPostgresCustomerRepository(pool).FindByID(t.Context(), garageID, "not-a-uuid")
		require.ErrorIs(t, err, domain.ErrCustomerNotFound)
	})

	t.Run("a corrupt consent log fails", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(valuesRow(t, customerValues(`{`)...)).Once()

		_, err := repository.NewPostgresCustomerRepository(pool).FindByID(t.Context(), garageID, customerID)
		require.Error(t, err)
	})
}

func TestPostgresCustomerRepository_Update(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresCustomerRepository(pool)
	consent := []domain.ConsentRecord{{Granted: true, RecordedAt: testTime}}

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE customers SET"), mock.MatchedBy(func(args []any) bool {
		consent, ok := args[6].([]byte)

		return ok && args[0] == garageID && args[1] == customerID && string(consent) == `[{"granted":true,"recorded_at":"2026-09-23T10:00:00Z"}]`
	})).Return(valuesRow(t, testTime)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE customers SET"), mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

	c := &domain.Customer{ID: customerID, GarageID: garageID, FullName: "Binh", ConsentLog: consent}
	require.NoError(t, repo.Update(t.Context(), c))
	assert.Equal(t, testTime, c.UpdatedAt)

	require.ErrorIs(t, repo.Update(t.Context(), &domain.Customer{ID: customerID, GarageID: garageID}), domain.ErrCustomerNotFound)
}

func TestPostgresCustomerRepository_List(t *testing.T) {
	t.Parallel()

	t.Run("counts then fetches the page with search patterns", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("SELECT count(*)"), []any{garageID, `%51f\_1%`, "%51F1%"}).
			Return(valuesRow(t, 3)).Once()
		conn.EXPECT().Query(mock.Anything, sqlContaining("ORDER BY c.created_at DESC"), []any{garageID, `%51f\_1%`, "%51F1%", 2, 2}).
			Return(resultRows(t, customerValues("[]")), nil).Once()

		list, total, err := repository.NewPostgresCustomerRepository(pool).List(t.Context(), garageID, usecase.ListFilter{Query: "51f_1", Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Equal(t, 3, total)
		require.Len(t, list, 1)
		assert.Empty(t, list[0].ConsentLog)
	})

	t.Run("skips the page query when nothing matches", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("SELECT count(*)"), []any{garageID, "", ""}).Return(valuesRow(t, 0)).Once()

		list, total, err := repository.NewPostgresCustomerRepository(pool).List(t.Context(), garageID, usecase.ListFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, list)
	})

	t.Run("propagates failures", func(t *testing.T) {
		t.Parallel()

		for _, fail := range []string{"count", "query", "scan"} {
			conn, pool := newPool(t)

			if fail == "count" {
				conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(errRow(t, errBoom)).Once()
			} else {
				conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(valuesRow(t, 1)).Once()
			}

			switch fail {
			case "query":
				conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom).Once()
			case "scan":
				conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(resultRows(t, []any{errBoom}), nil).Once()
			}

			_, _, err := repository.NewPostgresCustomerRepository(pool).List(t.Context(), garageID, usecase.ListFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom, fail)
		}
	})
}

func TestPostgresCustomerRepository_SoftDelete(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresCustomerRepository(pool)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customers SET deleted_at"), []any{garageID, customerID, new(actorID)}).Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customers SET deleted_at"), []any{garageID, customerID, (*string)(nil)}).Return(commandTag("0"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE customers SET deleted_at"), mock.Anything).Return(commandTag("0"), errBoom).Once()

	require.NoError(t, repo.SoftDelete(t.Context(), garageID, customerID, actorID))
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, customerID, ""), domain.ErrCustomerNotFound)
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, customerID, actorID), errBoom)
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, "bad", actorID), domain.ErrCustomerNotFound)
}

func TestPostgresCustomerRepository_FindByEmailAcrossGarages(t *testing.T) {
	t.Parallel()

	t.Run("returns the matching ids", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().Query(mock.Anything, sqlContaining("FROM find_customers_by_email($1)"), []any{"binh@example.com"}).
			Return(resultRows(t, []any{"c-1", "g-1"}, []any{"c-2", "g-2"}), nil).Once()

		refs, err := repository.NewPostgresCustomerRepository(pool).FindByEmailAcrossGarages(t.Context(), "binh@example.com")

		require.NoError(t, err)
		assert.Equal(t, []repository.CustomerRef{{ID: "c-1", GarageID: "g-1"}, {ID: "c-2", GarageID: "g-2"}}, refs)
	})

	t.Run("query and scan errors", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(nil, errBoom).Once()

		_, err := repository.NewPostgresCustomerRepository(pool).FindByEmailAcrossGarages(t.Context(), "x@y.z")
		require.ErrorIs(t, err, errBoom)

		conn, pool = newPool(t)
		conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(resultRows(t, []any{errBoom}), nil).Once()
		_, err = repository.NewPostgresCustomerRepository(pool).FindByEmailAcrossGarages(t.Context(), "x@y.z")
		require.ErrorIs(t, err, errBoom)
	})
}
