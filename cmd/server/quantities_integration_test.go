//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	inventoryDomain "github.com/viettechno/vietgara-backend/internal/inventory/domain"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	quoteDomain "github.com/viettechno/vietgara-backend/internal/quote/domain"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
)

// TestQuantitiesAreNumeric runs the quantity and cost columns (NUMERIC,
// migrations 000015-000017) through the real repositories: a decimal
// string goes in and comes back without padding zeros, and the stock
// balance is exact arithmetic.
func TestQuantitiesAreNumeric(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, customerID, vehicleID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'G')`, garageID, userID)
	exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customerID, garageID)
	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $3, '51F-111.11')`, vehicleID, garageID, customerID)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)

	inGarage := func(fn func(ctx context.Context)) {
		t.Helper()

		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
			fn(ctx)

			return nil
		}))
	}

	t.Run("quote and repair order lines", func(t *testing.T) {
		quotes := quoteRepository.NewPostgresRepository(pool)
		orders := repairorderRepository.NewPostgresRepository(pool)

		quote := &quoteDomain.Quote{
			ID: uuid.NewString(), GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID,
			CustomerName: "An", VehicleLicensePlate: "51F-111.11", Number: "BG-1",
			Status: quoteDomain.StatusDraft, ApprovalMode: quoteDomain.ApprovalModeLink,
			Lines: []quoteDomain.Line{
				{ID: uuid.NewString(), Position: 1, ItemType: quoteDomain.ItemTypeLabor, Description: "Labor", Quantity: "2", UnitPrice: 100, LineTotal: 200, Decision: quoteDomain.DecisionPending},
				{ID: uuid.NewString(), Position: 2, ItemType: quoteDomain.ItemTypePart, Description: "Oil", Quantity: "1.5", UnitPrice: 100, LineTotal: 150, Decision: quoteDomain.DecisionPending},
				{ID: uuid.NewString(), Position: 3, ItemType: quoteDomain.ItemTypePart, Description: "Filter", Quantity: "0.25", UnitPrice: 100, LineTotal: 25, Decision: quoteDomain.DecisionPending},
			},
			TotalAmount: 375,
		}

		order := &repairorderDomain.RepairOrder{
			ID: uuid.NewString(), GarageID: garageID, VehicleID: vehicleID, VehicleLicensePlate: "51F-111.11", CustomerName: "An",
			Number: "LSC-1", Status: repairorderDomain.StatusOpen, TotalAmount: 150,
			Lines: []repairorderDomain.Line{
				{ID: uuid.NewString(), Position: 1, ItemType: repairorderDomain.ItemTypePart, Description: "Oil", Quantity: "1.5", UnitPrice: 100, LineTotal: 150, Status: repairorderDomain.LineNotStarted},
			},
		}

		inGarage(func(ctx context.Context) {
			require.NoError(t, quotes.Create(ctx, quote))

			got, err := quotes.FindByID(ctx, garageID, quote.ID)
			require.NoError(t, err)
			require.Len(t, got.Lines, 3)
			assert.Equal(t, []string{"2", "1.5", "0.25"}, []string{got.Lines[0].Quantity, got.Lines[1].Quantity, got.Lines[2].Quantity})

			require.NoError(t, orders.Create(ctx, order))

			gotOrder, err := orders.FindByID(ctx, garageID, order.ID)
			require.NoError(t, err)
			require.Len(t, gotOrder.Lines, 1)
			assert.Equal(t, "1.5", gotOrder.Lines[0].Quantity)
		})
	})

	t.Run("stock quantities, threshold and unit cost", func(t *testing.T) {
		inventory := inventoryRepository.NewPostgresRepository(pool)
		item := &inventoryDomain.Item{ID: uuid.NewString(), GarageID: garageID, SKU: "OIL-5W30", Name: "Oil", Unit: "l", ReorderThreshold: 3}
		cost := int64(125000)

		inGarage(func(ctx context.Context) {
			require.NoError(t, inventory.CreateItem(ctx, item))

			for _, tx := range []*inventoryDomain.StockTransaction{
				{ID: uuid.NewString(), GarageID: garageID, ItemID: item.ID, Type: inventoryDomain.TransactionIn, Quantity: "10.5", UnitCost: &cost, RefType: inventoryDomain.RefPurchase},
				{ID: uuid.NewString(), GarageID: garageID, ItemID: item.ID, Type: inventoryDomain.TransactionOut, Quantity: "2.25", RefType: inventoryDomain.RefRepairOrder},
			} {
				require.NoError(t, inventory.RecordTransaction(ctx, tx))
			}

			got, err := inventory.FindItemByID(ctx, garageID, item.ID)
			require.NoError(t, err)
			assert.Equal(t, "8.25", got.QuantityOnHand)
			assert.Equal(t, 3, got.ReorderThreshold)

			transactions, total, err := inventory.ListTransactions(ctx, garageID, inventoryUsecase.TransactionFilter{ItemID: item.ID, Limit: 10})
			require.NoError(t, err)
			require.Equal(t, 2, total)

			byType := map[inventoryDomain.TransactionType]*inventoryDomain.StockTransaction{}
			for _, tx := range transactions {
				byType[tx.Type] = tx
			}

			assert.Equal(t, "10.5", byType[inventoryDomain.TransactionIn].Quantity)
			require.NotNil(t, byType[inventoryDomain.TransactionIn].UnitCost)
			assert.Equal(t, cost, *byType[inventoryDomain.TransactionIn].UnitCost)
			assert.Equal(t, "2.25", byType[inventoryDomain.TransactionOut].Quantity)
			assert.Nil(t, byType[inventoryDomain.TransactionOut].UnitCost)
		})
	})
}
