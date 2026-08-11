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

func TestGarageUseCase_Create(t *testing.T) {
	t.Run("creates garage, owner staff record, and publishes garage.created", func(t *testing.T) {
		garages := mocks.NewMockGarageRepository(t)
		staff := mocks.NewMockGarageStaffRepository(t)
		events := mocks.NewMockEventPublisher(t)
		uc := usecase.NewGarageUseCase(garages, staff, events)

		garages.EXPECT().Create(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool {
			return g.AccountID == "acc-1" && g.Name == "Gara A" && g.Status == domain.GarageStatusActive
		})).Return(nil).Once()
		staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool {
			return s.AccountID == "acc-1" && s.GarageID != "" && s.Role == domain.RoleOwner && s.Status == domain.StaffStatusActive
		})).Return(nil).Once()
		events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, mock.MatchedBy(func(payload any) bool {
			m, ok := payload.(map[string]string)
			return ok && m["garage_id"] != "" && m["account_id"] == "acc-1"
		})).Return(nil).Once()

		g, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"})
		require.NoError(t, err)
		assert.NotEmpty(t, g.ID)
	})

	t.Run("one account can own multiple independent garages", func(t *testing.T) {
		garages := mocks.NewMockGarageRepository(t)
		staff := mocks.NewMockGarageStaffRepository(t)
		events := mocks.NewMockEventPublisher(t)
		uc := usecase.NewGarageUseCase(garages, staff, events)

		garages.EXPECT().Create(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool { return g.AccountID == "acc-1" && g.Name == "Gara A" })).Return(nil).Once()
		staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool { return s.AccountID == "acc-1" && s.Role == domain.RoleOwner })).Return(nil).Once()
		events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, mock.MatchedBy(func(payload any) bool {
			m, ok := payload.(map[string]string)
			return ok && m["account_id"] == "acc-1"
		})).Return(nil).Once()

		g1, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"})
		require.NoError(t, err)

		garages.EXPECT().Create(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool { return g.AccountID == "acc-1" && g.Name == "Gara B" })).Return(nil).Once()
		staff.EXPECT().Create(mock.Anything, mock.MatchedBy(func(s *domain.GarageStaff) bool { return s.AccountID == "acc-1" && s.Role == domain.RoleOwner })).Return(nil).Once()
		events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, mock.MatchedBy(func(payload any) bool {
			m, ok := payload.(map[string]string)
			return ok && m["account_id"] == "acc-1"
		})).Return(nil).Once()

		g2, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara B"})
		require.NoError(t, err)

		assert.NotEqual(t, g1.ID, g2.ID)
	})

	t.Run("empty name is rejected", func(t *testing.T) {
		uc := usecase.NewGarageUseCase(mocks.NewMockGarageRepository(t), mocks.NewMockGarageStaffRepository(t), mocks.NewMockEventPublisher(t))

		_, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: ""})

		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an AppError, got %v", err)
		assert.Equal(t, apperror.CodeInvalidArgument, appErr.Code)
	})
}

func TestGarageUseCase_List(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		garages   []*domain.Garage
		wantErr   apperror.Code
	}{
		{
			name:      "returns garages for account",
			accountID: "acc-1",
			garages: []*domain.Garage{
				{ID: "g-1", AccountID: "acc-1", Name: "Gara A", Status: domain.GarageStatusActive},
				{ID: "g-2", AccountID: "acc-1", Name: "Gara B", Status: domain.GarageStatusActive},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockGarageRepository(t)
			uc := usecase.NewGarageUseCase(repo, nil, nil)

			repo.EXPECT().ListByAccountID(mock.Anything, tt.accountID).Return(tt.garages, nil).Once()

			result, err := uc.List(context.Background(), tt.accountID)
			require.NoError(t, err)
			require.Len(t, result, len(tt.garages))
			assert.Equal(t, tt.garages[0].ID, result[0].ID)
			assert.Equal(t, tt.garages[1].ID, result[1].ID)
		})
	}
}

func TestGarageUseCase_Update(t *testing.T) {
	t.Run("updates selected fields on an existing garage", func(t *testing.T) {
		repo := mocks.NewMockGarageRepository(t)
		uc := usecase.NewGarageUseCase(repo, nil, nil)

		name := "Updated Gara"
		taxCode := "TAX-123"
		address := "123 Main St"
		logoURL := "https://cdn.example.com/logo.png"
		existing := &domain.Garage{
			ID:        "g-1",
			AccountID: "acc-1",
			Name:      "Old name",
			TaxCode:   nil,
			Address:   nil,
			LogoURL:   nil,
			Timezone:  "Asia/Ho_Chi_Minh",
			Currency:  "VND",
			Status:    domain.GarageStatusActive,
		}

		repo.EXPECT().FindByID(mock.Anything, "g-1").Return(existing, nil).Once()
		repo.EXPECT().Update(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool {
			return g.ID == "g-1" &&
				g.Name == "Updated Gara" &&
				g.TaxCode != nil && *g.TaxCode == "TAX-123" &&
				g.Address != nil && *g.Address == "123 Main St" &&
				g.LogoURL != nil && *g.LogoURL == "https://cdn.example.com/logo.png"
		})).Return(nil).Once()

		updated, err := uc.Update(context.Background(), usecase.UpdateGarageRequest{
			GarageID: "g-1",
			Name:     &name,
			TaxCode:  &taxCode,
			Address:  &address,
			LogoURL:  &logoURL,
		})
		require.NoError(t, err)
		assert.Equal(t, "Updated Gara", updated.Name)
		assert.NotNil(t, updated.TaxCode)
		assert.Equal(t, "TAX-123", *updated.TaxCode)
	})

	t.Run("returns not found for missing garage", func(t *testing.T) {
		repo := mocks.NewMockGarageRepository(t)
		uc := usecase.NewGarageUseCase(repo, nil, nil)

		repo.EXPECT().FindByID(mock.Anything, "missing-garage").Return(nil, domain.ErrGarageNotFound).Once()

		_, err := uc.Update(context.Background(), usecase.UpdateGarageRequest{GarageID: "missing-garage"})
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.CodeNotFound, appErr.Code)
	})
}
