//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	inventoryDomain "github.com/viettechno/vietgara-backend/internal/inventory/domain"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	supplierRepository "github.com/viettechno/vietgara-backend/internal/supplier/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// TestStockAdjustments runs Release 7.1 -- stock counts, write-offs, returns to
// supplier and returns of repair order parts -- through the real use case,
// repositories and schema.
//
//nolint:funlen // one scenario through the real schema, read top to bottom
func TestStockAdjustments(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID, otherGarageID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	unitID, carton := uuid.NewString(), uuid.NewString()
	categoryID, supplierID := uuid.NewString(), uuid.NewString()
	partA, partB, partC := uuid.NewString(), uuid.NewString(), uuid.NewString()
	orderID, orderLineID := uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner Name')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, garageID, otherGarageID, userID)
	exec(`INSERT INTO units (id, garage_id, name, abbreviation) VALUES ($1, $3, 'Can', 'can'), ($2, $3, 'Carton', 'ctn')`, unitID, carton, garageID)
	exec(`INSERT INTO part_categories (id, garage_id, name) VALUES ($1, $2, 'Oils')`, categoryID, garageID)
	exec(`INSERT INTO parts (id, garage_id, sku, name, base_unit_id, category_id) VALUES ($1, $4, 'A', 'Part A', $5, $6), ($2, $4, 'B', 'Part B', $5, $6), ($3, $4, 'C', 'Part C', $5, NULL)`,
		partA, partB, partC, garageID, unitID, categoryID)
	exec(`INSERT INTO part_units (garage_id, part_id, unit_id, factor) VALUES ($1, $2, $3, 12)`, garageID, partA, carton)
	exec(`INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $2, 'Supplier A')`, supplierID, garageID)

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	entities := &fileEntities{}
	files := buildFiles(pool, store, tenantRepo.NewPostgresGarageRepository(pool), catalogueRepository.NewPostgresRepository(pool), entities)
	catalogue := buildCatalogue(pool, files, &catalogueUsage{})

	module := buildInventory(pool, outbox.NewEventPublisher(pool), inventoryParts{partQuantities{uc: catalogue.uc}},
		inventoryGarages{garages: tenantRepo.NewPostgresGarageRepository(pool)},
		inventoryDirectory{suppliers: supplierRepository.NewPostgresRepository(pool), users: identityRepo.NewPostgresUserRepository(pool)},
		audit.NewLogger(pool))
	inv := module.uc

	// in runs fn in a request-like transaction of the garage; failing ones are
	// isolated because a failed statement aborts its transaction.
	in := func(fn func(ctx context.Context) error) error {
		t.Helper()

		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, fn)
	}

	must := func(fn func(ctx context.Context) error) {
		t.Helper()
		require.NoError(t, in(fn))
	}

	level := func(partID string) *inventoryDomain.StockLevel {
		t.Helper()

		var got *inventoryDomain.StockLevel

		must(func(ctx context.Context) error {
			var err error

			got, err = inv.GetStockLevel(ctx, garageID, partID)

			return err
		})

		return got
	}

	reason := func(err error) string {
		t.Helper()

		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected an app error, got %v", err)

		return appErr.Reason
	}

	// Stock: A 10 @ 100, B 20 @ 50, C 5 @ 200, all from one goods receipt.
	var receipt *inventoryDomain.GoodsReceipt

	must(func(ctx context.Context) error {
		r, err := inv.CreateGoodsReceipt(ctx, inventoryUsecase.CreateGoodsReceiptRequest{
			GarageID: garageID, ActorID: userID, CanSetCost: true, DefaultSupplierID: supplierID,
			Lines: []inventoryUsecase.ReceiptLineInput{
				{PartID: partA, EnteredQuantity: "10", UnitCost: 100},
				{PartID: partB, EnteredQuantity: "20", UnitCost: 50},
				{PartID: partC, EnteredQuantity: "5", UnitCost: 200},
			},
		})
		if err != nil {
			return err
		}

		receipt, err = inv.CompleteGoodsReceipt(ctx, garageID, r.ID, userID)

		return err
	})

	t.Run("a stock count of 3 parts with 2 differences", func(t *testing.T) {
		var count *inventoryDomain.StockCount

		must(func(ctx context.Context) error {
			var err error

			count, err = inv.CreateStockCount(ctx, inventoryUsecase.CreateStockCountRequest{GarageID: garageID, ActorID: userID, Name: "Shelf A"})

			return err
		})
		assert.Equal(t, inventoryDomain.StockCountDraft, count.Status)
		assert.Equal(t, "Owner Name", count.CreatedByName)
		assert.Regexp(t, `^KK-\d{6}-0001$`, count.Number)

		// Lines by category (A and B, the part of the category with stock) and by pick (C); a repeat adds nothing.
		must(func(ctx context.Context) error {
			var err error

			count, err = inv.AddStockCountLines(ctx, inventoryUsecase.AddStockCountLinesRequest{
				GarageID: garageID, CountID: count.ID, ActorID: userID, CategoryID: categoryID, PartIDs: []string{partC},
			})
			if err != nil {
				return err
			}

			count, err = inv.AddStockCountLines(ctx, inventoryUsecase.AddStockCountLinesRequest{
				GarageID: garageID, CountID: count.ID, ActorID: userID, PartIDs: []string{partA},
			})

			return err
		})
		require.Len(t, count.Lines, 3)
		assert.Equal(t, 3, count.Tally.Uncounted)

		lineOf := func(partID string) string {
			for _, l := range count.Lines {
				if l.PartID == partID {
					return l.ID
				}
			}

			return ""
		}

		typed := func(partID, unit, quantity string) {
			t.Helper()

			must(func(ctx context.Context) error {
				var err error

				count, err = inv.UpdateStockCountLine(ctx, inventoryUsecase.UpdateStockCountLineRequest{
					GarageID: garageID, CountID: count.ID, LineID: lineOf(partID), ActorID: userID, UnitID: unit, EnteredCounted: quantity,
				})

				return err
			})
		}

		// Completing with an uncounted line is refused.
		err := in(func(ctx context.Context) error {
			_, err := inv.CompleteStockCount(ctx, inventoryUsecase.CompleteStockCountRequest{GarageID: garageID, CountID: count.ID, ActorID: userID})

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonStockCountHasUncountedLines, reason(err))

		typed(partA, "", "10")
		typed(partB, "", "18")
		typed(partC, "", "7")

		assert.Equal(t, inventoryDomain.Tally{Matched: 1, Over: 1, Short: 1}, count.Tally)
		assert.EqualValues(t, 300, count.TotalDifferenceValue, "-2 x 50 + 2 x 200 at average cost")

		// A count typed in a packaging unit is converted, and zero is a count.
		typed(partA, carton, "1")
		require.NotNil(t, count.Lines[count.LineByPart(partA)].Counted)
		assert.Equal(t, "12", *count.Lines[count.LineByPart(partA)].Counted)
		typed(partA, "", "10")

		must(func(ctx context.Context) error {
			var err error

			count, err = inv.CompleteStockCount(ctx, inventoryUsecase.CompleteStockCountRequest{GarageID: garageID, CountID: count.ID, ActorID: userID})

			return err
		})
		assert.Equal(t, inventoryDomain.StockCountCompleted, count.Status)
		assert.Equal(t, "Owner Name", count.CompletedByName)
		assert.EqualValues(t, 300, count.TotalDifferenceValue)

		assert.Equal(t, "10", level(partA).QuantityOnHand)
		assert.Equal(t, "18", level(partB).QuantityOnHand, "stock equals the count")
		assert.Equal(t, "7", level(partC).QuantityOnHand)
		assert.Equal(t, "200", *level(partC).AverageCost, "an adjustment never moves the average cost")

		// Completed counts cannot be edited.
		err = in(func(ctx context.Context) error {
			_, err := inv.UpdateStockCountLine(ctx, inventoryUsecase.UpdateStockCountLineRequest{
				GarageID: garageID, CountID: count.ID, LineID: lineOf(partA), ActorID: userID, EnteredCounted: "1",
			})

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonStockCountNotDraft, reason(err))

		// The part's stock card shows the adjustment with its document and user.
		must(func(ctx context.Context) error {
			page, err := inv.ListTransactions(ctx, inventoryUsecase.ListTransactionsRequest{
				GarageID: garageID, PartID: partC, RefType: inventoryDomain.RefStockCount,
			})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, count.Number, page.Items[0].RefNumber)
			assert.Equal(t, "Owner Name", page.Items[0].CreatedByName)
			assert.Equal(t, "7", page.Items[0].QuantityAfter)
			assert.Equal(t, inventoryDomain.TransactionIn, page.Items[0].Type)
			require.NotNil(t, page.Items[0].UnitCost)
			assert.Equal(t, "200", *page.Items[0].UnitCost, "valued at the average cost")

			return nil
		})

		// Cancelling the completed count posts the reversal.
		must(func(ctx context.Context) error {
			var err error

			count, err = inv.CancelStockCount(ctx, garageID, count.ID, userID)

			return err
		})
		assert.Equal(t, inventoryDomain.StockCountCancelled, count.Status)
		assert.Equal(t, "20", level(partB).QuantityOnHand)
		assert.Equal(t, "5", level(partC).QuantityOnHand)
		assert.Equal(t, "50", *level(partB).AverageCost)

		err = in(func(ctx context.Context) error {
			_, err := inv.CancelStockCount(ctx, garageID, count.ID, userID)

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonStockCountAlreadyCancelled, reason(err))
	})

	t.Run("a reversal is refused when the stock was used", func(t *testing.T) {
		var count *inventoryDomain.StockCount

		must(func(ctx context.Context) error {
			var err error
			if count, err = inv.CreateStockCount(ctx, inventoryUsecase.CreateStockCountRequest{GarageID: garageID, ActorID: userID}); err != nil {
				return err
			}

			if count, err = inv.AddStockCountLines(ctx, inventoryUsecase.AddStockCountLinesRequest{
				GarageID: garageID, CountID: count.ID, ActorID: userID, PartIDs: []string{partC},
			}); err != nil {
				return err
			}

			if count, err = inv.UpdateStockCountLine(ctx, inventoryUsecase.UpdateStockCountLineRequest{
				GarageID: garageID, CountID: count.ID, LineID: count.Lines[0].ID, ActorID: userID, EnteredCounted: "9",
			}); err != nil {
				return err
			}

			count, err = inv.CompleteStockCount(ctx, inventoryUsecase.CompleteStockCountRequest{GarageID: garageID, CountID: count.ID, ActorID: userID})

			return err
		})
		assert.Equal(t, "9", level(partC).QuantityOnHand)

		var writeOff *inventoryDomain.WriteOff

		must(func(ctx context.Context) error {
			var err error

			writeOff, err = inv.CreateWriteOff(ctx, inventoryUsecase.CreateWriteOffRequest{
				GarageID: garageID, ActorID: userID, Reason: inventoryDomain.WriteOffLost,
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partC, EnteredQuantity: "8"}},
			})

			return err
		})

		err := in(func(ctx context.Context) error {
			_, err := inv.CancelStockCount(ctx, garageID, count.ID, userID)

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonInsufficientStockToReverse, reason(err))

		must(func(ctx context.Context) error {
			_, err := inv.CancelWriteOff(ctx, garageID, writeOff.ID, userID)

			return err
		})
		must(func(ctx context.Context) error {
			_, err := inv.CancelStockCount(ctx, garageID, count.ID, userID)

			return err
		})
		assert.Equal(t, "5", level(partC).QuantityOnHand)
	})

	t.Run("drafts merge, skipping uncounted lines on completion", func(t *testing.T) {
		var first, second, merged *inventoryDomain.StockCount

		count := func(c **inventoryDomain.StockCount, parts map[string]string) {
			t.Helper()

			must(func(ctx context.Context) error {
				var err error
				if *c, err = inv.CreateStockCount(ctx, inventoryUsecase.CreateStockCountRequest{GarageID: garageID, ActorID: userID}); err != nil {
					return err
				}

				ids := make([]string, 0, len(parts))
				for id := range parts {
					ids = append(ids, id)
				}

				if *c, err = inv.AddStockCountLines(ctx, inventoryUsecase.AddStockCountLinesRequest{
					GarageID: garageID, CountID: (*c).ID, ActorID: userID, PartIDs: ids,
				}); err != nil {
					return err
				}

				for _, l := range (*c).Lines {
					if parts[l.PartID] == "" {
						continue
					}

					if *c, err = inv.UpdateStockCountLine(ctx, inventoryUsecase.UpdateStockCountLineRequest{
						GarageID: garageID, CountID: (*c).ID, LineID: l.ID, ActorID: userID, EnteredCounted: parts[l.PartID],
					}); err != nil {
						return err
					}
				}

				return nil
			})
		}

		count(&first, map[string]string{partA: "4"})
		count(&second, map[string]string{partA: "6", partB: "", partC: "6"})

		must(func(ctx context.Context) error {
			var err error

			merged, err = inv.MergeStockCounts(ctx, inventoryUsecase.MergeStockCountsRequest{
				GarageID: garageID, ActorID: userID, CountIDs: []string{first.ID, second.ID},
			})

			return err
		})
		assert.Equal(t, first.ID, merged.ID)
		require.Len(t, merged.Lines, 3)

		for _, l := range merged.Lines {
			if l.PartID == partA {
				assert.Equal(t, "10", *l.Counted, "a part counted on both adds up")
			}
		}

		must(func(ctx context.Context) error {
			got, err := inv.GetStockCount(ctx, garageID, second.ID)
			require.NoError(t, err)
			assert.Equal(t, inventoryDomain.StockCountCancelled, got.Status)
			assert.Empty(t, got.Lines)

			return nil
		})

		err := in(func(ctx context.Context) error {
			_, err := inv.MergeStockCounts(ctx, inventoryUsecase.MergeStockCountsRequest{GarageID: garageID, ActorID: userID, CountIDs: []string{first.ID, second.ID}})

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonStockCountNotDraft, reason(err))

		// The merged B line was never counted: completing skips it on request.
		must(func(ctx context.Context) error {
			var err error

			merged, err = inv.CompleteStockCount(ctx, inventoryUsecase.CompleteStockCountRequest{
				GarageID: garageID, CountID: merged.ID, ActorID: userID, SkipUncounted: true,
			})

			return err
		})
		assert.Len(t, merged.Lines, 2)
		assert.Equal(t, "20", level(partB).QuantityOnHand)

		page := new(inventoryUsecase.Page[*inventoryDomain.StockCount])

		must(func(ctx context.Context) error {
			var err error

			page, err = inv.ListStockCounts(ctx, inventoryUsecase.ListStockCountsRequest{GarageID: garageID, Status: inventoryDomain.StockCountCompleted})

			return err
		})
		require.Len(t, page.Items, 1)
		assert.Equal(t, inventoryDomain.Tally{Matched: 1, Over: 1}, page.Items[0].Tally)
		assert.EqualValues(t, 200, page.Items[0].TotalDifferenceValue)
		assert.Empty(t, page.Items[0].Lines)
		assert.Equal(t, "6", level(partC).QuantityOnHand)

		// Put the stock back as it was for the next scenarios.
		must(func(ctx context.Context) error {
			_, err := inv.CancelStockCount(ctx, garageID, merged.ID, userID)

			return err
		})
		assert.Equal(t, "10", level(partA).QuantityOnHand)
		assert.Equal(t, "5", level(partC).QuantityOnHand)
	})

	t.Run("write-off reduces stock at average cost and cancels", func(t *testing.T) {
		var w *inventoryDomain.WriteOff

		must(func(ctx context.Context) error {
			var err error

			w, err = inv.CreateWriteOff(ctx, inventoryUsecase.CreateWriteOffRequest{
				GarageID: garageID, ActorID: userID, Reason: inventoryDomain.WriteOffDamaged, Notes: "dropped",
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partB, EnteredQuantity: "3"}, {PartID: partA, UnitID: carton, EnteredQuantity: "0.5"}},
			})

			return err
		})
		assert.Equal(t, "17", level(partB).QuantityOnHand)
		assert.Equal(t, "4", level(partA).QuantityOnHand)
		assert.EqualValues(t, 3*50+6*100, w.TotalValue)
		assert.Equal(t, "Owner Name", w.CreatedByName)
		assert.Regexp(t, `^XH-\d{6}-0002$`, w.Number)

		// Not enough on hand.
		err := in(func(ctx context.Context) error {
			_, err := inv.CreateWriteOff(ctx, inventoryUsecase.CreateWriteOffRequest{
				GarageID: garageID, ActorID: userID, Reason: inventoryDomain.WriteOffExpired,
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partB, EnteredQuantity: "10"}, {PartID: partB, EnteredQuantity: "10"}},
			})

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonInsufficientStock, reason(err))

		must(func(ctx context.Context) error {
			var err error

			w, err = inv.CancelWriteOff(ctx, garageID, w.ID, userID)

			return err
		})
		assert.Equal(t, inventoryDomain.AdjustmentCancelled, w.Status)
		assert.Equal(t, "20", level(partB).QuantityOnHand)
		assert.Equal(t, "10", level(partA).QuantityOnHand)
		assert.Equal(t, "50", *level(partB).AverageCost)

		err = in(func(ctx context.Context) error {
			_, err := inv.CancelWriteOff(ctx, garageID, w.ID, userID)

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonWriteOffAlreadyCancelled, reason(err))

		must(func(ctx context.Context) error {
			page, err := inv.ListWriteOffs(ctx, inventoryUsecase.ListWriteOffsRequest{GarageID: garageID, Reason: inventoryDomain.WriteOffDamaged})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.EqualValues(t, 3*50+6*100, page.Items[0].TotalValue)

			return nil
		})
	})

	t.Run("return to supplier records the credit", func(t *testing.T) {
		var ret *inventoryDomain.SupplierReturn

		// From the receipt: the credit defaults to what the receipt paid.
		must(func(ctx context.Context) error {
			var err error

			ret, err = inv.CreateSupplierReturn(ctx, inventoryUsecase.CreateSupplierReturnRequest{
				GarageID: garageID, ActorID: userID, SupplierID: supplierID, GoodsReceiptID: receipt.ID, CreditNoteNumber: "CN-1",
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partA, EnteredQuantity: "4"}},
			})

			return err
		})
		assert.EqualValues(t, 400, ret.CreditAmount)
		assert.Equal(t, receipt.Number, ret.GoodsReceiptNumber)
		assert.Equal(t, "Supplier A", ret.SupplierName)
		assert.Equal(t, "6", level(partA).QuantityOnHand)

		// A typed credit wins when the caller may set costs; without a receipt the average cost applies.
		var typedCredit, average *inventoryDomain.SupplierReturn

		must(func(ctx context.Context) error {
			var err error
			if typedCredit, err = inv.CreateSupplierReturn(ctx, inventoryUsecase.CreateSupplierReturnRequest{
				GarageID: garageID, ActorID: userID, SupplierID: supplierID, CanSetCost: true,
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partB, EnteredQuantity: "2", UnitCost: 45}},
			}); err != nil {
				return err
			}

			average, err = inv.CreateSupplierReturn(ctx, inventoryUsecase.CreateSupplierReturnRequest{
				GarageID: garageID, ActorID: userID, SupplierID: supplierID, CanSetCost: false,
				Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partB, EnteredQuantity: "2", UnitCost: 1}},
			})

			return err
		})
		assert.EqualValues(t, 90, typedCredit.CreditAmount)
		assert.EqualValues(t, 100, average.CreditAmount, "a caller without cost access gets the default credit")
		assert.Equal(t, "16", level(partB).QuantityOnHand)

		// Refusals.
		for name, req := range map[string]inventoryUsecase.CreateSupplierReturnRequest{
			"insufficient": {SupplierID: supplierID, Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partA, EnteredQuantity: "100"}}},
			"unknown supplier": {
				SupplierID: uuid.NewString(), Lines: []inventoryUsecase.AdjustmentLineInput{{PartID: partA, EnteredQuantity: "1"}},
			},
		} {
			req.GarageID, req.ActorID = garageID, userID

			require.Error(t, in(func(ctx context.Context) error {
				_, err := inv.CreateSupplierReturn(ctx, req)

				return err
			}), name)
		}

		must(func(ctx context.Context) error {
			for _, r := range []*inventoryDomain.SupplierReturn{ret, typedCredit, average} {
				if _, err := inv.CancelSupplierReturn(ctx, garageID, r.ID, userID); err != nil {
					return err
				}
			}

			return nil
		})
		assert.Equal(t, "10", level(partA).QuantityOnHand)
		assert.Equal(t, "20", level(partB).QuantityOnHand)

		err := in(func(ctx context.Context) error {
			_, err := inv.CancelSupplierReturn(ctx, garageID, ret.ID, userID)

			return err
		})
		assert.Equal(t, inventoryUsecase.ReasonSupplierReturnAlreadyCancelled, reason(err))

		// The supplier is referenced by documents, so it can only be deactivated.
		must(func(ctx context.Context) error {
			referenced, err := module.repo.SupplierReferenced(ctx, garageID, supplierID)
			require.NoError(t, err)
			assert.True(t, referenced)

			return nil
		})
	})

	t.Run("unused repair order parts return to stock", func(t *testing.T) {
		returnPart := func(quantity, why string) (*inventoryDomain.RepairOrderPartReturn, error) {
			var got *inventoryDomain.RepairOrderPartReturn

			err := in(func(ctx context.Context) error {
				var err error

				got, err = inv.ReturnRepairOrderPart(ctx, inventoryUsecase.ReturnRepairOrderPartRequest{
					GarageID: garageID, RepairOrderID: orderID, ActorID: userID, RepairOrderLineID: orderLineID,
					EnteredQuantity: quantity, Reason: why,
				})

				return err
			})

			return got, err
		}

		var issue *inventoryDomain.StockIssue

		must(func(ctx context.Context) error {
			var err error

			issue, err = inv.CreateStockIssue(ctx, inventoryUsecase.CreateStockIssueRequest{
				GarageID: garageID, RepairOrderID: orderID, RepairOrderNumber: "LSC-1",
				Lines: []inventoryUsecase.IssueLineInput{{RepairOrderLineID: orderLineID, PartID: partB, Quantity: "4"}},
			})

			return err
		})

		// Nothing has left stock while the issue is a draft.
		_, err := returnPart("1", "not needed")
		assert.Equal(t, inventoryUsecase.ReasonStockIssueNotPosted, reason(err))

		must(func(ctx context.Context) error {
			_, err := inv.PostStockIssue(ctx, garageID, issue.ID, userID)

			return err
		})
		assert.Equal(t, "16", level(partB).QuantityOnHand)

		got, err := returnPart("1.5", "customer declined")
		require.NoError(t, err)
		assert.Equal(t, "17.5", level(partB).QuantityOnHand)
		assert.Equal(t, "Part B", got.PartName)
		require.NotNil(t, got.UnitCost)
		assert.Equal(t, "50", *got.UnitCost, "restored at the cost they left with")
		assert.Equal(t, "50", *level(partB).AverageCost)

		_, err = returnPart("3", "too many")
		assert.Equal(t, inventoryUsecase.ReasonReturnExceedsIssued, reason(err), "2.5 are still returnable")

		_, err = returnPart("1", "  ")
		require.Error(t, err)

		must(func(ctx context.Context) error {
			list, err := inv.ListRepairOrderPartReturns(ctx, garageID, orderID)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, "customer declined", list[0].Reason)

			again, err := inv.GetRepairOrderStockIssue(ctx, garageID, orderID)
			require.NoError(t, err)
			assert.Equal(t, "1.5", again.Lines[0].ReturnedQuantity)

			page, err := inv.ListTransactions(ctx, inventoryUsecase.ListTransactionsRequest{GarageID: garageID, PartID: partB, RefType: inventoryDomain.RefRepairOrderReturn})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			assert.Equal(t, "customer declined", page.Items[0].Reason)

			return nil
		})
	})

	t.Run("another garage sees none of it", func(t *testing.T) {
		require.NoError(t, txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: otherGarageID, UserID: userID}, func(ctx context.Context) error {
			counts, err := inv.ListStockCounts(ctx, inventoryUsecase.ListStockCountsRequest{GarageID: otherGarageID})
			require.NoError(t, err)
			assert.Empty(t, counts.Items)

			writeOffs, err := inv.ListWriteOffs(ctx, inventoryUsecase.ListWriteOffsRequest{GarageID: otherGarageID})
			require.NoError(t, err)
			assert.Empty(t, writeOffs.Items)

			return nil
		}))
	})
}
