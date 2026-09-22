package usecase_test

import (
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

func TestStaffUseCase_FindCandidate(t *testing.T) {
	t.Parallel()

	account := &usecase.AccountSummary{ID: "acc-2", FullName: "Tran Binh", Active: true}

	tests := []struct {
		name       string
		identifier string
		setup      func(*mocks.MockAccountDirectory, *mocks.MockGarageStaffRepository)
		wantCode   apperror.Code
		wantStaff  bool
	}{
		{
			name:       "email goes to the email lookup",
			identifier: "  Binh@Example.com ",
			setup: func(d *mocks.MockAccountDirectory, s *mocks.MockGarageStaffRepository) {
				d.EXPECT().FindByEmail(mock.Anything, "Binh@Example.com").Return(account, nil).Once()
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").Return(nil, errors.New("no rows")).Once()
			},
		},
		{
			name:       "phone goes to the phone lookup",
			identifier: "+84 912 345 678",
			setup: func(d *mocks.MockAccountDirectory, s *mocks.MockGarageStaffRepository) {
				d.EXPECT().FindByPhone(mock.Anything, "+84 912 345 678").Return(account, nil).Once()
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").
					Return(&domain.GarageStaff{Status: domain.StaffStatusActive}, nil).Once()
			},
			wantStaff: true,
		},
		{
			name:       "inactive membership is not counted as staff",
			identifier: "0912345678",
			setup: func(d *mocks.MockAccountDirectory, s *mocks.MockGarageStaffRepository) {
				d.EXPECT().FindByPhone(mock.Anything, "0912345678").Return(account, nil).Once()
				s.EXPECT().FindByGarageAndAccount(mock.Anything, "garage-1", "acc-2").
					Return(&domain.GarageStaff{Status: domain.StaffStatusInactive}, nil).Once()
			},
		},
		{
			name:       "too short to be a phone number",
			identifier: "0912",
			wantCode:   apperror.CodeInvalidArgument,
		},
		{
			name:       "no matching account",
			identifier: "nobody@example.com",
			setup: func(d *mocks.MockAccountDirectory, _ *mocks.MockGarageStaffRepository) {
				d.EXPECT().FindByEmail(mock.Anything, "nobody@example.com").Return(nil, domain.ErrAccountNotFound).Once()
			},
			wantCode: apperror.CodeNotFound,
		},
		{
			name:       "directory failure",
			identifier: "binh@example.com",
			setup: func(d *mocks.MockAccountDirectory, _ *mocks.MockGarageStaffRepository) {
				d.EXPECT().FindByEmail(mock.Anything, "binh@example.com").Return(nil, errors.New("db down")).Once()
			},
			wantCode: apperror.CodeInternal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			directory := mocks.NewMockAccountDirectory(t)
			staffRepo := mocks.NewMockGarageStaffRepository(t)

			if tc.setup != nil {
				tc.setup(directory, staffRepo)
			}

			uc := usecase.NewStaffUseCase(staffRepo, usecase.WithAccountDirectory(directory))

			got, err := uc.FindCandidate(t.Context(), "garage-1", tc.identifier)
			if tc.wantCode != "" {
				appErr, ok := apperror.As(err)
				require.True(t, ok)
				assert.Equal(t, tc.wantCode, appErr.Code)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "acc-2", got.Account.ID)
			assert.Equal(t, tc.wantStaff, got.AlreadyStaff)
		})
	}

	t.Run("without a directory the lookup is unavailable", func(t *testing.T) {
		t.Parallel()

		_, err := usecase.NewStaffUseCase(mocks.NewMockGarageStaffRepository(t)).FindCandidate(t.Context(), "garage-1", "binh@example.com")

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeFailedPrecondition, appErr.Code)
	})
}

func TestStaffUseCase_AddRequiresActiveAccount(t *testing.T) {
	t.Parallel()

	req := usecase.AddStaffRequest{GarageID: "garage-1", AccountID: "acc-2", Role: domain.RoleTechnician}

	t.Run("inactive account is rejected", func(t *testing.T) {
		t.Parallel()

		directory := mocks.NewMockAccountDirectory(t)
		directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{"acc-2": {ID: "acc-2"}}, nil).Once()

		_, err := usecase.NewStaffUseCase(mocks.NewMockGarageStaffRepository(t), usecase.WithAccountDirectory(directory)).Add(t.Context(), req)

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeFailedPrecondition, appErr.Code)
	})

	t.Run("unknown account is rejected", func(t *testing.T) {
		t.Parallel()

		directory := mocks.NewMockAccountDirectory(t)
		directory.EXPECT().ListByIDs(mock.Anything, []string{"acc-2"}).Return(map[string]*usecase.AccountSummary{}, nil).Once()

		_, err := usecase.NewStaffUseCase(mocks.NewMockGarageStaffRepository(t), usecase.WithAccountDirectory(directory)).Add(t.Context(), req)

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeNotFound, appErr.Code)
	})
}
