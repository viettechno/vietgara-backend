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

// assertAppError asserts that err is an AppError carrying wantCode.
func assertAppError(t *testing.T, err error, wantCode apperror.Code) {
	t.Helper()

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an AppError, got %v", err)
	assert.Equal(t, wantCode, appErr.Code)
}

// newGarageCreateMatcher matches the garage passed to repository.Create.
func newGarageCreateMatcher(accountID, name string) any {
	return mock.MatchedBy(func(g *domain.Garage) bool {
		return g.AccountID == accountID && g.Name == name && g.Status == domain.GarageStatusActive
	})
}

// newOwnerStaffCreateMatcher matches the owner staff record passed to
// repository.Create.
func newOwnerStaffCreateMatcher(accountID string) any {
	return mock.MatchedBy(func(s *domain.GarageStaff) bool {
		return s.AccountID == accountID && s.GarageID != "" && s.Role == domain.RoleOwner && s.Status == domain.StaffStatusActive
	})
}

// newCreatedEventMatcher matches the payload of a garage.created event.
func newCreatedEventMatcher(accountID string) any {
	return mock.MatchedBy(func(payload any) bool {
		m, ok := payload.(map[string]string)
		return ok && m["garage_id"] != "" && m["account_id"] == accountID
	})
}

func testGarage() *domain.Garage {
	return &domain.Garage{
		ID:        "g-1",
		AccountID: "acc-1",
		Name:      "Old name",
		Timezone:  "Asia/Ho_Chi_Minh",
		Currency:  "VND",
		Status:    domain.GarageStatusActive,
	}
}

func TestGarageUseCase_Create(t *testing.T) {
	tests := []struct {
		name               string
		req                usecase.CreateGarageRequest
		withEvents         bool
		expectGarageCreate bool
		expectStaffCreate  bool
		expectPublish      bool
		garageErr          error
		staffErr           error
		publishErr         error
		wantCode           apperror.Code
		wantTimezone       string
		wantCurrency       string
	}{
		{
			name:               "creates garage, owner staff record, and publishes garage.created",
			req:                usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"},
			withEvents:         true,
			expectGarageCreate: true,
			expectStaffCreate:  true,
			expectPublish:      true,
			wantTimezone:       "Asia/Ho_Chi_Minh",
			wantCurrency:       "VND",
		},
		{
			name: "applies the requested timezone and currency",
			req: usecase.CreateGarageRequest{
				AccountID: "acc-1",
				Name:      "Gara A",
				Timezone:  new("Asia/Tokyo"),
				Currency:  new("JPY"),
			},
			withEvents:         true,
			expectGarageCreate: true,
			expectStaffCreate:  true,
			expectPublish:      true,
			wantTimezone:       "Asia/Tokyo",
			wantCurrency:       "JPY",
		},
		{
			name:               "proceeds without an event publisher",
			req:                usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"},
			expectGarageCreate: true,
			expectStaffCreate:  true,
			wantTimezone:       "Asia/Ho_Chi_Minh",
			wantCurrency:       "VND",
		},
		{
			name:     "rejects an empty name",
			req:      usecase.CreateGarageRequest{AccountID: "acc-1"},
			wantCode: apperror.CodeInvalidArgument,
		},
		{
			name:               "fails when the garage cannot be created",
			req:                usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"},
			expectGarageCreate: true,
			garageErr:          errors.New("boom"),
			wantCode:           apperror.CodeInternal,
		},
		{
			name:               "fails when the owner staff record cannot be created",
			req:                usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"},
			expectGarageCreate: true,
			expectStaffCreate:  true,
			staffErr:           errors.New("boom"),
			wantCode:           apperror.CodeInternal,
		},
		{
			name:               "fails when the event cannot be published",
			req:                usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"},
			withEvents:         true,
			expectGarageCreate: true,
			expectStaffCreate:  true,
			expectPublish:      true,
			publishErr:         errors.New("boom"),
			wantCode:           apperror.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			garages := mocks.NewMockGarageRepository(t)
			staff := mocks.NewMockGarageStaffRepository(t)
			events := mocks.NewMockEventPublisher(t)

			var publisher usecase.EventPublisher

			if tt.withEvents {
				publisher = events
			}

			uc := usecase.NewGarageUseCase(garages, staff, publisher)

			if tt.expectGarageCreate {
				garages.EXPECT().Create(mock.Anything, newGarageCreateMatcher(tt.req.AccountID, tt.req.Name)).Return(tt.garageErr).Once()
			}

			if tt.expectStaffCreate {
				staff.EXPECT().Create(mock.Anything, newOwnerStaffCreateMatcher(tt.req.AccountID)).Return(tt.staffErr).Once()
			}

			if tt.expectPublish {
				events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, newCreatedEventMatcher(tt.req.AccountID)).Return(tt.publishErr).Once()
			}

			g, err := uc.Create(context.Background(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, g)
			assert.NotEmpty(t, g.ID)
			assert.Equal(t, tt.wantTimezone, g.Timezone)
			assert.Equal(t, tt.wantCurrency, g.Currency)
		})
	}
}

// TestGarageUseCase_CreateMultipleGarages documents FR-TEN-01: one account
// can own several independent garages, each with its own owner record and
// its own event.
func TestGarageUseCase_CreateMultipleGarages(t *testing.T) {
	garages := mocks.NewMockGarageRepository(t)
	staff := mocks.NewMockGarageStaffRepository(t)
	events := mocks.NewMockEventPublisher(t)
	uc := usecase.NewGarageUseCase(garages, staff, events)

	for _, garageName := range []string{"Gara A", "Gara B"} {
		garages.EXPECT().Create(mock.Anything, mock.MatchedBy(func(g *domain.Garage) bool {
			return g.Name == garageName && g.AccountID == "acc-1"
		})).Return(nil).Once()
		staff.EXPECT().Create(mock.Anything, newOwnerStaffCreateMatcher("acc-1")).Return(nil).Once()
		events.EXPECT().Publish(mock.Anything, "garage.created", mock.Anything, mock.Anything).Return(nil).Once()
	}

	g1, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara A"})
	require.NoError(t, err)

	g2, err := uc.Create(context.Background(), usecase.CreateGarageRequest{AccountID: "acc-1", Name: "Gara B"})
	require.NoError(t, err)

	assert.NotEqual(t, g1.ID, g2.ID)
}

func TestGarageUseCase_List(t *testing.T) {
	tests := []struct {
		name      string
		accountID string
		garages   []*domain.Garage
		listErr   error
		wantCode  apperror.Code
	}{
		{
			name:      "returns garages for account",
			accountID: "acc-1",
			garages: []*domain.Garage{
				{ID: "g-1", AccountID: "acc-1", Name: "Gara A", Status: domain.GarageStatusActive},
				{ID: "g-2", AccountID: "acc-1", Name: "Gara B", Status: domain.GarageStatusActive},
			},
		},
		{name: "returns nil list when account has no garages", accountID: "acc-2"},
		{name: "fails when the garage list cannot be loaded", accountID: "acc-1", listErr: errors.New("boom"), wantCode: apperror.CodeInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockGarageRepository(t)
			uc := usecase.NewGarageUseCase(repo, nil, nil)

			repo.EXPECT().ListByAccountID(mock.Anything, tt.accountID).Return(tt.garages, tt.listErr).Once()

			result, err := uc.List(context.Background(), tt.accountID)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.garages, result)
		})
	}
}

func TestGarageUseCase_Get(t *testing.T) {
	tests := []struct {
		name     string
		garageID string
		garage   *domain.Garage
		findErr  error
		wantCode apperror.Code
	}{
		{
			name:     "returns the garage",
			garageID: "g-1",
			garage:   &domain.Garage{ID: "g-1", AccountID: "acc-1", Name: "Gara A", Status: domain.GarageStatusActive},
		},
		{name: "returns not found for a missing garage", garageID: "missing-garage", findErr: domain.ErrGarageNotFound, wantCode: apperror.CodeNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockGarageRepository(t)
			uc := usecase.NewGarageUseCase(repo, nil, nil)

			repo.EXPECT().FindByID(mock.Anything, tt.garageID).Return(tt.garage, tt.findErr).Once()

			g, err := uc.Get(context.Background(), tt.garageID)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.garage, g)
		})
	}
}

func TestGarageUseCase_Update(t *testing.T) {
	tests := []struct {
		name         string
		req          usecase.UpdateGarageRequest
		findErr      error
		expectUpdate bool
		updateErr    error
		checkUpdate  func(*domain.Garage) bool
		wantCode     apperror.Code
	}{
		{
			name: "updates every field on an existing garage",
			req: usecase.UpdateGarageRequest{
				GarageID: "g-1",
				Name:     new("Updated Gara"),
				TaxCode:  new("TAX-123"),
				Address:  new("123 Main St"),
				LogoURL:  new("https://cdn.example.com/logo.png"),
				Timezone: new("Asia/Tokyo"),
				Currency: new("JPY"),
			},
			expectUpdate: true,
			checkUpdate: func(g *domain.Garage) bool {
				return g.ID == "g-1" && g.Name == "Updated Gara" &&
					g.TaxCode != nil && *g.TaxCode == "TAX-123" &&
					g.Address != nil && *g.Address == "123 Main St" &&
					g.LogoURL != nil && *g.LogoURL == "https://cdn.example.com/logo.png" &&
					g.Timezone == "Asia/Tokyo" && g.Currency == "JPY"
			},
		},
		{
			name:         "empty request leaves the garage unchanged",
			req:          usecase.UpdateGarageRequest{GarageID: "g-1"},
			expectUpdate: true,
			checkUpdate: func(g *domain.Garage) bool {
				return g.ID == "g-1" && g.Name == "Old name" &&
					g.TaxCode == nil && g.Address == nil && g.LogoURL == nil &&
					g.Timezone == "Asia/Ho_Chi_Minh" && g.Currency == "VND"
			},
		},
		{
			name:     "returns not found when the garage is missing",
			req:      usecase.UpdateGarageRequest{GarageID: "missing-garage"},
			findErr:  domain.ErrGarageNotFound,
			wantCode: apperror.CodeNotFound,
		},
		{
			name:         "fails when the update is rejected",
			req:          usecase.UpdateGarageRequest{GarageID: "g-1", Name: new("Updated Gara")},
			expectUpdate: true,
			updateErr:    errors.New("boom"),
			checkUpdate:  func(*domain.Garage) bool { return true },
			wantCode:     apperror.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockGarageRepository(t)
			uc := usecase.NewGarageUseCase(repo, nil, nil)

			repo.EXPECT().FindByID(mock.Anything, tt.req.GarageID).Return(testGarage(), tt.findErr).Once()

			if tt.expectUpdate {
				repo.EXPECT().Update(mock.Anything, mock.MatchedBy(tt.checkUpdate)).Return(tt.updateErr).Once()
			}

			g, err := uc.Update(context.Background(), tt.req)
			if tt.wantCode != "" {
				assertAppError(t, err, tt.wantCode)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, g)
			assert.True(t, tt.checkUpdate(g))
		})
	}
}
