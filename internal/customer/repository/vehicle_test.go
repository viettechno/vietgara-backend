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

func vehicleValues() []any {
	year := 2020

	return []any{
		vehicleID, garageID, customerID, "Tran Binh", "51F-123.45", new("Toyota"), nil, &year,
		nil, nil, true, nil, nil, testTime, testTime,
	}
}

func testVehicle() *domain.Vehicle {
	return &domain.Vehicle{ID: vehicleID, GarageID: garageID, CustomerID: customerID, LicensePlate: "51F-123.45"}
}

func TestPostgresVehicleRepository_CreateAndUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "succeeds"},
		{name: "maps the plate index", err: uniqueViolation("idx_vehicles_garage_plate_unique"), wantErr: domain.ErrLicensePlateTaken},
		{name: "maps a missing owner", err: foreignKeyViolation(), wantErr: domain.ErrCustomerNotFound},
		{name: "keeps unknown constraint errors", err: uniqueViolation("other_index"), wantErr: uniqueViolation("other_index")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			conn, pool := newPool(t)
			repo := repository.NewPostgresVehicleRepository(pool)

			var createRow, updateRow pgx.Row
			if tt.err != nil {
				createRow, updateRow = errRow(t, tt.err), errRow(t, tt.err)
			} else {
				createRow, updateRow = valuesRow(t, testTime, testTime), valuesRow(t, testTime)
			}

			conn.EXPECT().QueryRow(mock.Anything, sqlContaining("INSERT INTO vehicles"), mock.Anything).Return(createRow).Once()
			conn.EXPECT().QueryRow(mock.Anything, sqlContaining("UPDATE vehicles SET customer_id"), mock.Anything).Return(updateRow).Once()

			createErr := repo.Create(t.Context(), testVehicle())
			updateErr := repo.Update(t.Context(), testVehicle())

			if tt.wantErr == nil {
				require.NoError(t, createErr)
				require.NoError(t, updateErr)

				return
			}

			assert.Equal(t, tt.wantErr.Error(), createErr.Error())
			assert.Equal(t, tt.wantErr.Error(), updateErr.Error())
		})
	}
}

func TestPostgresVehicleRepository_FindByID(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresVehicleRepository(pool)

	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("JOIN customers c"), []any{garageID, vehicleID}).Return(valuesRow(t, vehicleValues()...)).Once()
	conn.EXPECT().QueryRow(mock.Anything, sqlContaining("JOIN customers c"), mock.Anything).Return(errRow(t, pgx.ErrNoRows)).Once()

	v, err := repo.FindByID(t.Context(), garageID, vehicleID)
	require.NoError(t, err)
	assert.Equal(t, "Tran Binh", v.CustomerName)
	assert.Equal(t, 2020, *v.Year)
	assert.Nil(t, v.Model)

	_, err = repo.FindByID(t.Context(), garageID, vehicleID)
	require.ErrorIs(t, err, domain.ErrVehicleNotFound)

	_, err = repo.FindByID(t.Context(), garageID, "nope")
	require.ErrorIs(t, err, domain.ErrVehicleNotFound)
}

func TestPostgresVehicleRepository_List(t *testing.T) {
	t.Parallel()

	t.Run("filters by customer and plate", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		customer := customerID
		conn.EXPECT().QueryRow(mock.Anything, sqlContaining("SELECT count(*)"), []any{garageID, &customer, "%30a.9%", "%30A9%", false}).
			Return(valuesRow(t, 1)).Once()
		conn.EXPECT().Query(mock.Anything, sqlContaining("ORDER BY v.created_at DESC"), mock.Anything).
			Return(resultRows(t, vehicleValues()), nil).Once()

		list, total, err := repository.NewPostgresVehicleRepository(pool).List(t.Context(), garageID, usecase.VehicleFilter{
			ListFilter: usecase.ListFilter{Query: "30a.9", Limit: 20}, CustomerID: customerID,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, list, 1)
	})

	t.Run("a malformed customer filter matches nothing", func(t *testing.T) {
		t.Parallel()

		_, pool := newPool(t)
		list, total, err := repository.NewPostgresVehicleRepository(pool).List(t.Context(), garageID, usecase.VehicleFilter{CustomerID: "nope"})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, list)
	})

	t.Run("a vehicle scan failure aborts", func(t *testing.T) {
		t.Parallel()

		conn, pool := newPool(t)
		conn.EXPECT().QueryRow(mock.Anything, mock.Anything, mock.Anything).Return(valuesRow(t, 1)).Once()
		conn.EXPECT().Query(mock.Anything, mock.Anything, mock.Anything).Return(resultRows(t, []any{errBoom}), nil).Once()

		_, _, err := repository.NewPostgresVehicleRepository(pool).List(t.Context(), garageID, usecase.VehicleFilter{ListFilter: usecase.ListFilter{Limit: 20}})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresVehicleRepository_SoftDelete(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresVehicleRepository(pool)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("AND id = $2"), []any{garageID, vehicleID, new(actorID)}).Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("AND id = $2"), mock.Anything).Return(commandTag("0"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("AND customer_id = $2"), []any{garageID, customerID, new(actorID)}).Return(commandTag("2"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("AND customer_id = $2"), mock.Anything).Return(commandTag("0"), errBoom).Once()

	require.NoError(t, repo.SoftDelete(t.Context(), garageID, vehicleID, actorID))
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, vehicleID, actorID), domain.ErrVehicleNotFound)
	require.ErrorIs(t, repo.SoftDelete(t.Context(), garageID, "nope", actorID), domain.ErrVehicleNotFound)

	require.NoError(t, repo.SoftDeleteByCustomer(t.Context(), garageID, customerID, actorID))
	require.ErrorIs(t, repo.SoftDeleteByCustomer(t.Context(), garageID, customerID, actorID), errBoom)
	require.NoError(t, repo.SoftDeleteByCustomer(t.Context(), garageID, "nope", actorID))
}

func TestPostgresVehicleRepository_SetActive(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresVehicleRepository(pool)

	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE vehicles SET is_active"), []any{garageID, vehicleID, false, new(actorID)}).
		Return(commandTag("1"), nil).Once()
	conn.EXPECT().Exec(mock.Anything, sqlContaining("UPDATE vehicles SET is_active"), mock.Anything).Return(commandTag("0"), nil).Once()

	require.NoError(t, repo.SetActive(t.Context(), garageID, vehicleID, false, actorID))
	require.ErrorIs(t, repo.SetActive(t.Context(), garageID, vehicleID, true, actorID), domain.ErrVehicleNotFound)
	require.ErrorIs(t, repo.SetActive(t.Context(), garageID, "nope", true, actorID), domain.ErrVehicleNotFound)
}

func TestPostgresVehicleRepository_IDsByCustomer(t *testing.T) {
	t.Parallel()

	conn, pool := newPool(t)
	repo := repository.NewPostgresVehicleRepository(pool)

	conn.EXPECT().Query(mock.Anything, sqlContaining("SELECT id FROM vehicles"), []any{garageID, customerID}).
		Return(resultRows(t, []any{vehicleID}, []any{"v-2"}), nil).Once()
	conn.EXPECT().Query(mock.Anything, sqlContaining("SELECT id FROM vehicles"), mock.Anything).Return(nil, errBoom).Once()
	conn.EXPECT().Query(mock.Anything, sqlContaining("SELECT id FROM vehicles"), mock.Anything).
		Return(resultRows(t, []any{errBoom}), nil).Once()

	ids, err := repo.IDsByCustomer(t.Context(), garageID, customerID)
	require.NoError(t, err)
	assert.Equal(t, []string{vehicleID, "v-2"}, ids)

	_, err = repo.IDsByCustomer(t.Context(), garageID, customerID)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.IDsByCustomer(t.Context(), garageID, customerID)
	require.ErrorIs(t, err, errBoom)

	ids, err = repo.IDsByCustomer(t.Context(), garageID, "nope")
	require.NoError(t, err)
	assert.Empty(t, ids)
}
