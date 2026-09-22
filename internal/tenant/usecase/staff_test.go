package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/internal/tenant/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

func TestStaffUseCase_Add(t *testing.T) {
	tests := []struct {
		name         string
		req          usecase.AddStaffRequest
		existing     *domain.GarageStaff
		findErr      error
		createErr    error
		expectCreate bool
		wantCode     apperror.Code
		wantRole     domain.Role
	}{
		{
			name:         "assigns a valid role",
			req:          usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician},
			expectCreate: true,
			wantRole:     domain.RoleTechnician,
		},
		{
			name:     "rejects an invalid role",
			req:      usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.Role("not-a-real-role")},
			wantCode: apperror.CodeInvalidArgument,
		},
		{
			name:     "rejects an already active membership",
			req:      usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician},
			existing: &domain.GarageStaff{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusActive},
			wantCode: apperror.CodeAlreadyExists,
		},
		{
			name:         "allows re-adding an inactive membership",
			req:          usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician},
			existing:     &domain.GarageStaff{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusInactive},
			expectCreate: true,
			wantRole:     domain.RoleTechnician,
		},
		{
			name:         "proceeds when the membership lookup fails",
			req:          usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician},
			findErr:      errors.New("boom"),
			expectCreate: true,
			wantRole:     domain.RoleTechnician,
		},
		{
			name:         "fails when the staff record cannot be created",
			req:          usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician},
			createErr:    errors.New("boom"),
			expectCreate: true,
			wantCode:     apperror.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staff := mocks.NewMockGarageStaffRepository(t)
			uc := usecase.NewStaffUseCase(staff)

			if tt.wantCode != apperror.CodeInvalidArgument {
				staff.EXPECT().FindByGarageAndAccount(mock.Anything, tt.req.GarageID, tt.req.AccountID).Return(tt.existing, tt.findErr).Once()
			}

			if tt.expectCreate {
				staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
					return s.GarageID == tt.req.GarageID && s.AccountID == tt.req.AccountID && s.Role == tt.req.Role && s.Status == domain.StaffStatusActive
				})).Return(tt.createErr).Once()
			}

			s, err := uc.Add(context.Background(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, s)
			assert.Equal(t, tt.wantRole, s.Role)
			assert.Equal(t, domain.StaffStatusActive, s.Status)
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-tenant isolation test (documents the expected RLS behavior at the
// usecase level using the mock repo -- the REAL guarantee is enforced by
// Postgres RLS + TenantContextInterceptor, verified separately via an
// integration test against a real database per LLD Section 6).
// ---------------------------------------------------------------------------

func TestStaffUseCase_List(t *testing.T) {
	tests := []struct {
		name     string
		garageID string
		list     []*domain.GarageStaff
		listErr  error
		wantCode apperror.Code
	}{
		{
			name:     "returns only this garage's staff",
			garageID: "garage-A",
			list: []*domain.GarageStaff{
				{ID: "staff-1", GarageID: "garage-A", AccountID: "acc-1", Role: domain.RoleTechnician, Status: domain.StaffStatusActive},
			},
		},
		{
			name:     "fails when the staff list cannot be loaded",
			garageID: "garage-A",
			listErr:  errors.New("boom"),
			wantCode: apperror.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staff := mocks.NewMockGarageStaffRepository(t)
			uc := usecase.NewStaffUseCase(staff)

			staff.EXPECT().ListByGarageID(mock.Anything, tt.garageID).Return(tt.list, tt.listErr).Once()

			result, err := uc.List(context.Background(), tt.garageID)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.list, result)
		})
	}
}

func activeTechnicianStaff() *domain.GarageStaff {
	return &domain.GarageStaff{
		ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusActive,
	}
}

func TestStaffUseCase_Update(t *testing.T) {
	activeTechnician := activeTechnicianStaff

	t.Run("deactivates a technician", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()
		staff.EXPECT().Update(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.ID == "staff-1" && s.Status == domain.StaffStatusInactive
		})).Return(nil).Once()

		s, err := uc.Deactivate(context.Background(), "garage-1", "staff-1")
		require.NoError(t, err)
		assert.Equal(t, domain.StaffStatusInactive, s.Status)
	})

	t.Run("reactivates an inactive membership", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		inactive := &domain.GarageStaff{
			ID: "staff-1", GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician, Status: domain.StaffStatusInactive,
		}
		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(inactive, nil).Once()
		staff.EXPECT().Update(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.Status == domain.StaffStatusActive
		})).Return(nil).Once()

		active := domain.StaffStatusActive
		s, err := uc.Update(context.Background(), usecase.UpdateStaffRequest{StaffID: "staff-1", GarageID: "garage-1", Status: &active})
		require.NoError(t, err)
		assert.Equal(t, domain.StaffStatusActive, s.Status)
	})

	t.Run("rejects deactivating the owner", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		owner := &domain.GarageStaff{
			ID: "staff-1", GarageID: "garage-1", AccountID: "acc-1", Role: domain.RoleOwner, Status: domain.StaffStatusActive,
		}
		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(owner, nil).Once()

		_, err := uc.Deactivate(context.Background(), "garage-1", "staff-1")
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		staff.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	})

	t.Run("rejects an invalid role", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()

		bad := domain.Role("admin")
		_, err := uc.Update(context.Background(), usecase.UpdateStaffRequest{StaffID: "staff-1", GarageID: "garage-1", Role: &bad})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("returns not found for a missing membership", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "missing").Return(nil, pgx.ErrNoRows).Once()

		_, err := uc.Deactivate(context.Background(), "garage-1", "missing")
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("returns not found when the garage does not match", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()

		_, err := uc.Deactivate(context.Background(), "garage-other", "staff-1")
		assertAppError(t, err, apperror.CodeNotFound)
		staff.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	})
}

func TestStaffUseCase_UpdateRoleAndErrors(t *testing.T) {
	activeTechnician := activeTechnicianStaff

	t.Run("changes the role", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()
		staff.EXPECT().Update(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.Role == domain.RoleAccountant && s.Status == domain.StaffStatusActive
		})).Return(nil).Once()

		role := domain.RoleAccountant
		s, err := uc.Update(context.Background(), usecase.UpdateStaffRequest{StaffID: "staff-1", GarageID: "garage-1", Role: &role})
		require.NoError(t, err)
		assert.Equal(t, domain.RoleAccountant, s.Role)
	})

	t.Run("rejects assigning the owner role", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()

		role := domain.RoleOwner
		_, err := uc.Update(context.Background(), usecase.UpdateStaffRequest{StaffID: "staff-1", GarageID: "garage-1", Role: &role})
		assertAppError(t, err, apperror.CodeInvalidArgument)
		staff.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	})

	t.Run("rejects an invalid status", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()

		bad := domain.StaffStatus("removed")
		_, err := uc.Update(context.Background(), usecase.UpdateStaffRequest{StaffID: "staff-1", GarageID: "garage-1", Status: &bad})
		assertAppError(t, err, apperror.CodeInvalidArgument)
		staff.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	})

	t.Run("fails when the lookup fails", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(nil, errors.New("boom")).Once()

		_, err := uc.Deactivate(context.Background(), "garage-1", "staff-1")
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("fails when the update is rejected", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByID(mock.Anything, "staff-1").Return(activeTechnician(), nil).Once()
		staff.EXPECT().Update(mock.Anything, mock.Anything).Return(errors.New("boom")).Once()

		_, err := uc.Deactivate(context.Background(), "garage-1", "staff-1")
		assertAppError(t, err, apperror.CodeInternal)
	})
}
