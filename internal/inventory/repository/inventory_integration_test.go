//go:build integration

package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/repository"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	dbpkg "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/pgtest"
)

// fixture is a migrated database with one garage, a part, a second part and
// two suppliers, plus a second garage that must see none of it.
type fixture struct {
	t                           *testing.T
	repo                        *repository.PostgresRepository
	txm                         *dbpkg.TxManager
	garage, other               string
	part, part2, supplier, sup2 string
	unit                        string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	pg := pgtest.StartMigrated(t)
	f := &fixture{
		t: t, garage: uuid.NewString(), other: uuid.NewString(), part: uuid.NewString(), part2: uuid.NewString(),
		supplier: uuid.NewString(), sup2: uuid.NewString(), unit: uuid.NewString(),
	}
	user := uuid.NewString()

	exec := func(query string, args ...any) {
		t.Helper()

		_, err := pg.Admin.ExecContext(t.Context(), query, args...)
		require.NoError(t, err)
	}

	exec(`INSERT INTO users (id, email, full_name) VALUES ($1, 'owner@example.com', 'Owner')`, user)
	exec(`INSERT INTO garages (id, user_id, name) VALUES ($1, $3, 'A'), ($2, $3, 'B')`, f.garage, f.other, user)
	exec(`INSERT INTO units (id, garage_id, name, abbreviation) VALUES ($1, $2, 'Can', 'can')`, f.unit, f.garage)
	exec(`INSERT INTO parts (id, garage_id, sku, name, base_unit_id) VALUES ($1, $3, 'OIL', 'Oil', $4), ($2, $3, 'FILTER', 'Filter', $4)`,
		f.part, f.part2, f.garage, f.unit)
	exec(`INSERT INTO suppliers (id, garage_id, name) VALUES ($1, $3, 'Supplier A'), ($2, $3, 'Supplier B')`, f.supplier, f.sup2, f.garage)

	pool := pg.AppPool(t)
	f.txm, f.repo = dbpkg.NewTxManager(pool), repository.NewPostgresRepository(pool)

	return f
}

func (f *fixture) in(garageID string, fn func(ctx context.Context)) {
	f.t.Helper()

	require.NoError(f.t, f.txm.WithTransaction(f.t.Context(), dbpkg.GarageScope(garageID), func(ctx context.Context) error {
		fn(ctx)

		return nil
	}))
}

func (f *fixture) record(ctx context.Context, partID string, kind domain.TransactionType, ref domain.RefType, quantity string, cost *string) *domain.StockTransaction {
	f.t.Helper()

	refID := uuid.NewString()
	tx := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: f.garage, PartID: partID, Type: kind, Quantity: quantity, UnitCost: cost, RefType: ref, RefID: &refID,
	}
	require.NoError(f.t, f.repo.RecordTransaction(ctx, tx))

	return tx
}

func balance(t *testing.T, ctx context.Context, f *fixture, partID string) usecase.Balance {
	t.Helper()

	got, err := f.repo.Balances(ctx, f.garage, []string{partID})
	require.NoError(t, err)

	return got[partID]
}

func TestInventoryRepository_AverageCost(t *testing.T) {
	f := newFixture(t)

	f.in(f.garage, func(ctx context.Context) {
		f.record(ctx, f.part, domain.TransactionIn, domain.RefGoodsReceipt, "10", new("100"))
		assert.Equal(t, usecase.Balance{OnHand: "10", Reserved: "0", AverageCost: "100"}, balance(t, ctx, f, f.part))

		second := f.record(ctx, f.part, domain.TransactionIn, domain.RefGoodsReceipt, "10", new("200"))
		assert.Equal(t, usecase.Balance{OnHand: "20", Reserved: "0", AverageCost: "150"}, balance(t, ctx, f, f.part), "a second price changes the average")
		require.Nil(t, second.SupplierID)

		f.record(ctx, f.part, domain.TransactionIn, domain.RefGoodsReceipt, "5", nil)
		assert.Equal(t, "150", balance(t, ctx, f, f.part).AverageCost, "a receipt without a cost leaves the average alone")

		issue := f.record(ctx, f.part, domain.TransactionOut, domain.RefStockIssue, "5", nil)
		require.NotNil(t, issue.UnitCost)
		assert.Equal(t, "150", *issue.UnitCost, "an issue is stamped with the average cost")
		assert.Equal(t, "20", balance(t, ctx, f, f.part).OnHand)
		assert.Equal(t, "150", balance(t, ctx, f, f.part).AverageCost, "an issue never changes the average")

		reversal := &domain.StockTransaction{
			ID: uuid.NewString(), GarageID: f.garage, PartID: f.part, Type: domain.TransactionOut, Quantity: "10",
			UnitCost: new("200"), RefType: domain.RefGoodsReceipt, RefID: second.RefID,
		}
		require.NoError(t, f.repo.RecordTransaction(ctx, reversal))
		assert.Equal(t, usecase.Balance{OnHand: "10", Reserved: "0", AverageCost: "100"}, balance(t, ctx, f, f.part), "reversing a receipt unwinds its cost")

		// Stock may go negative when parts were used; a receipt then restarts the average.
		f.record(ctx, f.part2, domain.TransactionOut, domain.RefStockIssue, "3", nil)
		assert.Equal(t, "-3", balance(t, ctx, f, f.part2).OnHand)
		f.record(ctx, f.part2, domain.TransactionIn, domain.RefGoodsReceipt, "10", new("40"))
		assert.Equal(t, usecase.Balance{OnHand: "7", Reserved: "0", AverageCost: "40"}, balance(t, ctx, f, f.part2))
	})

	f.in(f.other, func(ctx context.Context) {
		got, err := f.repo.Balances(ctx, f.other, nil)
		require.NoError(t, err)
		assert.Empty(t, got, "another garage sees no balances")
	})
}

func TestInventoryRepository_GoodsReceipt(t *testing.T) {
	f := newFixture(t)
	receipt := &domain.GoodsReceipt{
		ID: uuid.NewString(), GarageID: f.garage, Number: "PN-1", Status: domain.GoodsReceiptDraft, DefaultSupplierID: &f.supplier,
		ReceiptDate: "2026-10-15", DeliveryNoteNumber: "DN-1", Notes: "morning",
		Lines: []domain.GoodsReceiptLine{
			{ID: uuid.NewString(), PartID: f.part, EnteredQuantity: "10", Quantity: "10", SupplierID: &f.supplier, UnitCost: 150000},
			{ID: uuid.NewString(), PartID: f.part2, EnteredQuantity: "2.5", Quantity: "2.5", SupplierID: &f.sup2, UnitCost: 1001},
		},
	}

	f.in(f.garage, func(ctx context.Context) {
		require.NoError(t, f.repo.CreateGoodsReceipt(ctx, receipt))

		got, err := f.repo.FindGoodsReceipt(ctx, f.garage, receipt.ID, true)
		require.NoError(t, err)
		assert.Equal(t, "2026-10-15", got.ReceiptDate)
		require.Len(t, got.Lines, 2)
		assert.Equal(t, "2.5", got.Lines[1].EnteredQuantity)
		assert.Equal(t, int64(1001), got.Lines[1].UnitCost)

		// Replace the lines: keep one, drop one, add one.
		extra := domain.GoodsReceiptLine{ID: uuid.NewString(), PartID: f.part, EnteredQuantity: "1", Quantity: "1", UnitCost: 5}
		got.Lines = []domain.GoodsReceiptLine{got.Lines[0], extra}
		got.Notes, got.Status = "changed", domain.GoodsReceiptCompleted
		require.NoError(t, f.repo.SaveGoodsReceipt(ctx, got))

		again, err := f.repo.FindGoodsReceipt(ctx, f.garage, receipt.ID, false)
		require.NoError(t, err)
		require.Len(t, again.Lines, 2)
		assert.Equal(t, extra.ID, again.Lines[1].ID)
		assert.Nil(t, again.Lines[1].SupplierID)
		assert.Equal(t, domain.GoodsReceiptCompleted, again.Status)

		page, total, err := f.repo.ListGoodsReceipts(ctx, f.garage, usecase.GoodsReceiptFilter{Query: "CHANGED", Limit: 10})
		require.NoError(t, err)
		require.Equal(t, 1, total)
		assert.Equal(t, int64(1_500_005), page[0].TotalAmount, "10 x 150000 + 1 x 5")

		for name, filter := range map[string]usecase.GoodsReceiptFilter{
			"supplier": {SupplierID: f.supplier, Limit: 10}, "status": {Status: domain.GoodsReceiptCompleted, Limit: 10},
			"number": {Query: "pn-1", Limit: 10}, "delivery note": {Query: "dn-1", Limit: 10},
		} {
			_, total, err := f.repo.ListGoodsReceipts(ctx, f.garage, filter)
			require.NoError(t, err, name)
			assert.Equal(t, 1, total, name)
		}

		for name, filter := range map[string]usecase.GoodsReceiptFilter{
			"other supplier": {SupplierID: f.sup2, Limit: 10}, "draft": {Status: domain.GoodsReceiptDraft, Limit: 10},
			"nothing": {Query: "zzz", Limit: 10},
		} {
			_, total, err := f.repo.ListGoodsReceipts(ctx, f.garage, filter)
			require.NoError(t, err, name)
			assert.Zero(t, total, name)
		}

		_, err = f.repo.FindGoodsReceipt(ctx, f.garage, uuid.NewString(), false)
		require.ErrorIs(t, err, domain.ErrGoodsReceiptNotFound)
	})

	f.in(f.other, func(ctx context.Context) {
		_, err := f.repo.FindGoodsReceipt(ctx, f.other, receipt.ID, false)
		require.ErrorIs(t, err, domain.ErrGoodsReceiptNotFound, "another garage cannot read it")
	})
}

func TestInventoryRepository_PartSuppliers(t *testing.T) {
	f := newFixture(t)

	f.in(f.garage, func(ctx context.Context) {
		list, err := f.repo.PartSuppliers(ctx, f.garage, f.part)
		require.NoError(t, err)
		assert.Empty(t, list)

		require.NoError(t, f.repo.ReplacePartSuppliers(ctx, f.garage, f.part, []domain.PartSupplier{
			{SupplierID: f.sup2, SupplierSKU: "B-1"}, {SupplierID: f.supplier, SupplierSKU: "A-1", IsDefault: true},
		}))
		require.NoError(t, f.repo.RecordLastCost(ctx, f.garage, f.part, f.supplier, "6250.5"))
		require.NoError(t, f.repo.RecordLastCost(ctx, f.garage, f.part, f.supplier, "7000"))

		list, err = f.repo.PartSuppliers(ctx, f.garage, f.part)
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, f.supplier, list[0].SupplierID, "the default comes first")
		assert.Equal(t, "7000", *list[0].LastUnitCost)
		assert.Nil(t, list[1].LastUnitCost)

		defaults, err := f.repo.DefaultSuppliers(ctx, f.garage, []string{f.part, f.part2})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{f.part: f.supplier}, defaults)

		// Replacing keeps the last cost of a supplier that stays, and adds a cost-only supplier.
		require.NoError(t, f.repo.ReplacePartSuppliers(ctx, f.garage, f.part, []domain.PartSupplier{{SupplierID: f.supplier}}))
		list, err = f.repo.PartSuppliers(ctx, f.garage, f.part)
		require.NoError(t, err)
		require.Len(t, list, 1)
		assert.Equal(t, "7000", *list[0].LastUnitCost)
		assert.False(t, list[0].IsDefault)

		require.NoError(t, f.repo.RecordLastCost(ctx, f.garage, f.part2, f.sup2, "10"))
		list, err = f.repo.PartSuppliers(ctx, f.garage, f.part2)
		require.NoError(t, err)
		require.Len(t, list, 1, "completing a receipt adds the supplier to the part")

		used, err := f.repo.SupplierReferenced(ctx, f.garage, f.sup2)
		require.NoError(t, err)
		assert.True(t, used)

		unused, err := f.repo.SupplierReferenced(ctx, f.garage, uuid.NewString())
		require.NoError(t, err)
		assert.False(t, unused)
	})
}

func TestInventoryRepository_StockIssue(t *testing.T) {
	f := newFixture(t)
	repairOrder := uuid.NewString()
	issue := &domain.StockIssue{
		ID: uuid.NewString(), GarageID: f.garage, Number: "PX-1", RepairOrderID: repairOrder, RepairOrderNumber: "LSC-1",
		Status: domain.StockIssueDraft,
		Lines: []domain.StockIssueLine{
			{ID: uuid.NewString(), RepairOrderLineID: uuid.NewString(), PartID: f.part, EnteredQuantity: "2", Quantity: "2"},
			{ID: uuid.NewString(), RepairOrderLineID: uuid.NewString(), PartID: f.part, EnteredQuantity: "1", Quantity: "1"},
		},
	}

	var posted *domain.StockTransaction

	f.in(f.garage, func(ctx context.Context) {
		f.record(ctx, f.part, domain.TransactionIn, domain.RefGoodsReceipt, "10", new("100"))

		created, err := f.repo.CreateStockIssue(ctx, issue)
		require.NoError(t, err)
		require.True(t, created)

		again := *issue
		again.ID, again.Number = uuid.NewString(), "PX-2"
		created, err = f.repo.CreateStockIssue(ctx, &again)
		require.NoError(t, err)
		assert.False(t, created, "an order has one stock issue")

		got, err := f.repo.FindStockIssueByRepairOrder(ctx, f.garage, repairOrder, true)
		require.NoError(t, err)
		assert.Equal(t, "PX-1", got.Number)
		require.Len(t, got.Lines, 2)

		b := balance(t, ctx, f, f.part)
		assert.Equal(t, usecase.Balance{OnHand: "10", Reserved: "3", AverageCost: "100"}, b, "the draft reserves; on hand does not move")

		all, err := f.repo.Balances(ctx, f.garage, nil)
		require.NoError(t, err)
		assert.Len(t, all, 1)

		// A reservation of a part without a balance row still shows.
		reserved := &domain.StockIssue{
			ID: uuid.NewString(), GarageID: f.garage, Number: "PX-3", RepairOrderID: uuid.NewString(), Status: domain.StockIssueDraft,
			Lines: []domain.StockIssueLine{{ID: uuid.NewString(), RepairOrderLineID: uuid.NewString(), PartID: f.part2, EnteredQuantity: "4", Quantity: "4"}},
		}
		_, err = f.repo.CreateStockIssue(ctx, reserved)
		require.NoError(t, err)

		got2, err := f.repo.Balances(ctx, f.garage, []string{f.part2})
		require.NoError(t, err)
		assert.Equal(t, usecase.Balance{OnHand: "0", Reserved: "4", AverageCost: "0"}, got2[f.part2])

		// Posting releases the reservation and stamps the cost.
		tx := f.record(ctx, f.part, domain.TransactionOut, domain.RefStockIssue, "3", nil)
		got.Status, got.PostedAt, got.PostedBy = domain.StockIssuePosted, new(time.Now()), new(uuid.NewString())

		for i := range got.Lines {
			got.Lines[i].UnitCost = tx.UnitCost
		}

		require.NoError(t, f.repo.SaveStockIssue(ctx, got))

		saved, err := f.repo.FindStockIssue(ctx, f.garage, issue.ID, false)
		require.NoError(t, err)
		assert.Equal(t, domain.StockIssuePosted, saved.Status)
		assert.Equal(t, "100", *saved.Lines[0].UnitCost)
		assert.Equal(t, usecase.Balance{OnHand: "7", Reserved: "0", AverageCost: "100"}, balance(t, ctx, f, f.part))

		_, err = f.repo.FindStockIssue(ctx, f.garage, uuid.NewString(), false)
		require.ErrorIs(t, err, domain.ErrStockIssueNotFound)

		posted = tx
	})

	// A part's movement is written once per stock issue: the database refuses a duplicate.
	dup := &domain.StockTransaction{
		ID: uuid.NewString(), GarageID: f.garage, PartID: f.part, Type: domain.TransactionOut, Quantity: "1",
		RefType: domain.RefStockIssue, RefID: posted.RefID,
	}
	err := f.txm.WithTransaction(t.Context(), dbpkg.GarageScope(f.garage), func(ctx context.Context) error {
		return f.repo.RecordTransaction(ctx, dup)
	})
	require.Error(t, err)
}

func TestInventoryRepository_Reports(t *testing.T) {
	f := newFixture(t)
	receipt := &domain.GoodsReceipt{
		ID: uuid.NewString(), GarageID: f.garage, Number: "PN-9", Status: domain.GoodsReceiptCompleted, ReceiptDate: "2026-10-10",
		Lines: []domain.GoodsReceiptLine{
			{ID: uuid.NewString(), PartID: f.part, EnteredQuantity: "10", Quantity: "240", SupplierID: &f.supplier, UnitCost: 6250, UnitID: &f.unit},
			{ID: uuid.NewString(), PartID: f.part2, EnteredQuantity: "2", Quantity: "2", SupplierID: &f.sup2, UnitCost: 40000},
			{ID: uuid.NewString(), PartID: f.part2, EnteredQuantity: "1", Quantity: "1", UnitCost: 1},
		},
	}
	draft := &domain.GoodsReceipt{
		ID: uuid.NewString(), GarageID: f.garage, Number: "PN-10", Status: domain.GoodsReceiptDraft, ReceiptDate: "2026-10-10",
		Lines: []domain.GoodsReceiptLine{{ID: uuid.NewString(), PartID: f.part, EnteredQuantity: "1", Quantity: "1", SupplierID: &f.supplier, UnitCost: 1}},
	}

	f.in(f.garage, func(ctx context.Context) {
		require.NoError(t, f.repo.CreateGoodsReceipt(ctx, receipt))
		require.NoError(t, f.repo.CreateGoodsReceipt(ctx, draft))

		all, err := f.repo.PurchasesReport(ctx, f.garage, usecase.PurchaseFilter{})
		require.NoError(t, err)
		require.Len(t, all, 2, "only completed receipts, and only lines with a supplier")
		assert.Equal(t, "PN-9", all[0].GoodsReceiptNumber)
		assert.Equal(t, "240", all[0].Quantity)
		assert.Equal(t, int64(260), all[0].UnitCost, "62500 per case of 240 is 260.4 per can, rounded to whole dong")
		assert.Equal(t, int64(62500), all[0].Amount)

		bySupplier, err := f.repo.PurchasesReport(ctx, f.garage, usecase.PurchaseFilter{SupplierID: f.sup2})
		require.NoError(t, err)
		require.Len(t, bySupplier, 1)
		assert.Equal(t, int64(80000), bySupplier[0].Amount)

		for name, filter := range map[string]usecase.PurchaseFilter{
			"from after": {From: "2026-10-11"}, "to before": {To: "2026-10-10"},
		} {
			rows, err := f.repo.PurchasesReport(ctx, f.garage, filter)
			require.NoError(t, err, name)
			assert.Empty(t, rows, name)
		}

		inRange, err := f.repo.PurchasesReport(ctx, f.garage, usecase.PurchaseFilter{From: "2026-10-10", To: "2026-10-11"})
		require.NoError(t, err)
		assert.Len(t, inRange, 2, "from is inclusive, to exclusive")

		// Valuation after the last movement before a bound.
		f.record(ctx, f.part, domain.TransactionIn, domain.RefGoodsReceipt, "240", new("260.4167"))
		f.record(ctx, f.part, domain.TransactionOut, domain.RefStockIssue, "40", nil)
		f.record(ctx, f.part2, domain.TransactionIn, domain.RefGoodsReceipt, "2", new("20000"))

		rows, err := f.repo.StockValuation(ctx, f.garage, time.Now().Add(time.Hour))
		require.NoError(t, err)

		got := map[string]usecase.ValuationRow{}

		for _, r := range rows {
			got[r.PartID] = r
		}

		assert.Equal(t, usecase.ValuationRow{PartID: f.part, Quantity: "200", AverageCost: "260.4167"}, got[f.part], "the last movement of the part wins")
		assert.Equal(t, "2", got[f.part2].Quantity)

		before, err := f.repo.StockValuation(ctx, f.garage, time.Now().Add(-time.Hour))
		require.NoError(t, err)
		assert.Empty(t, before, "nothing had moved an hour ago")
	})
}
