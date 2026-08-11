package usecase_test

import (
	"context"
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
	t.Run("assigns a valid role successfully", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, nil).Once()
		staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.GarageID == "garage-1" && s.AccountID == "acc-2" && s.Role == domain.RoleTechnician && s.Status == domain.StaffStatusActive
		})).Return(nil).Once()

		s, err := uc.Add(context.Background(), usecase.AddStaffRequest{
			GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician,
		})
		require.NoError(t, err)
		assert.Equal(t, domain.RoleTechnician, s.Role)
	})

	t.Run("rejects an invalid role", func(t *testing.T) {
		uc := usecase.NewStaffUseCase(mocks.NewMockGarageStaffRepository(t))

		_, err := uc.Add(context.Background(), usecase.AddStaffRequest{
			GarageID: "garage-1", AccountID: "acc-2", Role: domain.Role("not-a-real-role"),
		})

		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an AppError, got %v", err)
		assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code)
	})

	t.Run("rejects adding the same account twice to the same garage", func(t *testing.T) {
		staff := mocks.NewMockGarageStaffRepository(t)
		uc := usecase.NewStaffUseCase(staff)

		staff.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(&domain.GarageStaff{
			GarageID:  "garage-1",
			AccountID: "acc-2",
			Role:      domain.RoleTechnician,
			Status:    domain.StaffStatusActive,
		}, nil).Once()

		_, err := uc.Add(context.Background(), usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician})
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an AppError, got %v", err)
		assert.Equal(t, apperror.CodeAlreadyExists, appErr.Code)
	})
}

// ---------------------------------------------------------------------------
// Cross-tenant isolation test (documents the expected RLS behavior at the
// usecase level using the mock repo -- the REAL guarantee is enforced by
// Postgres RLS + TenantContextInterceptor, verified separately via an
// integration test against a real database per LLD Section 6).
// ---------------------------------------------------------------------------

func TestStaffUseCase_List_DoesNotLeakOtherGarageStaff(t *testing.T) {
	staff := mocks.NewMockGarageStaffRepository(t)
	uc := usecase.NewStaffUseCase(staff)

	staff.EXPECT().ListByGarageID(mock.Anything, "garage-A").Return([]*domain.GarageStaff{{
		ID:        "staff-1",
		GarageID:  "garage-A",
		AccountID: "acc-1",
		Role:      domain.RoleTechnician,
		Status:    domain.StaffStatusActive,
	}}, nil).Once()

	result, err := uc.List(context.Background(), "garage-A")
	require.NoError(t, err)

	require.Len(t, result, 1)
	assert.Equal(t, "acc-1", result[0].AccountID)
}
