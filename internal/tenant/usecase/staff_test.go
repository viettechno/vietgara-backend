package usecase_test

import (
	"context"
	"errors"
	"testing"

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
