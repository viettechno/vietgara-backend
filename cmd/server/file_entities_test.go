package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/mocks"
)

// A part image is vouched for by the Catalogue; before it is wired, and for
// any other entity type, nothing is.
func TestFileEntities(t *testing.T) {
	t.Parallel()

	unwired := &fileEntities{}

	exists, err := unwired.EntityExists(t.Context(), "garage-1", "part", "part-1")
	require.NoError(t, err)
	assert.False(t, exists)

	repo := mocks.NewMockCatalogueRepository(t)
	repo.EXPECT().FindPart(mock.Anything, "garage-1", "part-1").Return(&catalogueDomain.Part{ID: "part-1"}, nil).Once()
	repo.EXPECT().FindPart(mock.Anything, "garage-1", "gone").Return(nil, catalogueDomain.ErrPartNotFound).Once()

	wired := &fileEntities{parts: catalogueUsecase.NewUseCase(repo, mocks.NewMockCatalogueUsageChecker(t), mocks.NewMockCatalogueFiles(t))}

	exists, err = wired.EntityExists(t.Context(), "garage-1", "part", "part-1")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = wired.EntityExists(t.Context(), "garage-1", "part", "gone")
	require.NoError(t, err)
	assert.False(t, exists)

	exists, err = wired.EntityExists(t.Context(), "garage-1", "repair_order", "ro-1")
	require.NoError(t, err)
	assert.False(t, exists)
}
