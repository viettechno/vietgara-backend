//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	catalogueDomain "github.com/viettechno/vietgara-backend/internal/catalogue/domain"
	catalogueRepository "github.com/viettechno/vietgara-backend/internal/catalogue/repository"
	catalogueUsecase "github.com/viettechno/vietgara-backend/internal/catalogue/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	inventoryDomain "github.com/viettechno/vietgara-backend/internal/inventory/domain"
	inventoryRepository "github.com/viettechno/vietgara-backend/internal/inventory/repository"
	inventoryUsecase "github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	quoteDomain "github.com/viettechno/vietgara-backend/internal/quote/domain"
	quoteRepository "github.com/viettechno/vietgara-backend/internal/quote/repository"
	repairorderDomain "github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	repairorderRepository "github.com/viettechno/vietgara-backend/internal/repairorder/repository"
	repairorderUsecase "github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	supplierRepository "github.com/viettechno/vietgara-backend/internal/supplier/repository"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/lifecycle"
)

// The shortage marks as the events carry them.
const (
	marked  = "true"
	cleared = "false"
)

// TestJobsAndReservation runs the Release 7.3 modules against a real
// PostgreSQL: the job catalogue with its delete-or-deactivate rule, the job
// and hidden-consumable columns of quote and repair order lines, line
// editing, and the shortage marks of draft stock issues.
//
//nolint:funlen,maintidx // one scenario through the real schema, read top to bottom
func TestJobsAndReservation(t *testing.T) {
	pg := pgtest.StartMigrated(t)
	userID, garageID := uuid.NewString(), uuid.NewString()
	customerID, vehicleID, supplierID := uuid.NewString(), uuid.NewString(), uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, userID)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $2, 'A')`, garageID, userID)
	exec(`INSERT INTO customers (id, garage_id, full_name) VALUES ($1, $2, 'An')`, customerID, garageID)
	exec(`INSERT INTO vehicles (id, garage_id, customer_id, license_plate) VALUES ($1, $2, $3, '51F-111.11')`, vehicleID, garageID, customerID)
	exec(`INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $2, 'Supplier A')`, supplierID, garageID)

	store, err := storage.NewLocal(t.TempDir(), "", nil)
	require.NoError(t, err)

	pool := pg.AppPool(t)
	txm := dbpkg.NewTxManager(pool)
	entities := &fileEntities{}
	files := buildFiles(pool, store, tenantRepo.NewPostgresGarageRepository(pool), catalogueRepository.NewPostgresRepository(pool), entities)
	usage := &catalogueUsage{}
	catalogue := buildCatalogue(pool, files, usage)
	entities.parts = catalogue.uc

	quotes := quoteRepository.NewPostgresRepository(pool)
	orders := repairorderRepository.NewPostgresRepository(pool)
	usage.inventory, usage.quotes, usage.repairOrders = inventoryRepository.NewPostgresRepository(pool), quotes, orders

	uc := catalogue.uc
	inv := buildInventory(pool, outbox.NewEventPublisher(pool), inventoryParts{partQuantities{uc: uc}},
		inventoryGarages{garages: tenantRepo.NewPostgresGarageRepository(pool)},
		inventoryDirectory{suppliers: supplierRepository.NewPostgresRepository(pool), users: identityRepo.NewPostgresUserRepository(pool)},
		audit.NewLogger(pool)).uc

	inGarage := func(fn func(ctx context.Context)) {
		t.Helper()

		scope := dbpkg.Scope{GarageID: garageID, UserID: userID}

		require.NoError(t, txm.WithTransaction(t.Context(), scope, func(ctx context.Context) error {
			fn(access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true}))

			return nil
		}))
	}

	failing := func(fn func(ctx context.Context) error) error {
		t.Helper()

		return txm.WithTransaction(t.Context(), dbpkg.Scope{GarageID: garageID, UserID: userID}, func(ctx context.Context) error {
			return fn(access.WithAccess(ctx, &access.Access{GarageID: garageID, UserID: userID, OwnerUserID: userID, Owner: true, Active: true}))
		})
	}

	str := func(s string) *string { return &s }

	var (
		litre, jug *catalogueDomain.Unit
		oil, rag   *catalogueDomain.Part
		job        *catalogueDomain.Job
	)

	t.Run("a job suggests parts, priced from the catalogue", func(t *testing.T) {
		inGarage(func(ctx context.Context) {
			mk := func(name, abbr string) *catalogueDomain.Unit {
				u, err := uc.CreateUnit(ctx, catalogueUsecase.CreateUnitRequest{
					GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.UnitFields{Name: str(name), Abbreviation: str(abbr)},
				})
				require.NoError(t, err)

				return u
			}

			litre, jug = mk("Litre", "l"), mk("Jug", "jug")

			var err error

			oil, err = uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.PartFields{
					SKU: str("OIL-5W30"), Name: str("Engine oil 5W-30"), BaseUnitID: &litre.ID, SalePrice: new(int64(90_000)),
				},
			})
			require.NoError(t, err)

			rag, err = uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.PartFields{
					SKU: str("RAG"), Name: str("Shop rag"), BaseUnitID: &litre.ID, SalePrice: new(int64(1_000)),
				},
			})
			require.NoError(t, err)

			oil, err = uc.SetPartUnits(ctx, garageID, oil.ID, userID, []catalogueDomain.PackagingUnit{{UnitID: jug.ID, Factor: "5"}})
			require.NoError(t, err)

			parts := []catalogueUsecase.JobPartInput{
				{PartID: oil.ID, Quantity: "4"},
				{PartID: rag.ID, Quantity: "1", NonBillable: true},
			}

			job, err = uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{
					Name: str("Oil change"), Category: str("Maintenance"), LaborPrice: new(int64(200_000)), DurationMinutes: new(45),
					Parts: &parts,
				},
			})
			require.NoError(t, err)
			require.Len(t, job.Parts, 2)
			assert.Equal(t, "OIL-5W30", job.Parts[0].PartSKU)
			assert.Equal(t, int64(90_000), job.Parts[0].UnitPrice)
			assert.True(t, job.Parts[1].NonBillable)

			// A suggestion in the packaging unit is priced per jug: 5 litres.
			job, err = uc.UpdateJob(ctx, catalogueUsecase.UpdateJobRequest{
				GarageID: garageID, JobID: job.ID, ActorID: userID, Fields: catalogueUsecase.JobFields{Parts: &[]catalogueUsecase.JobPartInput{
					{PartID: oil.ID, Quantity: "1", UnitID: &jug.ID},
					{PartID: rag.ID, Quantity: "1", NonBillable: true},
				}},
			})
			require.NoError(t, err)
			assert.Equal(t, "jug", job.Parts[0].UnitAbbreviation)
			assert.Equal(t, int64(450_000), job.Parts[0].UnitPrice)

			list, err := uc.ListJobs(ctx, catalogueUsecase.ListJobsRequest{GarageID: garageID, Query: "oil"})
			require.NoError(t, err)
			assert.Equal(t, 1, list.Total)

			list, err = uc.ListJobs(ctx, catalogueUsecase.ListJobsRequest{GarageID: garageID, Category: "Nothing"})
			require.NoError(t, err)
			assert.Zero(t, list.Total)
		})
	})

	t.Run("the quote reads a job's lines priced per base unit", func(t *testing.T) {
		inGarage(func(ctx context.Context) {
			j, err := quoteJobs{jobs: uc}.Job(ctx, garageID, job.ID)
			require.NoError(t, err)
			require.Len(t, j.Parts, 2)

			// One jug of 5 litres: the line is priced per litre, so 5 litres cost one jug's price.
			assert.Equal(t, jug.ID, j.Parts[0].UnitID)
			assert.Equal(t, int64(90_000), j.Parts[0].UnitPrice)
			assert.True(t, j.Parts[1].NonBillable)

			// A suggestion whose part is deactivated is left out, an inactive job refused.
			_, err = uc.UpdatePart(ctx, catalogueUsecase.UpdatePartRequest{
				GarageID: garageID, PartID: rag.ID, Fields: catalogueUsecase.PartFields{IsActive: new(false)},
			})
			require.NoError(t, err)

			j, err = quoteJobs{jobs: uc}.Job(ctx, garageID, job.ID)
			require.NoError(t, err)
			assert.Len(t, j.Parts, 1)

			_, err = uc.UpdatePart(ctx, catalogueUsecase.UpdatePartRequest{
				GarageID: garageID, PartID: rag.ID, Fields: catalogueUsecase.PartFields{IsActive: new(true)},
			})
			require.NoError(t, err)

			_, err = uc.UpdateJob(ctx, catalogueUsecase.UpdateJobRequest{
				GarageID: garageID, JobID: job.ID, ActorID: userID, Fields: catalogueUsecase.JobFields{IsActive: new(false)},
			})
			require.NoError(t, err)

			_, err = quoteJobs{jobs: uc}.Job(ctx, garageID, job.ID)
			require.ErrorContains(t, err, "not active")

			_, err = uc.UpdateJob(ctx, catalogueUsecase.UpdateJobRequest{
				GarageID: garageID, JobID: job.ID, ActorID: userID, Fields: catalogueUsecase.JobFields{IsActive: new(true)},
			})
			require.NoError(t, err)
		})
	})

	t.Run("a job name is unique, without regard to case", func(t *testing.T) {
		err := failing(func(ctx context.Context) error {
			_, err := uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{Name: str("OIL CHANGE")},
			})

			return err
		})
		require.ErrorIs(t, err, catalogueDomain.ErrJobNameTaken)
	})

	t.Run("the packaging unit a job uses cannot be removed", func(t *testing.T) {
		err := failing(func(ctx context.Context) error {
			_, err := uc.SetPartUnits(ctx, garageID, oil.ID, userID, nil)

			return err
		})
		require.ErrorIs(t, err, catalogueDomain.ErrPackagingUnitUsedByJob)

		inGarage(func(ctx context.Context) {
			// Changing the factor of the unit stays possible.
			_, err := uc.SetPartUnits(ctx, garageID, oil.ID, userID, []catalogueDomain.PackagingUnit{{UnitID: jug.ID, Factor: "4"}})
			require.NoError(t, err)
		})
	})

	t.Run("a part a job suggests is deactivated, not deleted, and its base unit is locked", func(t *testing.T) {
		inGarage(func(ctx context.Context) {
			outcome, err := uc.DeletePart(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: rag.ID, ActorID: userID, DryRun: true})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deactivated, outcome)

			_, err = uc.UpdatePart(ctx, catalogueUsecase.UpdatePartRequest{
				GarageID: garageID, PartID: rag.ID, Fields: catalogueUsecase.PartFields{BaseUnitID: &jug.ID},
			})
			require.ErrorIs(t, err, catalogueDomain.ErrBaseUnitLocked)
		})
	})

	t.Run("lines remember their job and hidden consumables", func(t *testing.T) {
		quoteID := uuid.NewString()
		quote := &quoteDomain.Quote{
			ID: quoteID, GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, CustomerName: "An",
			VehicleLicensePlate: "51F-111.11", Number: "BG-73", Status: quoteDomain.StatusDraft, ApprovalMode: quoteDomain.ApprovalModeLink,
			Lines: []quoteDomain.Line{
				{
					ID: uuid.NewString(), Position: 1, ItemType: quoteDomain.ItemTypeLabor, Description: "Oil change", Quantity: "1",
					UnitPrice: 200_000, LineTotal: 200_000, JobID: &job.ID, Decision: quoteDomain.DecisionPending,
				},
				{
					ID: uuid.NewString(), Position: 2, ItemType: quoteDomain.ItemTypePart, ItemRefID: &rag.ID, Description: "Shop rag",
					Quantity: "1", JobID: &job.ID, NonBillable: true, Decision: quoteDomain.DecisionPending,
				},
			},
			TotalAmount: 200_000,
		}

		inGarage(func(ctx context.Context) {
			require.NoError(t, quotes.Create(ctx, quote))

			got, err := quotes.FindByID(ctx, garageID, quoteID)
			require.NoError(t, err)
			require.Len(t, got.Lines, 2)
			assert.Equal(t, job.ID, *got.Lines[0].JobID)
			assert.True(t, got.Lines[1].NonBillable)
			assert.False(t, got.Lines[0].NonBillable)
		})

		inGarage(func(ctx context.Context) {
			used, err := usage.JobInUse(ctx, garageID, job.ID)
			require.NoError(t, err)
			assert.True(t, used)

			outcome, err := uc.DeleteJob(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: job.ID, ActorID: userID})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deactivated, outcome, "a job used on a quote is only deactivated")

			listed, err := uc.ListJobs(ctx, catalogueUsecase.ListJobsRequest{GarageID: garageID})
			require.NoError(t, err)
			assert.Zero(t, listed.Total, "an inactive job leaves the picker")

			listed, err = uc.ListJobs(ctx, catalogueUsecase.ListJobsRequest{GarageID: garageID, IncludeInactive: true})
			require.NoError(t, err)
			assert.Equal(t, 1, listed.Total)

			again, err := uc.ActivateJob(ctx, garageID, job.ID, userID)
			require.NoError(t, err)
			assert.True(t, again.IsActive)
		})
	})

	t.Run("hidden consumables are left out of the printed documents", func(t *testing.T) {
		quoteID := uuid.NewString()
		quote := &quoteDomain.Quote{
			ID: quoteID, GarageID: garageID, CustomerID: customerID, VehicleID: vehicleID, CustomerName: "An",
			VehicleLicensePlate: "51F-111.11", Number: "BG-74", Status: quoteDomain.StatusDraft, ApprovalMode: quoteDomain.ApprovalModeLink,
			Lines: []quoteDomain.Line{
				{ID: uuid.NewString(), Position: 1, ItemType: quoteDomain.ItemTypeLabor, Description: "Labor", Quantity: "1", UnitPrice: 1000, LineTotal: 1000, Decision: quoteDomain.DecisionPending},
				{
					ID: uuid.NewString(), Position: 2, ItemType: quoteDomain.ItemTypePart, ItemRefID: &rag.ID, Description: "Hidden rag",
					Quantity: "1", NonBillable: true, Decision: quoteDomain.DecisionPending,
				},
			},
			TotalAmount: 1000,
		}

		inGarage(func(ctx context.Context) {
			require.NoError(t, quotes.Create(ctx, quote))

			info, err := documentQuotes{quotes: quotes}.Get(ctx, garageID, quoteID)
			require.NoError(t, err)
			require.Len(t, info.Lines, 1)
			assert.Equal(t, "Labor", info.Lines[0].Description)
			assert.Equal(t, int64(1000), info.TotalAmount)

			ro := &repairorderDomain.RepairOrder{
				ID: uuid.NewString(), GarageID: garageID, VehicleID: vehicleID, VehicleLicensePlate: "51F-111.11", CustomerName: "An",
				Number: "LSC-74", Status: repairorderDomain.StatusOpen,
				Lines: []repairorderDomain.Line{
					{ID: uuid.NewString(), Position: 1, ItemType: repairorderDomain.ItemTypeLabor, Description: "Labor", Quantity: "1", UnitPrice: 1000, LineTotal: 1000, Status: repairorderDomain.LineNotStarted},
					{
						ID: uuid.NewString(), Position: 2, ItemType: repairorderDomain.ItemTypePart, ItemRefID: &rag.ID, Description: "Hidden rag",
						Quantity: "1", NonBillable: true, Status: repairorderDomain.LineNotStarted,
					},
				},
				TotalAmount: 1000,
			}
			require.NoError(t, orders.Create(ctx, ro))

			orderInfo, err := documentRepairOrders{uc: repairorderUsecase.NewRepairOrderUseCase(repairorderUsecase.Deps{RepairOrders: orders})}.
				Get(ctx, garageID, ro.ID)
			require.NoError(t, err)
			require.Len(t, orderInfo.Lines, 1)
			assert.Equal(t, "Labor", orderInfo.Lines[0].Description)
		})
	})

	t.Run("an unused job is removed, and its name freed", func(t *testing.T) {
		inGarage(func(ctx context.Context) {
			spare, err := uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{Name: str("Spare job")},
			})
			require.NoError(t, err)

			outcome, err := uc.DeleteJob(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: spare.ID, ActorID: userID})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deleted, outcome)

			_, err = uc.GetJob(ctx, garageID, spare.ID)
			require.ErrorIs(t, err, catalogueDomain.ErrJobNotFound)

			_, err = uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{Name: str("Spare job")},
			})
			require.NoError(t, err)
		})
	})

	t.Run("a deleted job no longer holds a packaging unit", func(t *testing.T) {
		inGarage(func(ctx context.Context) {
			coolant, err := uc.CreatePart(ctx, catalogueUsecase.CreatePartRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.PartFields{SKU: str("COOLANT"), Name: str("Coolant"), BaseUnitID: &litre.ID},
			})
			require.NoError(t, err)

			_, err = uc.SetPartUnits(ctx, garageID, coolant.ID, userID, []catalogueDomain.PackagingUnit{{UnitID: jug.ID, Factor: "5"}})
			require.NoError(t, err)

			temp, err := uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{
					Name: str("Coolant flush"), Parts: &[]catalogueUsecase.JobPartInput{{PartID: coolant.ID, Quantity: "1", UnitID: &jug.ID}},
				},
			})
			require.NoError(t, err)

			outcome, err := uc.DeleteJob(ctx, catalogueUsecase.DeleteRequest{GarageID: garageID, ID: temp.ID, ActorID: userID})
			require.NoError(t, err)
			assert.Equal(t, lifecycle.Deleted, outcome)

			_, err = uc.SetPartUnits(ctx, garageID, coolant.ID, userID, nil)
			require.NoError(t, err, "the deleted job suggests nothing")
		})
	})

	t.Run("a repair order keeps the waiting mark, edited lines and the cancelled status", func(t *testing.T) {
		var onOrderOnly *catalogueDomain.Job

		inGarage(func(ctx context.Context) {
			var err error

			onOrderOnly, err = uc.CreateJob(ctx, catalogueUsecase.CreateJobRequest{
				GarageID: garageID, ActorID: userID, Fields: catalogueUsecase.JobFields{Name: str("Only on an order")},
			})
			require.NoError(t, err)
		})

		ro := &repairorderDomain.RepairOrder{
			ID: uuid.NewString(), GarageID: garageID, VehicleID: vehicleID, VehicleLicensePlate: "51F-111.11", CustomerName: "An",
			Number: "LSC-73", Status: repairorderDomain.StatusOpen,
			Lines: []repairorderDomain.Line{
				{
					ID: uuid.NewString(), Position: 1, ItemType: repairorderDomain.ItemTypeLabor, Description: "Oil change", Quantity: "1",
					UnitPrice: 200_000, LineTotal: 200_000, JobID: &onOrderOnly.ID, Status: repairorderDomain.LineNotStarted,
				},
				{
					ID: uuid.NewString(), Position: 2, ItemType: repairorderDomain.ItemTypePart, ItemRefID: &rag.ID, Description: "Shop rag",
					Quantity: "1", NonBillable: true, Status: repairorderDomain.LineNotStarted,
				},
			},
			TotalAmount: 200_000,
		}

		inGarage(func(ctx context.Context) {
			require.NoError(t, orders.Create(ctx, ro))

			got, err := orders.FindByID(ctx, garageID, ro.ID)
			require.NoError(t, err)
			assert.True(t, got.Lines[1].NonBillable)
			assert.Equal(t, onOrderOnly.ID, *got.Lines[0].JobID)

			// A job that only an order's line came from is deactivated, not deleted.
			used, err := usage.JobInUse(ctx, garageID, onOrderOnly.ID)
			require.NoError(t, err)
			assert.True(t, used)

			require.NoError(t, orders.Lock(ctx, garageID, ro.ID))

			// Edit one line, drop one, add one: ids of the kept lines stay.
			kept := got.Lines[0]
			kept.Description, kept.UnitPrice, kept.LineTotal = "Oil and filter change", 250_000, 250_000
			added := repairorderDomain.Line{
				ID: uuid.NewString(), Position: 2, ItemType: repairorderDomain.ItemTypeLabor, Description: "Wash", Quantity: "1",
				UnitPrice: 50_000, LineTotal: 50_000, Status: repairorderDomain.LineNotStarted,
			}
			got.Lines, got.TotalAmount, got.WaitingOnParts = []repairorderDomain.Line{kept, added}, 300_000, true
			got.DeriveStatus()
			require.NoError(t, orders.ReplaceLines(ctx, got))

			again, err := orders.FindByID(ctx, garageID, ro.ID)
			require.NoError(t, err)
			require.Len(t, again.Lines, 2)
			assert.Equal(t, kept.ID, again.Lines[0].ID)
			assert.Equal(t, "Oil and filter change", again.Lines[0].Description)
			assert.Equal(t, "Wash", again.Lines[1].Description)
			assert.Equal(t, int64(300_000), again.TotalAmount)
			assert.True(t, again.WaitingOnParts)
			assert.Equal(t, repairorderDomain.StatusWaitingOnParts, again.Status)

			again.Status, again.WaitingOnParts = repairorderDomain.StatusCancelled, false
			require.NoError(t, orders.Save(ctx, again))

			cancelled, err := orders.FindByID(ctx, garageID, ro.ID)
			require.NoError(t, err)
			assert.Equal(t, repairorderDomain.StatusCancelled, cancelled.Status)
		})
	})

	t.Run("draft stock issues are served in order, and the marks follow the stock", func(t *testing.T) {
		orderA, orderB := uuid.NewString(), uuid.NewString()
		lineA, lineB := uuid.NewString(), uuid.NewString()

		// The scenario runs in one transaction, so the events are read in it.
		var scenario context.Context

		shortMarks := func(order string) []string {
			t.Helper()

			rows, err := dbpkg.QuerierFromContext(scenario, pool).Query(scenario,
				`SELECT payload->>'short' FROM outbox_events
				 WHERE event_type = 'stock_issue.shortage_changed' AND payload->>'repair_order_id' = $1 ORDER BY created_at, id`, order)
			require.NoError(t, err)

			defer rows.Close()

			var out []string

			for rows.Next() {
				var s string
				require.NoError(t, rows.Scan(&s))

				out = append(out, s)
			}

			require.NoError(t, rows.Err())

			return out
		}

		issueFor := func(order, line, quantity string) inventoryUsecase.CreateStockIssueRequest {
			return inventoryUsecase.CreateStockIssueRequest{
				GarageID: garageID, RepairOrderID: order, RepairOrderNumber: "LSC-" + order[:4],
				Lines: []inventoryUsecase.IssueLineInput{{RepairOrderLineID: line, PartID: oil.ID, Quantity: quantity, EnteredQuantity: quantity}},
			}
		}

		receive := func(ctx context.Context, quantity string) {
			t.Helper()

			r, err := inv.CreateGoodsReceipt(ctx, inventoryUsecase.CreateGoodsReceiptRequest{
				GarageID: garageID, ActorID: userID, CanSetCost: true, DefaultSupplierID: supplierID,
				Lines: []inventoryUsecase.ReceiptLineInput{{PartID: oil.ID, EnteredQuantity: quantity, UnitCost: 60_000}},
			})
			require.NoError(t, err)

			_, err = inv.CompleteGoodsReceipt(ctx, garageID, r.ID, userID)
			require.NoError(t, err)
		}

		inGarage(func(ctx context.Context) {
			scenario = ctx //nolint:fatcontext // the scenario runs in one transaction, whose events the checks read

			receive(ctx, "5")

			_, err := inv.CreateStockIssue(ctx, issueFor(orderA, lineA, "4"))
			require.NoError(t, err)

			_, err = inv.CreateStockIssue(ctx, issueFor(orderB, lineB, "4"))
			require.NoError(t, err)

			a, err := inv.GetRepairOrderStockIssue(ctx, garageID, orderA)
			require.NoError(t, err)
			assert.False(t, a.IsShort, "the first order is served first")
			assert.Equal(t, "0", a.Lines[0].ShortQuantity)

			b, err := inv.GetRepairOrderStockIssue(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.True(t, b.IsShort)
			assert.Equal(t, "3", b.Lines[0].ShortQuantity, "5 on hand, 4 held by the first order")
			assert.ElementsMatch(t, []string{marked}, shortMarks(orderB))
			assert.Empty(t, shortMarks(orderA))

			// Stock arrives: the second order is covered and told so.
			receive(ctx, "10")

			b, err = inv.GetRepairOrderStockIssue(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.False(t, b.IsShort)
			assert.ElementsMatch(t, []string{marked, cleared}, shortMarks(orderB))

			// The first order grows past the stock: it is short, the second too.
			require.NoError(t, inv.ResyncStockIssue(ctx, issueFor(orderA, lineA, "14")))

			a, err = inv.GetRepairOrderStockIssue(ctx, garageID, orderA)
			require.NoError(t, err)
			assert.False(t, a.IsShort, "14 of 15 is still covered")

			b, err = inv.GetRepairOrderStockIssue(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.True(t, b.IsShort, "only 1 left for the 4 it holds")
			assert.ElementsMatch(t, []string{marked, cleared, marked}, shortMarks(orderB))

			// Cancelling the first order releases its reservation.
			require.NoError(t, inv.CancelStockIssue(ctx, garageID, orderA))

			a, err = inv.GetRepairOrderStockIssue(ctx, garageID, orderA)
			require.NoError(t, err)
			assert.Equal(t, inventoryDomain.StockIssueCancelled, a.Status)

			b, err = inv.GetRepairOrderStockIssue(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.False(t, b.IsShort)
			assert.ElementsMatch(t, []string{marked, cleared, marked, cleared}, shortMarks(orderB))

			posted, err := inv.StockIssuePosted(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.False(t, posted)

			// The reservation shows on the stock level: only B's 4 is held.
			level, err := inv.GetStockLevel(ctx, garageID, oil.ID)
			require.NoError(t, err)
			assert.Equal(t, "15", level.QuantityOnHand)
			assert.Equal(t, "4", level.QuantityReserved)
			assert.Equal(t, "11", level.QuantityAvailable)

			// Posting the second issue takes the stock and ends the reservation.
			issue, err := inv.PostStockIssue(ctx, garageID, b.ID, userID)
			require.NoError(t, err)
			assert.Equal(t, inventoryDomain.StockIssuePosted, issue.Status)

			posted, err = inv.StockIssuePosted(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.True(t, posted)

			// A posted issue ignores later changes and cancellations.
			require.NoError(t, inv.ResyncStockIssue(ctx, issueFor(orderB, lineB, "1")))
			require.NoError(t, inv.CancelStockIssue(ctx, garageID, orderB))

			after, err := inv.GetRepairOrderStockIssue(ctx, garageID, orderB)
			require.NoError(t, err)
			assert.Equal(t, inventoryDomain.StockIssuePosted, after.Status)
			assert.Equal(t, "4", after.Lines[0].Quantity)
		})
	})
}
