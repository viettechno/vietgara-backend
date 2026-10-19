package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/inventory/handler"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
)

const (
	garageID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	partID   = "11111111-1111-1111-1111-111111111111"
	unitID   = "33333333-3333-3333-3333-333333333333"
	actorID  = "22222222-2222-2222-2222-222222222222"
)

var (
	anyArg  = mock.Anything
	errBoom = errors.New("boom")
)

type env struct {
	srv       *handler.Server
	repo      *mocks.MockInventoryRepository
	parts     *mocks.MockInventoryPartCatalogue
	events    *mocks.MockInventoryEventPublisher
	garages   *mocks.MockInventoryGarages
	directory *mocks.MockInventoryDirectory
	sequences *mocks.MockInventorySequencer
	audit     *mocks.MockInventoryAuditLogger
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: mocks.NewMockInventoryRepository(t), parts: mocks.NewMockInventoryPartCatalogue(t),
		events: mocks.NewMockInventoryEventPublisher(t), garages: mocks.NewMockInventoryGarages(t),
		directory: mocks.NewMockInventoryDirectory(t), sequences: mocks.NewMockInventorySequencer(t),
		audit: mocks.NewMockInventoryAuditLogger(t),
	}
	e.repo.EXPECT().DraftLines(mock.Anything, mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	e.srv = handler.NewServer(usecase.NewInventoryUseCase(usecase.Dependencies{
		Repo: e.repo, Parts: e.parts, Events: e.events, Garages: e.garages, Directory: e.directory, Sequences: e.sequences,
		Audit: e.audit, Now: func() time.Time { return time.Date(2026, time.October, 15, 3, 0, 0, 0, time.UTC) },
	}))

	return e
}

func samplePart() usecase.PartInfo {
	return usecase.PartInfo{
		ID: partID, SKU: "BEER-330", Name: "Beer 330 ml", BaseUnitName: "Can", BaseUnitAbbreviation: "can",
		CategoryName: "Beverages", Brand: "Tiger", IsActive: true, ReorderThreshold: "5", ThumbnailURL: "https://cdn/x.png",
	}
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}

// signedInWithCost is a caller who holds inventory.cost.read.
func signedInWithCost(t *testing.T) context.Context {
	t.Helper()

	return access.WithAccess(signedIn(t), &access.Access{Permissions: []access.Permission{access.InventoryRead, access.InventoryCostRead}})
}
