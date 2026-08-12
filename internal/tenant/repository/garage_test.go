package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

func TestPostgresGarageRepository_FindByID(t *testing.T) {
	errScanBoom := errors.New("scan boom")

	tests := []struct {
		name    string
		id      string
		kind    string
		row     func(t *testing.T) pgx.Row
		want    *domain.Garage
		wantErr error
	}{
		{
			name: "returns the garage with the tenant status mapped",
			id:   "garage-1",
			kind: "ok",
			row: func(t *testing.T) pgx.Row {
				return valuesRow(t, garageScanValues("garage-1", "acc-1", "Gara A", "active"))
			},
			want: &domain.Garage{
				ID:        "garage-1",
				AccountID: "acc-1",
				Name:      "Gara A",
				Timezone:  "Asia/Ho_Chi_Minh",
				Currency:  "VND",
				Status:    domain.GarageStatusActive,
				CreatedAt: testTimestamp,
			},
		},
		{
			name:    "maps ErrNoRows to ErrGarageNotFound",
			id:      "garage-1",
			kind:    "not-found",
			row:     func(t *testing.T) pgx.Row { return errRow(t, pgx.ErrNoRows) },
			wantErr: domain.ErrGarageNotFound,
		},
		{
			name:    "propagates scan errors",
			id:      "garage-1",
			kind:    "scan-error",
			row:     func(t *testing.T) pgx.Row { return errRow(t, errScanBoom) },
			wantErr: errScanBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageRepository(pool)

			if tt.kind == "ok" {
				conn.EXPECT().
					QueryRow(t.Context(), sqlContaining("FROM garages WHERE id = $1"), []any{tt.id}).
					Return(tt.row(t))
			} else {
				conn.EXPECT().
					QueryRow(mock.Anything, sqlContaining("FROM garages WHERE id = $1"), []any{tt.id}).
					Return(tt.row(t))
			}

			g, err := repo.FindByID(t.Context(), tt.id)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.ID, g.ID)
			assert.Equal(t, tt.want.AccountID, g.AccountID)
			assert.Equal(t, tt.want.Name, g.Name)
			assert.Equal(t, tt.want.Timezone, g.Timezone)
			assert.Equal(t, tt.want.Currency, g.Currency)
			assert.Equal(t, tt.want.Status, g.Status)
			require.Nil(t, g.TaxCode)
			assert.Equal(t, tt.want.CreatedAt, g.CreatedAt)
		})
	}
}

func TestPostgresGarageRepository_ListByAccountID(t *testing.T) {
	queryBoom := errors.New("query boom")
	scanBoom := errors.New("scan boom")

	tests := []struct {
		name       string
		rows       [][]any
		scanAt     int
		scanErr    error
		queryErr   error
		wantIDs    []string
		wantStatus []domain.GarageStatus
		wantErr    error
	}{
		{
			name: "returns all rows in order and closes the result set",
			rows: [][]any{
				garageScanValues("garage-1", "acc-1", "Gara A", "active"),
				garageScanValues("garage-2", "acc-1", "Gara B", "inactive"),
			},
			wantIDs:    []string{"garage-1", "garage-2"},
			wantStatus: []domain.GarageStatus{domain.GarageStatusActive, domain.GarageStatusInactive},
		},
		{
			name: "returns an empty slice when there are no rows",
			rows: nil,
		},
		{
			name:     "propagates query errors",
			queryErr: queryBoom,
			wantErr:  queryBoom,
		},
		{
			name: "aborts on a scan error and still closes the result set",
			rows: [][]any{
				garageScanValues("garage-1", "acc-1", "Gara A", "active"),
				garageScanValues("garage-2", "acc-1", "Gara B", "active"),
			},
			scanAt:  1,
			scanErr: scanBoom,
			wantErr: scanBoom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageRepository(pool)

			st := newRowsState(tt.rows)
			st.scanAt, st.scanErr = tt.scanAt, tt.scanErr

			if tt.queryErr != nil {
				conn.EXPECT().
					Query(mock.Anything, sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
					Return(nil, tt.queryErr)
			} else {
				rows := resultRows(t, st)
				conn.EXPECT().
					Query(t.Context(), sqlContaining("FROM garages WHERE account_id = $1"), []any{"acc-1"}).
					Return(rows, nil)
			}

			list, err := repo.ListByAccountID(t.Context(), "acc-1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)

			if len(tt.wantIDs) == 0 {
				assert.Empty(t, list)
				return
			}

			require.Len(t, list, len(tt.wantIDs))

			for i := range tt.wantIDs {
				assert.Equal(t, tt.wantIDs[i], list[i].ID)

				if i < len(tt.wantStatus) {
					assert.Equal(t, tt.wantStatus[i], list[i].Status)
				}
			}
		})
	}
}

func TestPostgresGarageRepository_Create(t *testing.T) {
	boom := errors.New("insert boom")
	createdAt := time.Date(2026, 8, 9, 13, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		garage      *domain.Garage
		row         func(t *testing.T) pgx.Row
		args        []any
		wantID      string
		wantCreated time.Time
		wantUpdated time.Time
		wantErr     error
	}{
		{
			name: "inserts the garage and scans back id and timestamps",
			garage: &domain.Garage{
				ID:        "garage-new",
				AccountID: "acc-1",
				Name:      "Gara A",
				Timezone:  "Asia/Ho_Chi_Minh",
				Currency:  "VND",
				Status:    domain.GarageStatusActive,
			},
			args: []any{
				"garage-new", "acc-1", "Gara A", (*string)(nil), (*string)(nil), (*string)(nil),
				"Asia/Ho_Chi_Minh", "VND", "active",
			},
			row: func(t *testing.T) pgx.Row {
				return valuesRow(t, []any{"garage-new", createdAt, createdAt})
			},
			wantID:      "garage-new",
			wantCreated: createdAt,
			wantUpdated: createdAt,
		},
		{
			name: "propagates scan errors",
			garage: &domain.Garage{
				ID:        "garage-new",
				AccountID: "acc-2",
				Name:      "Gara A",
				Timezone:  "Asia/Ho_Chi_Minh",
				Currency:  "VND",
				Status:    domain.GarageStatusActive,
			},
			args: []any{
				"garage-new", "acc-2", "Gara A", (*string)(nil), (*string)(nil), (*string)(nil),
				"Asia/Ho_Chi_Minh", "VND", "active",
			},
			row: func(t *testing.T) pgx.Row {
				return errRow(t, boom)
			},
			wantErr: boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageRepository(pool)

			conn.EXPECT().
				QueryRow(t.Context(), sqlContaining("INSERT INTO garages"), tt.args).
				Return(tt.row(t))

			err := repo.Create(t.Context(), tt.garage)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, tt.garage.ID)
			assert.Equal(t, tt.wantCreated, tt.garage.CreatedAt)
			assert.Equal(t, tt.wantUpdated, tt.garage.UpdatedAt)
		})
	}
}

func TestPostgresGarageRepository_Update(t *testing.T) {
	boom := errors.New("update boom")

	tests := []struct {
		name    string
		garage  *domain.Garage
		args    []any
		execErr error
		wantErr error
	}{
		{
			name: "updates all editable fields",
			garage: &domain.Garage{
				ID:       "garage-1",
				Name:     "Gara A",
				Timezone: "Asia/Ho_Chi_Minh",
				Currency: "VND",
				Status:   domain.GarageStatusActive,
			},
			args: []any{"Gara A", (*string)(nil), (*string)(nil), (*string)(nil), "Asia/Ho_Chi_Minh", "VND", "active", "garage-1"},
		},
		{
			name:    "propagates errors",
			garage:  &domain.Garage{ID: "garage-1"},
			args:    []any{(*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), "garage-1"},
			execErr: boom,
			wantErr: boom,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageRepository(pool)

			if tt.wantErr != nil {
				conn.EXPECT().
					Exec(mock.Anything, sqlContaining("UPDATE garages SET"), mock.Anything).
					Return(pgconn.CommandTag{}, tt.execErr)
			} else {
				conn.EXPECT().
					Exec(t.Context(), sqlContaining("UPDATE garages SET"), tt.args).
					Return(pgconn.CommandTag{}, nil)
			}

			err := repo.Update(t.Context(), tt.garage)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}
