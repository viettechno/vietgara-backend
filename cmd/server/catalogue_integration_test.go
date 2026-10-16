//go:build integration

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	fileDomain "github.com/viettechno/vietgara-backend/internal/file/domain"
	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteDomain "github.com/viettechno/vietgara-backend/internal/quote/domain"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// TestCatalogueModule runs the Catalogue module, its wiring in cmd/server and
// its neighbours (stock, quote lines, files) against a real PostgreSQL: unit
// conversion, the category tree, part images, and the delete-or-deactivate and
// frozen-base-unit rules that depend on what other modules hold.
//
//nolint:funlen // one scenario through the real schema, read top to bottom
func TestCatalogueModule(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	customerID, vehicleID := uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)
	exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customerID, garageID)
	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $3, '51F-111.11')`, vehicleID, garageID, customerID)

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	entities := &fileEntities{}
	files := buildFiles(pool, store, tenantRepo.NewPostgresGarageRepository(pool), catalogueRepository.NewPostgresRepository(pool), entities)
	usage := &catalogueUsage{}
	catalogue := buildCatalogue(pool, files, usage)
	entities.parts = catalogue.uc

	inventory := inventoryRepository.NewPostgresRepository(pool)
	quotes := quoteRepository.NewPostgresRepository(pool)
	usage.inventory, usage.quotes, usage.repairOrders = inventory, quotes, repairorderRepository.NewPostgresRepository(pool)

	uc := catalogue.uc
	inv := inventoryUsecase.NewInventoryUseCase(inventory, inventoryParts{partQuantities{uc: uc}}, nil)

	inGarage := func(id string, fn func(ctx context.Context)) {
		t.Helper()

		scope := dbpkg.Scope{GarageID: id, UserID: userID}

		require.NoError(t, txm.WithTransaction(t.Context(), scope, func(ctx context.Context) error {
			fn(access.WithAccess(ctx, &access.Access{GarageID: id, UserID: userID, OwnerUserID: userID, Owner: true, Active: true}))

			return nil
		}))
	}

	// failing runs fn in its own transaction, as a request would: a statement
	// that violates a constraint aborts its transaction, so each is isolated.
	failing := func(fn func(ctx context.Context) error) error {
		t.Helper()

		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, func(ctx context.Context) error {
			return fn(access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true}))
		})
	}

	str := func(s string) *string { return &s }

	var can, carton, litre *catalogueDomain.Unit

	mkUnit := func(ctx context.Context, name, abbr string) *catalogueDomain.Unit {
		t.Helper()

		u, err := uc.CreateUnit(ctx, catalogueUsecase.CreateUnitRequest{
			GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.UnitFields{Name: str(name), Abbreviation: str(abbr)},
		})
		require.NoError(t, err)

		return u
	}

	t.Run("units and categories", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			can, carton, litre = mkUnit(ctx, "Can", "can"), mkUnit(ctx, "Carton", "ctn"), mkUnit(ctx, "Litre", "l")

			page, err := uc.ListUnits(ctx, catalogueUsecase.ListUnitsRequest{GarageID: garageID})
			require.NoError(t, err)
			assert.Equal(t, 3, page.Total)

			root, err := uc.CreateCategory(ctx, catalogueUsecase.CreateCategoryRequest{
				GarageID: garageID, Fields: catalogueUsecase.CategoryFields{Name: str("Oil"), SetParent: true},
			})
			require.NoError(t, err)

			mid, err := uc.CreateCategory(ctx, catalogueUsecase.CreateCategoryRequest{
				GarageID: garageID, Fields: catalogueUsecase.CategoryFields{Name: str("Synthetic"), SetParent: true, ParentID: root.ID},
			})
			require.NoError(t, err)

			leaf, err := uc.CreateCategory(ctx, catalogueUsecase.CreateCategoryRequest{
				GarageID: garageID, Fields: catalogueUsecase.CategoryFields{Name: str("5W-30"), SetParent: true, ParentID: mid.ID},
			})
			require.NoError(t, err)

			_, err = uc.CreateCategory(ctx, catalogueUsecase.CreateCategoryRequest{
				GarageID: garageID, Fields: catalogueUsecase.CategoryFields{Name: str("Too deep"), SetParent: true, ParentID: leaf.ID},
			})
			require.ErrorContains(t, err, "three levels")

			_, err = uc.UpdateCategory(ctx, catalogueUsecase.UpdateCategoryRequest{
				GarageID: garageID, CategoryID: root.ID, Fields: catalogueUsecase.CategoryFields{SetParent: true, ParentID: leaf.ID},
			})
			require.Error(t, err, "a category cannot move under its own descendant")

			all, err := uc.ListCategories(ctx, garageID, false)
			require.NoError(t, err)
			assert.Len(t, all, 3)

			// Deactivating and reactivating is saved: is_active is part of the update.
			_, err = uc.UpdateCategory(ctx, catalogueUsecase.UpdateCategoryRequest{
				GarageID: garageID, CategoryID: leaf.ID, Fields: catalogueUsecase.CategoryFields{IsActive: new(false)},
			})
			require.NoError(t, err)

			active, err := uc.ListCategories(ctx, garageID, false)
			require.NoError(t, err)
			assert.Len(t, active, 2)

			_, err = uc.UpdateCategory(ctx, catalogueUsecase.UpdateCategoryRequest{
				GarageID: garageID, CategoryID: leaf.ID, Fields: catalogueUsecase.CategoryFields{IsActive: new(true)},
			})
			require.NoError(t, err)

			_, err = uc.UpdateUnit(ctx, catalogueUsecase.UpdateUnitRequest{
				GarageID: garageID, UnitID: litre.ID, Fields: catalogueUsecase.UnitFields{IsActive: new(false)},
			})
			require.NoError(t, err)

			units, err := uc.ListUnits(ctx, catalogueUsecase.ListUnitsRequest{GarageID: garageID})
			require.NoError(t, err)
			assert.Equal(t, 2, units.Total)

			_, err = uc.UpdateUnit(ctx, catalogueUsecase.UpdateUnitRequest{
				GarageID: garageID, UnitID: litre.ID, Fields: catalogueUsecase.UnitFields{IsActive: new(true)},
			})
			require.NoError(t, err)
		})
	})

	t.Run("duplicate unit name", func(t *testing.T) {
		err := failing(func(ctx context.Context) error {
			_, err := uc.CreateUnit(ctx, catalogueUsecase.CreateUnitRequest{
				GarageID: garageID, Fields: catalogueUsecase.UnitFields{Name: str("CAN"), Abbreviation: str("c")},
			})

			return err
		})
		require.ErrorContains(t, err, "already has this name")
	})

	var beer *catalogueDomain.Part

	t.Run("part, packaging and conversion", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			cats, err := uc.ListCategories(ctx, garageID, false)
			require.NoError(t, err)

			var leafID string

			for _, c := range cats {
				if c.Name == "5W-30" {
					leafID = c.ID
				}
			}

			beer, err = uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.PartFields{
					SKU: str("BEER-330"), Name: str("Beer 330 ml"), BaseUnitID: &can.ID, SalePrice: new(int64(15_000)),
					Brand: str("Tiger"), CategoryID: &leafID, ReorderThreshold: str("48"),
				},
			})
			require.NoError(t, err)
			assert.Equal(t, "can", beer.BaseUnitAbbreviation)
			assert.Equal(t, "5W-30", beer.CategoryName)
			assert.Equal(t, "48", beer.ReorderThreshold)

			beer, err = uc.SetPartUnits(ctx, garageID, beer.ID, userID, []catalogueDomain.PackagingUnit{
				{UnitID: carton.ID, Factor: "24", SalePrice: new(int64(340_000))},
			})
			require.NoError(t, err)
			require.Len(t, beer.PackagingUnits, 1)
			assert.Equal(t, "24", beer.PackagingUnits[0].Factor)
			assert.Equal(t, int64(340_000), beer.PackagingUnits[0].EffectivePrice, "the override wins")

			beer, err = uc.SetPartUnits(ctx, garageID, beer.ID, userID, []catalogueDomain.PackagingUnit{{UnitID: carton.ID, Factor: "24"}})
			require.NoError(t, err)
			assert.Equal(t, int64(360_000), beer.PackagingUnits[0].EffectivePrice, "derived: 15,000 x 24")

			_, err = uc.SetPartUnits(ctx, garageID, beer.ID, userID, []catalogueDomain.PackagingUnit{{UnitID: can.ID, Factor: "1"}})
			require.ErrorContains(t, err, "base unit cannot also be a packaging unit")

			q, err := uc.ResolveQuantity(ctx, garageID, beer.ID, carton.ID, "10")
			require.NoError(t, err)
			assert.Equal(t, "240", q.Base)
			assert.Equal(t, carton.ID, *q.UnitID)
			assert.Equal(t, "10", *q.Entered)

			q, err = uc.ResolveQuantity(ctx, garageID, beer.ID, "", "7")
			require.NoError(t, err)
			assert.Equal(t, "7", q.Base)
			assert.Nil(t, q.UnitID)

			_, err = uc.ResolveQuantity(ctx, garageID, beer.ID, litre.ID, "1")
			require.ErrorContains(t, err, "not a packaging unit")

			_, err = uc.ResolveQuantity(ctx, garageID, beer.ID, carton.ID, "0.0001")
			require.Error(t, err, "more than three decimals is refused, never rounded")

			found, err := uc.ListParts(ctx, catalogueUsecase.ListPartsRequest{GarageID: garageID, Query: "tiger"})
			require.NoError(t, err)
			require.Len(t, found.Items, 1)

			cats, err = uc.ListCategories(ctx, garageID, false)
			require.NoError(t, err)

			var rootID string

			for _, c := range cats {
				if c.Name == "Oil" {
					rootID = c.ID
				}
			}

			byRoot, err := uc.ListParts(ctx, catalogueUsecase.ListPartsRequest{GarageID: garageID, CategoryID: rootID})
			require.NoError(t, err)
			assert.Len(t, byRoot.Items, 1, "a category filter includes the sub-categories")
		})

		// Another garage sees nothing.
		inGarage(otherGarageID, func(ctx context.Context) {
			_, err := uc.GetPart(ctx, otherGarageID, beer.ID)
			require.ErrorIs(t, err, catalogueDomain.ErrPartNotFound)
		})
	})

	t.Run("duplicate SKU", func(t *testing.T) {
		err := failing(func(ctx context.Context) error {
			_, err := uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, Fields: catalogueUsecase.PartFields{SKU: str("BEER-330"), Name: str("Dup"), BaseUnitID: &can.ID},
			})

			return err
		})
		require.ErrorContains(t, err, "another part already uses this SKU")
	})

	t.Run("stock receipt in a packaging unit, cost per base unit", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			result, err := inv.CreateStockReceipt(ctx, inventoryUsecase.CreateStockReceiptRequest{
				GarageID: garageID, ActorID: userID, PartID: beer.ID, Quantity: "10", UnitID: carton.ID, UnitCost: 240_000,
			})
			require.NoError(t, err)
			assert.Equal(t, "240", result.Transaction.Quantity)
			assert.Equal(t, "ctn", result.Transaction.UnitAbbreviation)
			assert.Equal(t, "10000", *result.Transaction.UnitCost, "240,000 per carton of 24 is 10,000 per can")
			assert.Equal(t, "240", result.StockLevel.QuantityOnHand)

			low, err := inv.ListStockLevels(ctx, inventoryUsecase.ListStockLevelsRequest{GarageID: garageID, LowStock: true})
			require.NoError(t, err)
			assert.Empty(t, low.Items, "240 cans is above the threshold of 48")

			page, err := inv.ListTransactions(ctx, inventoryUsecase.ListTransactionsRequest{GarageID: garageID, PartID: beer.ID})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, "10", *page.Items[0].EnteredQuantity)
			assert.Equal(t, "BEER-330", page.Items[0].PartSKU)
		})
	})

	t.Run("a base unit with stock is frozen; the unit and part are deactivated, not deleted", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			_, err := uc.UpdatePart(ctx, catalogueUsecase.UpdatePartRequest{
				GarageID: garageID, PartID: beer.ID, Fields: catalogueUsecase.PartFields{BaseUnitID: &litre.ID},
			})
			require.ErrorContains(t, err, "base unit cannot change")

			outcome, err := uc.DeleteUnit(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: carton.ID, ActorID: userID, DryRun: true})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deactivated, outcome, "a part's packaging uses it")

			outcome, err = uc.DeletePart(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: beer.ID, ActorID: userID})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deactivated, outcome, "stock movements refer to it")

			p, err := uc.GetPart(ctx, garageID, beer.ID)
			require.NoError(t, err)
			assert.False(t, p.IsActive)

			_, err = inv.CreateStockReceipt(ctx, inventoryUsecase.CreateStockReceiptRequest{
				GarageID: garageID, PartID: beer.ID, Quantity: "1",
			})
			require.ErrorContains(t, err, "not active")

			p, err = uc.ActivatePart(ctx, garageID, beer.ID, userID)
			require.NoError(t, err)
			assert.True(t, p.IsActive)
		})
	})

	t.Run("an unused part is deleted and frees its SKU", func(t *testing.T) {
		inGarage(garageID, func(ctx context.Context) {
			p, err := uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, Fields: catalogueUsecase.PartFields{SKU: str("TMP"), Name: str("Temp"), BaseUnitID: &litre.ID},
			})
			require.NoError(t, err)

			// The base unit may change while nothing uses the part.
			p, err = uc.UpdatePart(ctx, catalogueUsecase.UpdatePartRequest{
				GarageID: garageID, PartID: p.ID, Fields: catalogueUsecase.PartFields{BaseUnitID: &can.ID},
			})
			require.NoError(t, err)
			assert.Equal(t, can.ID, p.BaseUnitID)

			outcome, err := uc.DeletePart(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: p.ID, ActorID: userID})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deleted, outcome)

			_, err = uc.GetPart(ctx, garageID, p.ID)
			require.ErrorIs(t, err, catalogueDomain.ErrPartNotFound)

			_, err = uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, Fields: catalogueUsecase.PartFields{SKU: str("TMP"), Name: str("Temp again"), BaseUnitID: &litre.ID},
			})
			require.NoError(t, err)
		})
	})

	t.Run("quote line keeps the unit; the unit then stays", func(t *testing.T) {
		unitID, entered := carton.ID, "2"
		quote := &quoteDomain.Quote{
			ID: uuid.NewString(), GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID,
			CustomerName: "An", VehicleLicensePlate: "51F-111.11", Number: "BG-9",
			Status: quoteDomain.StatusDraft, ApprovalMode: quoteDomain.ApprovalModeLink,
			Lines: []quoteDomain.Line{{
				ID: uuid.NewString(), Position: 1, ItemType: quoteDomain.ItemTypePart, ItemRefID: &beer.ID, Description: "Beer",
				Quantity: "48", UnitID: &unitID, EnteredQuantity: &entered, UnitPrice: 15_000, LineTotal: 720_000,
				Decision: quoteDomain.DecisionPending,
			}},
			TotalAmount: 720_000,
		}

		inGarage(garageID, func(ctx context.Context) {
			require.NoError(t, quotes.Create(ctx, quote))

			got, err := quotes.FindByID(ctx, garageID, quote.ID)
			require.NoError(t, err)
			require.Len(t, got.Lines, 1)
			assert.Equal(t, "48", got.Lines[0].Quantity)
			assert.Equal(t, "2", *got.Lines[0].EnteredQuantity)
			assert.Equal(t, "ctn", got.Lines[0].UnitAbbreviation)

			used, err := usage.UnitInUse(ctx, garageID, carton.ID)
			require.NoError(t, err)
			assert.True(t, used)

			used, err = usage.PartInUse(ctx, garageID, beer.ID)
			require.NoError(t, err)
			assert.True(t, used)

			fresh := mkUnit(ctx, "Fresh", "f")
			used, err = usage.UnitInUse(ctx, garageID, fresh.ID)
			require.NoError(t, err)
			assert.False(t, used)
		})
	})

	t.Run("part images: upload, attach, limit, remove", func(t *testing.T) {
		png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 32)...)

		inGarage(garageID, func(ctx context.Context) {
			upload := func(partID string) *fileUsecase.Result {
				t.Helper()

				r, err := files.uc.Upload(ctx, fileUsecase.UploadRequest{
					GarageID: garageID, ActorID: userID, Kind: fileDomain.KindPartImage, EntityType: "part", EntityID: partID,
					Content: png, ContentType: "image/png",
				})
				require.NoError(t, err)

				return r
			}

			_, err := files.uc.Upload(ctx, fileUsecase.UploadRequest{
				GarageID: garageID, ActorID: userID, Kind: fileDomain.KindPartImage, EntityType: "part", EntityID: uuid.NewString(),
				Content: png, ContentType: "image/png",
			})
			require.ErrorContains(t, err, "does not belong", "the part must exist in the garage")

			var ids []string

			for range 3 {
				up := upload(beer.ID)
				ids = append(ids, up.File.ID)

				p, err := uc.AddPartImage(ctx, garageID, beer.ID, up.File.ID, userID)
				require.NoError(t, err)
				assert.Len(t, p.Images, len(ids))
				assert.Contains(t, p.Images[len(ids)-1].URL, up.File.Key)
			}

			extra := upload(beer.ID)
			_, err = uc.AddPartImage(ctx, garageID, beer.ID, extra.File.ID, userID)
			require.ErrorContains(t, err, "at most three images")

			_, err = uc.AddPartImage(ctx, garageID, beer.ID, ids[0], userID)
			require.Error(t, err, "an image already attached cannot be attached again")

			elsewhere := upload(beer.ID)
			other, err := uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, Fields: catalogueUsecase.PartFields{SKU: str("OTHER"), Name: str("Other"), BaseUnitID: &can.ID},
			})
			require.NoError(t, err)

			_, err = uc.AddPartImage(ctx, garageID, other.ID, elsewhere.File.ID, userID)
			require.ErrorContains(t, err, "not an image", "an image uploaded for another part is refused")

			inUse, err := fileUsage{parts: catalogueRepository.NewPostgresRepository(pool)}.FileInUse(ctx, garageID, &fileDomain.File{ID: ids[0], Kind: fileDomain.KindPartImage})
			require.NoError(t, err)
			assert.True(t, inUse)

			p, err := uc.RemovePartImage(ctx, garageID, beer.ID, ids[0], userID)
			require.NoError(t, err)
			assert.Len(t, p.Images, 2)

			_, err = uc.RemovePartImage(ctx, garageID, beer.ID, ids[0], userID)
			require.ErrorIs(t, err, catalogueDomain.ErrImageNotFound)
		})
	})
}
