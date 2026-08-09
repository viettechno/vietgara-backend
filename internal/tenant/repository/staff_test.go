package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

func TestPostgresGarageStaffRepository_Create(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			staff *domain.GarageStaff
		}
		wantID      string
		wantCreated time.Time
		wantUpdated time.Time
		wantErr     error
	}{
		{
			name: "inserts the staff member and returns back id and timestamps",
			args: struct{ staff *domain.GarageStaff }{
				staff: &domain.GarageStaff{
					ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2",
					Role: domain.RoleTechnician, Status: domain.StaffStatusActive,
				},
			},
			wantID:      "staff-1",
			wantCreated: time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC),
			wantUpdated: time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC),
		},
		{
			name: "propagates scan errors",
			args: struct{ staff *domain.GarageStaff }{
				staff: &domain.GarageStaff{ID: "s", GarageID: "g", AccountID: "a"},
			},
			wantErr: errors.New("insert boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageStaffRepository(pool)

			if tt.wantErr == nil {
				createdAt := tt.wantCreated
				conn.EXPECT().
					QueryRow(t.Context(), sqlContaining("INSERT INTO garage_staff"), []any{"staff-1", "garage-1", "acc-2", "technician", "active"}).
					Return(valuesRow(t, []any{"staff-1", createdAt, createdAt}))
			} else {
				conn.EXPECT().
					QueryRow(mock.Anything, sqlContaining("INSERT INTO garage_staff"), mock.Anything).
					Return(errRow(t, tt.wantErr))
			}

			err := repo.Create(t.Context(), tt.args.staff)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, tt.args.staff.ID)
			assert.Equal(t, tt.wantCreated, tt.args.staff.CreatedAt)
			assert.Equal(t, tt.wantUpdated, tt.args.staff.UpdatedAt)
		})
	}
}

func TestPostgresGarageStaffRepository_ListByGarageID(t *testing.T) {
	tests := []struct {
		name    string
		args    struct{ garageID string }
		want    []domain.GarageStaff
		wantErr error
	}{
		{
			name: "returns active staff with roles and statuses mapped",
			args: struct{ garageID string }{garageID: "garage-1"},
			want: []domain.GarageStaff{
				{ID: "staff-1", GarageID: "garage-1", AccountID: "acc-1", Role: domain.RoleOwner, Status: domain.StaffStatusActive},
				{ID: "staff-2", GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleServiceAdvisor, Status: domain.StaffStatusActive},
			},
		},
		{
			name:    "propagates query errors",
			args:    struct{ garageID string }{garageID: "garage-1"},
			wantErr: errors.New("query boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageStaffRepository(pool)

			if tt.wantErr == nil {
				rows := resultRows(t, newRowsState([][]any{
					staffScanValues("staff-1", "garage-1", "acc-1", "owner", "active"),
					staffScanValues("staff-2", "garage-1", "acc-2", "service_advisor", "active"),
				}))
				conn.EXPECT().
					Query(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1"), []any{"garage-1"}).
					Return(rows, nil)
			} else {
				conn.EXPECT().
					Query(mock.Anything, sqlContaining("FROM garage_staff WHERE garage_id = $1"), []any{"garage-1"}).
					Return(nil, tt.wantErr)
			}

			got, err := repo.ListByGarageID(t.Context(), tt.args.garageID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			assert.Equal(t, tt.want[0].Role, got[0].Role)
			assert.Equal(t, tt.want[1].Role, got[1].Role)
			assert.Equal(t, tt.want[0].Status, got[0].Status)
		})
	}
}

func TestPostgresGarageStaffRepository_FindByGarageAndAccount(t *testing.T) {
	tests := []struct {
		name string
		args struct {
			garageID  string
			accountID string
		}
		want    *domain.GarageStaff
		wantErr error
	}{
		{
			name: "returns the staff member",
			args: struct {
				garageID  string
				accountID string
			}{garageID: "garage-1", accountID: "acc-2"},
			want: &domain.GarageStaff{
				ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusActive,
			},
		},
		{
			name: "propagates errors untouched (no ErrNoRows mapping here)",
			args: struct {
				garageID  string
				accountID string
			}{garageID: "garage-1", accountID: "nobody"},
			wantErr: pgx.ErrNoRows,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageStaffRepository(pool)

			if tt.wantErr == nil {
				conn.EXPECT().
					QueryRow(t.Context(), sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), []any{"garage-1", "acc-2"}).
					Return(valuesRow(t, staffScanValues("staff-1", "garage-1", "acc-2", "technician", "active")))
			} else {
				conn.EXPECT().
					QueryRow(mock.Anything, sqlContaining("FROM garage_staff WHERE garage_id = $1 AND account_id = $2"), mock.Anything).
					Return(errRow(t, pgx.ErrNoRows))
			}

			got, err := repo.FindByGarageAndAccount(t.Context(), tt.args.garageID, tt.args.accountID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want.GarageID, got.GarageID)
			assert.Equal(t, tt.want.Role, got.Role)
			assert.Equal(t, tt.want.Status, got.Status)
		})
	}
}

func TestPostgresGarageStaffRepository_ListRolesForAccount(t *testing.T) {
	tests := []struct {
		name    string
		args    struct{ accountID string }
		want    []repository.RoleAssignment
		wantErr error
	}{
		{
			name: "returns the role assignments for every active membership",
			args: struct{ accountID string }{accountID: "acc-2"},
			want: []repository.RoleAssignment{
				{GarageID: "garage-1", Role: "owner"},
				{GarageID: "garage-2", Role: "accountant"},
			},
		},
		{
			name:    "propagates query errors",
			args:    struct{ accountID string }{accountID: "acc-2"},
			wantErr: errors.New("query boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, pool := newQuerier(t)
			repo := repository.NewPostgresGarageStaffRepository(pool)

			if tt.wantErr == nil {
				rows := resultRows(t, newRowsState([][]any{
					{"garage-1", "owner"},
					{"garage-2", "accountant"},
				}))
				conn.EXPECT().
					Query(t.Context(), sqlContaining("FROM garage_staff WHERE account_id = $1 AND status = 'active'"), []any{"acc-2"}).
					Return(rows, nil)
			} else {
				conn.EXPECT().
					Query(mock.Anything, sqlContaining("FROM garage_staff WHERE account_id = $1"), []any{"acc-2"}).
					Return(nil, tt.wantErr)
			}

			got, err := repo.ListRolesForAccount(t.Context(), tt.args.accountID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			assert.Equal(t, tt.want, got)
		})
	}
}
