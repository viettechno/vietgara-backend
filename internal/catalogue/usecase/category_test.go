package usecase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// tree: Oil (root) > Synthetic > 5W-30; Tyres (root); Old (inactive root).
func tree() []*domain.Category {
	oil, synth, leaf, tyres := "oil", "synth", "leaf", "tyres"

	return []*domain.Category{
		{ID: oil, GarageID: garageID, Name: "Oil", IsActive: true},
		{ID: synth, GarageID: garageID, ParentID: &oil, Name: "Synthetic", IsActive: true},
		{ID: leaf, GarageID: garageID, ParentID: &synth, Name: "5W-30", IsActive: true},
		{ID: tyres, GarageID: garageID, Name: "Tyres", IsActive: true},
		{ID: "old", GarageID: garageID, Name: "Old", IsActive: false},
	}
}

func TestUseCase_CreateCategory(t *testing.T) {
	t.Parallel()

	create := func(t *testing.T, fields usecase.CategoryFields) (*domain.Category, error) {
		t.Helper()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Maybe()
		d.Repo.EXPECT().CreateCategory(mock.Anything, mock.Anything).Return(nil).Maybe()

		return uc.CreateCategory(t.Context(), usecase.CreateCategoryRequest{GarageID: garageID, ActorID: actorID, Fields: fields})
	}

	t.Run("root and nested", func(t *testing.T) {
		t.Parallel()

		c, err := create(t, usecase.CategoryFields{Name: new(" Filters "), SetParent: true})
		require.NoError(t, err)
		assert.Equal(t, "Filters", c.Name)
		assert.Nil(t, c.ParentID)
		assert.True(t, c.IsActive)

		c, err = create(t, usecase.CategoryFields{Name: new("Brake"), SetParent: true, ParentID: "tyres"})
		require.NoError(t, err)
		assert.Equal(t, "tyres", *c.ParentID)

		c, err = create(t, usecase.CategoryFields{Name: new("No parent field")})
		require.NoError(t, err)
		assert.Nil(t, c.ParentID)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			fields usecase.CategoryFields
			code   apperror.Code
		}{
			"no name":         {usecase.CategoryFields{}, apperror.CodeInvalidArgument},
			"blank name":      {usecase.CategoryFields{Name: new(" ")}, apperror.CodeInvalidArgument},
			"name too long":   {usecase.CategoryFields{Name: new(strings.Repeat("x", 256))}, apperror.CodeInvalidArgument},
			"fourth level":    {usecase.CategoryFields{Name: new("deep"), SetParent: true, ParentID: "leaf"}, apperror.CodeInvalidArgument},
			"unknown parent":  {usecase.CategoryFields{Name: new("x"), SetParent: true, ParentID: "nope"}, apperror.CodeNotFound},
			"inactive parent": {usecase.CategoryFields{Name: new("x"), SetParent: true, ParentID: "old"}, apperror.CodeInvalidArgument},
		} {
			_, err := create(t, tc.fields)
			assertAppError(t, err, tc.code)

			_ = name
		}
	})

	t.Run("storage failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(nil, errBoom).Once()

		_, err := uc.CreateCategory(t.Context(), usecase.CreateCategoryRequest{GarageID: garageID, Fields: usecase.CategoryFields{Name: new("x")}})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().CreateCategory(mock.Anything, mock.Anything).Return(domain.ErrCategoryNameTaken).Once()

		_, err = uc.CreateCategory(t.Context(), usecase.CreateCategoryRequest{GarageID: garageID, Fields: usecase.CategoryFields{Name: new("x")}})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestUseCase_UpdateCategory(t *testing.T) {
	t.Parallel()

	find := func(id string) *domain.Category {
		for _, c := range tree() {
			if c.ID == id {
				return c
			}
		}

		return nil
	}

	update := func(t *testing.T, id string, fields usecase.CategoryFields) (*domain.Category, error) {
		t.Helper()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, id).Return(find(id), nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().UpdateCategory(mock.Anything, mock.Anything).Return(nil).Maybe()

		return uc.UpdateCategory(t.Context(), usecase.UpdateCategoryRequest{GarageID: garageID, CategoryID: id, ActorID: actorID, Fields: fields})
	}

	t.Run("rename and deactivate", func(t *testing.T) {
		t.Parallel()

		c, err := update(t, "tyres", usecase.CategoryFields{Name: new("Tires"), IsActive: new(false)})
		require.NoError(t, err)
		assert.Equal(t, "Tires", c.Name)
		assert.False(t, c.IsActive)
	})

	t.Run("moves", func(t *testing.T) {
		t.Parallel()

		// A leaf can go to the root, or under a root.
		c, err := update(t, "leaf", usecase.CategoryFields{SetParent: true})
		require.NoError(t, err)
		assert.Nil(t, c.ParentID)

		c, err = update(t, "leaf", usecase.CategoryFields{SetParent: true, ParentID: "tyres"})
		require.NoError(t, err)
		assert.Equal(t, "tyres", *c.ParentID)

		// Oil has a subtree of height 2: it fits only at the root.
		_, err = update(t, "oil", usecase.CategoryFields{SetParent: true, ParentID: "tyres"})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		// Never under itself or its own descendants.
		_, err = update(t, "oil", usecase.CategoryFields{SetParent: true, ParentID: "oil"})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = update(t, "oil", usecase.CategoryFields{SetParent: true, ParentID: "leaf"})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		_, err = update(t, "oil", usecase.CategoryFields{SetParent: true, ParentID: "synth"})
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "x").Return(nil, domain.ErrCategoryNotFound).Once()

		_, err := uc.UpdateCategory(t.Context(), usecase.UpdateCategoryRequest{GarageID: garageID, CategoryID: "x"})
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "tyres").Return(tree()[3], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(nil, errBoom).Once()

		_, err = uc.UpdateCategory(t.Context(), usecase.UpdateCategoryRequest{GarageID: garageID, CategoryID: "tyres"})
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "tyres").Return(tree()[3], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().UpdateCategory(mock.Anything, mock.Anything).Return(domain.ErrCategoryNameTaken).Once()

		_, err = uc.UpdateCategory(t.Context(), usecase.UpdateCategoryRequest{GarageID: garageID, CategoryID: "tyres"})
		assertAppError(t, err, apperror.CodeAlreadyExists)
	})
}

func TestUseCase_ListCategories(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().ListCategories(mock.Anything, garageID, false).Return(tree(), nil).Once()

	got, err := uc.ListCategories(t.Context(), garageID, false)
	require.NoError(t, err)
	assert.Len(t, got, 5)

	d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(nil, errBoom).Once()

	_, err = uc.ListCategories(t.Context(), garageID, true)
	assertAppError(t, err, apperror.CodeInternal)
}

func TestUseCase_DeleteCategory(t *testing.T) {
	t.Parallel()

	req := func(id string) usecase.DeleteRequest {
		return usecase.DeleteRequest{GarageID: garageID, ID: id, ActorID: actorID}
	}

	t.Run("an empty leaf is deleted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "tyres").Return(tree()[3], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().CategoryHasParts(mock.Anything, garageID, "tyres").Return(false, nil).Once()
		d.Repo.EXPECT().SoftDeleteCategory(mock.Anything, garageID, "tyres", actorID).Return(nil).Once()

		outcome, err := uc.DeleteCategory(t.Context(), req("tyres"))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deleted, outcome)
	})

	t.Run("a category with sub-categories is deactivated", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "oil").Return(tree()[0], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().SetCategoryActive(mock.Anything, garageID, "oil", false, actorID).Return(nil).Once()

		outcome, err := uc.DeleteCategory(t.Context(), req("oil"))
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("a category with parts is deactivated", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "leaf").Return(tree()[2], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(tree(), nil).Once()
		d.Repo.EXPECT().CategoryHasParts(mock.Anything, garageID, "leaf").Return(true, nil).Once()

		dry := req("leaf")
		dry.DryRun = true

		outcome, err := uc.DeleteCategory(t.Context(), dry)
		require.NoError(t, err)
		assert.Equal(t, lifecycle.Deactivated, outcome)
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "x").Return(nil, domain.ErrCategoryNotFound).Once()

		_, err := uc.DeleteCategory(t.Context(), req("x"))
		assertAppError(t, err, apperror.CodeNotFound)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindCategory(mock.Anything, garageID, "tyres").Return(tree()[3], nil).Once()
		d.Repo.EXPECT().ListCategories(mock.Anything, garageID, true).Return(nil, errBoom).Once()

		_, err = uc.DeleteCategory(t.Context(), req("tyres"))
		assertAppError(t, err, apperror.CodeInternal)
	})
}
