package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	"github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	"github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
)

var (
	now     = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

const (
	garageID = "g-1"
	// Valid UUIDs: the repository short-circuits malformed ids to "not found".
	unitID = "11111111-1111-1111-1111-111111111111"
	catID  = "22222222-2222-2222-2222-222222222222"
	partID = "33333333-3333-3333-3333-333333333333"
	fileID = "44444444-4444-4444-4444-444444444444"
	caseID = "55555555-5555-5555-5555-555555555555"
)

func newRepo() *repository.PostgresRepository { return repository.NewPostgresRepository(nil) }

func unitRow() []any {
	return []any{unitID, garageID, "Can", "can", true, nil, nil, now, now}
}

func categoryRow() []any {
	return []any{catID, garageID, nil, "Oil", true, nil, nil, now, now}
}

func partRow() []any {
	return []any{partID, garageID, "BEER-330", "Beer", unitID, "Can", "can", int64(15_000), catID, "Beverages", "Tiger", true, "48.000", nil, nil, now, now}
}

func TestPostgresRepository_Units(t *testing.T) {
	t.Parallel()

	unit := &domain.Unit{ID: unitID, GarageID: garageID, Name: "Can", Abbreviation: "can", IsActive: true}

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("INSERT INTO units", dbtest.Row(now, now)).
			ExpectQueryRow("INSERT INTO units", dbtest.ErrRow(dbtest.UniqueViolation("idx_units_garage_name_unique"))).
			ExpectQueryRow("INSERT INTO units", dbtest.ErrRow(dbtest.UniqueViolation("something_else"))).
			ExpectQueryRow("INSERT INTO units", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		require.NoError(t, newRepo().CreateUnit(ctx, unit))
		assert.Equal(t, now, unit.CreatedAt)
		require.ErrorIs(t, newRepo().CreateUnit(ctx, unit), domain.ErrUnitNameTaken)
		require.Error(t, newRepo().CreateUnit(ctx, unit))
		require.ErrorIs(t, newRepo().CreateUnit(ctx, unit), errBoom)
	})

	t.Run("find", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM units u", dbtest.Row(unitRow()...)).
			ExpectQueryRow("FROM units u", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM units u", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())

		got, err := newRepo().FindUnit(ctx, garageID, unitID)
		require.NoError(t, err)
		assert.Equal(t, "can", got.Abbreviation)

		_, err = newRepo().FindUnit(ctx, garageID, unitID)
		require.ErrorIs(t, err, domain.ErrUnitNotFound)

		_, err = newRepo().FindUnit(ctx, garageID, unitID)
		require.ErrorIs(t, err, errBoom)

		_, err = newRepo().FindUnit(ctx, garageID, "not-a-uuid")
		require.ErrorIs(t, err, domain.ErrUnitNotFound)
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("UPDATE units", dbtest.Row(now)).
			ExpectQueryRow("UPDATE units", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("UPDATE units", dbtest.ErrRow(dbtest.UniqueViolation("idx_units_garage_name_unique")))
		ctx := tx.Context(t.Context())

		require.NoError(t, newRepo().UpdateUnit(ctx, unit))
		require.ErrorIs(t, newRepo().UpdateUnit(ctx, unit), domain.ErrUnitNotFound)
		require.ErrorIs(t, newRepo().UpdateUnit(ctx, unit), domain.ErrUnitNameTaken)
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM units u", dbtest.NewRows(unitRow()), nil).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM units u", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM units u", dbtest.NewRows(unitRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		units, total, err := newRepo().ListUnits(ctx, garageID, usecase.UnitFilter{Limit: 20})
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		assert.Len(t, units, 1)

		units, total, err = newRepo().ListUnits(ctx, garageID, usecase.UnitFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, units)

		for range 3 {
			_, _, err = newRepo().ListUnits(ctx, garageID, usecase.UnitFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("delete, activate, usage", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectExec("UPDATE units SET deleted_at", dbtest.Tag(1), nil).
			ExpectExec("UPDATE units SET deleted_at", dbtest.Tag(0), nil).
			ExpectExec("UPDATE units SET deleted_at", dbtest.Tag(0), errBoom).
			ExpectExec("UPDATE units SET is_active", dbtest.Tag(1), nil).
			ExpectExec("UPDATE units SET is_active", dbtest.Tag(0), nil).
			ExpectExec("UPDATE units SET is_active", dbtest.Tag(0), errBoom).
			ExpectQueryRow("FROM items", dbtest.Row(true)).
			ExpectQueryRow("FROM items", dbtest.ErrRow(errBoom))
		ctx := tx.Context(t.Context())
		repo := newRepo()

		require.NoError(t, repo.SoftDeleteUnit(ctx, garageID, unitID, "actor"))
		require.ErrorIs(t, repo.SoftDeleteUnit(ctx, garageID, unitID, ""), domain.ErrUnitNotFound)
		require.ErrorIs(t, repo.SoftDeleteUnit(ctx, garageID, unitID, "actor"), errBoom)
		require.ErrorIs(t, repo.SoftDeleteUnit(ctx, garageID, "bad", "actor"), domain.ErrUnitNotFound)

		require.NoError(t, repo.SetUnitActive(ctx, garageID, unitID, false, "actor"))
		require.ErrorIs(t, repo.SetUnitActive(ctx, garageID, unitID, true, ""), domain.ErrUnitNotFound)
		require.ErrorIs(t, repo.SetUnitActive(ctx, garageID, unitID, true, "actor"), errBoom)
		require.ErrorIs(t, repo.SetUnitActive(ctx, garageID, "bad", true, "actor"), domain.ErrUnitNotFound)

		used, err := repo.UnitUsedByParts(ctx, garageID, unitID)
		require.NoError(t, err)
		assert.True(t, used)

		_, err = repo.UnitUsedByParts(ctx, garageID, unitID)
		require.ErrorIs(t, err, errBoom)
	})
}

func TestPostgresRepository_Categories(t *testing.T) {
	t.Parallel()

	parent := catID
	cat := &domain.Category{ID: catID, GarageID: garageID, ParentID: &parent, Name: "Oil", IsActive: true}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO part_categories", dbtest.Row(now, now)).
		ExpectQueryRow("INSERT INTO part_categories", dbtest.ErrRow(dbtest.UniqueViolation("idx_part_categories_sibling_name_unique"))).
		ExpectQueryRow("FROM part_categories c", dbtest.Row(categoryRow()...)).
		ExpectQueryRow("FROM part_categories c", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("FROM part_categories c", dbtest.ErrRow(errBoom)).
		ExpectQueryRow("UPDATE part_categories", dbtest.Row(now)).
		ExpectQueryRow("UPDATE part_categories", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("UPDATE part_categories", dbtest.ErrRow(dbtest.UniqueViolation("idx_part_categories_sibling_name_unique"))).
		ExpectQuery("FROM part_categories c", dbtest.NewRows(categoryRow()), nil).
		ExpectQuery("FROM part_categories c", nil, errBoom).
		ExpectQuery("FROM part_categories c", dbtest.NewRows(categoryRow()).WithScanError(errBoom), nil).
		ExpectExec("UPDATE part_categories SET deleted_at", dbtest.Tag(1), nil).
		ExpectExec("UPDATE part_categories SET is_active", dbtest.Tag(1), nil).
		ExpectQueryRow("FROM items", dbtest.Row(true)).
		ExpectQueryRow("FROM items", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())
	repo := newRepo()

	require.NoError(t, repo.CreateCategory(ctx, cat))
	require.ErrorIs(t, repo.CreateCategory(ctx, cat), domain.ErrCategoryNameTaken)

	got, err := repo.FindCategory(ctx, garageID, catID)
	require.NoError(t, err)
	assert.Equal(t, "Oil", got.Name)

	_, err = repo.FindCategory(ctx, garageID, catID)
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)

	_, err = repo.FindCategory(ctx, garageID, catID)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.FindCategory(ctx, garageID, "bad")
	require.ErrorIs(t, err, domain.ErrCategoryNotFound)

	require.NoError(t, repo.UpdateCategory(ctx, cat))
	require.ErrorIs(t, repo.UpdateCategory(ctx, cat), domain.ErrCategoryNotFound)
	require.ErrorIs(t, repo.UpdateCategory(ctx, cat), domain.ErrCategoryNameTaken)

	all, err := repo.ListCategories(ctx, garageID, true)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	_, err = repo.ListCategories(ctx, garageID, true)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.ListCategories(ctx, garageID, true)
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.SoftDeleteCategory(ctx, garageID, catID, "actor"))
	require.NoError(t, repo.SetCategoryActive(ctx, garageID, catID, false, "actor"))

	has, err := repo.CategoryHasParts(ctx, garageID, catID)
	require.NoError(t, err)
	assert.True(t, has)

	_, err = repo.CategoryHasParts(ctx, garageID, catID)
	require.ErrorIs(t, err, errBoom)
}

func packagingRow() []any {
	return []any{partID, caseID, "Carton", "ctn", "24.000000", nil}
}

func overrideRow() []any {
	return []any{partID, "66666666-6666-6666-6666-666666666666", "Half", "half", "12.500000", int64(190_000)}
}

func TestPostgresRepository_FindPart(t *testing.T) {
	t.Parallel()

	repo := newRepo()

	t.Run("with packaging and images", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM items i", dbtest.Row(partRow()...)).
			ExpectQuery("FROM item_units iu", dbtest.NewRows(packagingRow(), overrideRow()), nil).
			ExpectQuery("FROM item_images", dbtest.NewRows([]any{partID, fileID, 0}), nil)

		p, err := repo.FindPart(tx.Context(t.Context()), garageID, partID)
		require.NoError(t, err)
		assert.Equal(t, "48", p.ReorderThreshold, "NUMERIC text loses its trailing zeros")
		assert.Equal(t, catID, *p.CategoryID)
		require.Len(t, p.PackagingUnits, 2)
		assert.Equal(t, "24", p.PackagingUnits[0].Factor)
		assert.Equal(t, int64(360_000), p.PackagingUnits[0].EffectivePrice, "derived from the sale price")
		assert.Equal(t, "12.5", p.PackagingUnits[1].Factor)
		assert.Equal(t, int64(190_000), p.PackagingUnits[1].EffectivePrice, "an override wins")
		require.Len(t, p.Images, 1)
		assert.Equal(t, fileID, p.Images[0].FileID)
	})

	t.Run("not found and failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("FROM items i", dbtest.ErrRow(pgx.ErrNoRows)).
			ExpectQueryRow("FROM items i", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("FROM items i", dbtest.Row(partRow()...)).
			ExpectQuery("FROM item_units iu", nil, errBoom).
			ExpectQueryRow("FROM items i", dbtest.Row(partRow()...)).
			ExpectQuery("FROM item_units iu", dbtest.NewRows(packagingRow()).WithScanError(errBoom), nil).
			ExpectQueryRow("FROM items i", dbtest.Row(partRow()...)).
			ExpectQuery("FROM item_units iu", dbtest.NewRows(), nil).
			ExpectQuery("FROM item_images", nil, errBoom).
			ExpectQueryRow("FROM items i", dbtest.Row(partRow()...)).
			ExpectQuery("FROM item_units iu", dbtest.NewRows(), nil).
			ExpectQuery("FROM item_images", dbtest.NewRows([]any{partID, fileID, 0}).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		_, err := repo.FindPart(ctx, garageID, partID)
		require.ErrorIs(t, err, domain.ErrPartNotFound)

		for range 5 {
			_, err = repo.FindPart(ctx, garageID, partID)
			require.ErrorIs(t, err, errBoom)
		}

		_, err = repo.FindPart(ctx, garageID, "bad")
		require.ErrorIs(t, err, domain.ErrPartNotFound)
	})
}

func TestPostgresRepository_FindPartsByIDs(t *testing.T) {
	t.Parallel()

	repo := newRepo()

	got, err := repo.FindPartsByIDs(t.Context(), garageID, []string{"bad"})
	require.NoError(t, err)
	assert.Empty(t, got, "malformed ids are skipped without a query")

	tx := dbtest.New(t).
		ExpectQuery("FROM items i", dbtest.NewRows(partRow()), nil).
		ExpectQuery("FROM item_units iu", dbtest.NewRows(), nil).
		ExpectQuery("FROM item_images", dbtest.NewRows(), nil).
		ExpectQuery("FROM items i", nil, errBoom).
		ExpectQuery("FROM items i", dbtest.NewRows(partRow()).WithScanError(errBoom), nil)
	ctx := tx.Context(t.Context())

	got, err = repo.FindPartsByIDs(ctx, garageID, []string{partID, "bad"})
	require.NoError(t, err)
	assert.Len(t, got, 1)

	for range 2 {
		_, err = repo.FindPartsByIDs(ctx, garageID, []string{partID})
		require.ErrorIs(t, err, errBoom)
	}
}

func TestPostgresRepository_PartWrites(t *testing.T) {
	t.Parallel()

	cat := catID
	part := &domain.Part{
		ID: partID, GarageID: garageID, SKU: "BEER-330", Name: "Beer", BaseUnitID: unitID, SalePrice: 15_000, CategoryID: &cat,
		Brand: "Tiger", IsActive: true, ReorderThreshold: "48",
	}

	tx := dbtest.New(t).
		ExpectQueryRow("INSERT INTO items", dbtest.Row(now, now)).
		ExpectQueryRow("INSERT INTO items", dbtest.ErrRow(dbtest.UniqueViolation("idx_items_garage_sku_unique"))).
		ExpectQueryRow("UPDATE items SET sku", dbtest.Row(now)).
		ExpectQueryRow("UPDATE items SET sku", dbtest.ErrRow(pgx.ErrNoRows)).
		ExpectQueryRow("UPDATE items SET sku", dbtest.ErrRow(dbtest.UniqueViolation("idx_items_garage_sku_unique"))).
		ExpectExec("UPDATE items SET deleted_at", dbtest.Tag(1), nil).
		ExpectExec("UPDATE items SET is_active", dbtest.Tag(0), nil)
	ctx := tx.Context(t.Context())
	repo := newRepo()

	require.NoError(t, repo.CreatePart(ctx, part))
	assert.Equal(t, now, part.CreatedAt)
	require.ErrorIs(t, repo.CreatePart(ctx, part), domain.ErrSKUTaken)

	require.NoError(t, repo.UpdatePart(ctx, part))
	require.ErrorIs(t, repo.UpdatePart(ctx, part), domain.ErrPartNotFound)
	require.ErrorIs(t, repo.UpdatePart(ctx, part), domain.ErrSKUTaken)

	require.NoError(t, repo.SoftDeletePart(ctx, garageID, partID, "actor"))
	require.ErrorIs(t, repo.SetPartActive(ctx, garageID, partID, true, "actor"), domain.ErrPartNotFound)
}

func TestPostgresRepository_ListParts(t *testing.T) {
	t.Parallel()

	repo := newRepo()
	filter := usecase.PartFilter{Query: "beer", CategoryIDs: []string{catID}, Brand: "Tiger", Limit: 20}

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM items i", dbtest.NewRows(partRow()), nil).
			ExpectQuery("FROM item_units iu", dbtest.NewRows(packagingRow()), nil).
			ExpectQuery("FROM item_images", dbtest.NewRows(), nil)

		parts, total, err := repo.ListParts(tx.Context(t.Context()), garageID, filter)
		require.NoError(t, err)
		assert.Equal(t, 1, total)
		require.Len(t, parts, 1)
		assert.Len(t, parts[0].PackagingUnits, 1)
	})

	t.Run("no matches and failures", func(t *testing.T) {
		t.Parallel()

		tx := dbtest.New(t).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(0)).
			ExpectQueryRow("SELECT count(*)", dbtest.ErrRow(errBoom)).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM items i", nil, errBoom).
			ExpectQueryRow("SELECT count(*)", dbtest.Row(1)).
			ExpectQuery("FROM items i", dbtest.NewRows(partRow()).WithScanError(errBoom), nil)
		ctx := tx.Context(t.Context())

		parts, total, err := repo.ListParts(ctx, garageID, usecase.PartFilter{Limit: 20})
		require.NoError(t, err)
		assert.Zero(t, total)
		assert.Empty(t, parts)

		for range 3 {
			_, _, err = repo.ListParts(ctx, garageID, usecase.PartFilter{Limit: 20})
			require.ErrorIs(t, err, errBoom)
		}
	})
}

func TestPostgresRepository_Packaging(t *testing.T) {
	t.Parallel()

	price := int64(340_000)
	units := []domain.PackagingUnit{{UnitID: caseID, Factor: "24", SalePrice: &price}, {UnitID: unitID, Factor: "6"}}
	repo := newRepo()

	tx := dbtest.New(t).
		ExpectExec("DELETE FROM item_units", dbtest.Tag(2), nil).
		ExpectExec("INSERT INTO item_units", dbtest.Tag(1), nil).
		ExpectExec("INSERT INTO item_units", dbtest.Tag(1), nil).
		ExpectExec("UPDATE items SET updated_by", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM item_units", dbtest.Tag(0), errBoom).
		ExpectExec("DELETE FROM item_units", dbtest.Tag(0), nil).
		ExpectExec("INSERT INTO item_units", dbtest.Tag(0), errBoom)
	ctx := tx.Context(t.Context())

	require.NoError(t, repo.ReplacePackaging(ctx, garageID, partID, "actor", units))
	require.ErrorIs(t, repo.ReplacePackaging(ctx, garageID, partID, "actor", units), errBoom)
	require.ErrorIs(t, repo.ReplacePackaging(ctx, garageID, partID, "", units), errBoom)
}

func TestPostgresRepository_Images(t *testing.T) {
	t.Parallel()

	repo := newRepo()

	tx := dbtest.New(t).
		ExpectQuery("FROM item_images", dbtest.NewRows([]any{fileID, 0}, []any{"f2", 1}), nil).
		ExpectQuery("FROM item_images", nil, errBoom).
		ExpectQuery("FROM item_images", dbtest.NewRows([]any{fileID, 0}).WithScanError(errBoom), nil).
		ExpectExec("INSERT INTO item_images", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM item_images", dbtest.Tag(1), nil).
		ExpectExec("DELETE FROM item_images", dbtest.Tag(0), nil).
		ExpectExec("DELETE FROM item_images", dbtest.Tag(0), errBoom).
		ExpectQueryRow("FROM item_images", dbtest.Row(true)).
		ExpectQueryRow("FROM item_images", dbtest.ErrRow(errBoom))
	ctx := tx.Context(t.Context())

	images, err := repo.ListImages(ctx, garageID, partID)
	require.NoError(t, err)
	assert.Equal(t, []usecase.ImageRef{{FileID: fileID}, {FileID: "f2", Position: 1}}, images)

	_, err = repo.ListImages(ctx, garageID, partID)
	require.ErrorIs(t, err, errBoom)

	_, err = repo.ListImages(ctx, garageID, partID)
	require.ErrorIs(t, err, errBoom)

	require.NoError(t, repo.AddImage(ctx, garageID, partID, fileID, "actor"))

	require.NoError(t, repo.RemoveImage(ctx, garageID, partID, fileID))
	require.ErrorIs(t, repo.RemoveImage(ctx, garageID, partID, fileID), domain.ErrImageNotFound)
	require.ErrorIs(t, repo.RemoveImage(ctx, garageID, partID, fileID), errBoom)
	require.ErrorIs(t, repo.RemoveImage(ctx, garageID, "bad", fileID), domain.ErrImageNotFound)
	require.ErrorIs(t, repo.RemoveImage(ctx, garageID, partID, "bad"), domain.ErrImageNotFound)

	attached, err := repo.FileAttached(ctx, garageID, fileID)
	require.NoError(t, err)
	assert.True(t, attached)

	_, err = repo.FileAttached(ctx, garageID, fileID)
	require.ErrorIs(t, err, errBoom)

	attached, err = repo.FileAttached(ctx, garageID, "bad")
	require.NoError(t, err)
	assert.False(t, attached)
}
