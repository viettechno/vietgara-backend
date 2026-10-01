package handler_test

import (
	"context"
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
	itemID   = "11111111-1111-1111-1111-111111111111"
	actorID  = "22222222-2222-2222-2222-222222222222"
)

var anyArg = mock.Anything

type env struct {
	srv    *handler.Server
	repo   *mocks.MockInventoryRepository
	events *mocks.MockInventoryEventPublisher
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{repo: mocks.NewMockInventoryRepository(t), events: mocks.NewMockInventoryEventPublisher(t)}
	e.srv = handler.NewServer(usecase.NewInventoryUseCase(e.repo, e.events))

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}
