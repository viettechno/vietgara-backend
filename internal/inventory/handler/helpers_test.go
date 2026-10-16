package handler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/inventory/handler"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
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
	srv    *handler.Server
	repo   *mocks.MockInventoryRepository
	parts  *mocks.MockInventoryPartCatalogue
	events *mocks.MockInventoryEventPublisher
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: mocks.NewMockInventoryRepository(t), parts: mocks.NewMockInventoryPartCatalogue(t),
		events: mocks.NewMockInventoryEventPublisher(t),
	}
	e.srv = handler.NewServer(usecase.NewInventoryUseCase(e.repo, e.parts, e.events))

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
