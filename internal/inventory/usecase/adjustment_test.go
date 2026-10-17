package usecase_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	writeOffID = "d0d0d0d0-0000-0000-0000-000000000001"
	returnID   = "e0e0e0e0-0000-0000-0000-000000000001"
)

// expectStock stubs the lock and the balance read of an adjustment.
func expectStock(d *deps, onHand, cost string) {
	d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(nil).Once()
	d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).
		Return(map[string]usecase.Balance{partID: {OnHand: onHand, AverageCost: cost}}, nil).Once()
}

// expectOut stubs an OUT movement that the repository stamps with cost.
func expectOut(d *deps, ref domain.RefType, quantity, cost string) {
	d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
		return tx.Type == domain.TransactionOut && tx.RefType == ref && tx.Quantity == quantity
	})).Run(func(_ context.Context, tx *domain.StockTransaction) { tx.UnitCost = &cost }).Return(nil).Once()
}

func resolved(d *deps, part, unit, entered, base string) {
	r := usecase.ResolvedQuantity{Base: base}
	if unit != "" {
		r.UnitID, r.Entered = new(unit), new(entered)
	}

	d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, part, unit, entered).Return(r, nil).Once()
}

func writeOffOf(status domain.AdjustmentStatus, lines ...domain.AdjustmentLine) *domain.WriteOff {
	return &domain.WriteOff{
		ID: writeOffID, GarageID: garageID, Number: "XH-202610-0001", Status: status, Reason: domain.WriteOffDamaged, Lines: lines,
		CreatedBy: new(actorID),
	}
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_CreateWriteOff(t *testing.T) {
	t.Parallel()

	req := usecase.CreateWriteOffRequest{
		GarageID: garageID, ActorID: actorID, Reason: domain.WriteOffDamaged, Notes: "dropped",
		Lines: []usecase.AdjustmentLineInput{{PartID: partID, EnteredQuantity: "3"}, {PartID: partID, UnitID: unitID, EnteredQuantity: "1"}},
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, mock.Anything).Return(map[string]string{unitID: "ctn"}, nil).Maybe()
		d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("Lan Nguyen", nil).Maybe()

		return uc, d
	}

	t.Run("posts one movement per line at average cost", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		resolved(d, partID, "", "3", "3")
		resolved(d, partID, unitID, "1", "12")
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "write_off", "XH-202610-{SEQ:4}").Return(4, nil).Once()
		expectStock(d, "15", "50")
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.RefType == domain.RefWriteOff && tx.Reason == "damaged" && tx.Type == domain.TransactionOut
		})).Run(func(_ context.Context, tx *domain.StockTransaction) { tx.UnitCost = new("50") }).Return(nil).Twice()
		d.Repo.EXPECT().CreateWriteOff(mock.Anything, mock.MatchedBy(func(w *domain.WriteOff) bool {
			return w.Number == "XH-202610-0004" && w.Status == domain.AdjustmentCompleted && *w.Lines[1].UnitID == unitID
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		w, err := uc.CreateWriteOff(t.Context(), req)
		require.NoError(t, err)
		assert.EqualValues(t, 3*50+12*50, w.TotalValue)
		assert.Equal(t, "Lan Nguyen", w.CreatedByName)
		assert.Equal(t, "ctn", w.Lines[1].UnitAbbreviation)
	})

	t.Run("refuses more than is on hand, several lines of a part adding up", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		resolved(d, partID, "", "3", "3")
		resolved(d, partID, unitID, "1", "12")
		expectStock(d, "14", "50")

		_, err := uc.CreateWriteOff(t.Context(), req)
		assert.Equal(t, usecase.ReasonInsufficientStock, mustReason(t, err))
	})

	t.Run("refuses a bad reason, no lines, an unknown part and a bad cost", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		bad := req
		bad.Reason = "stolen"
		_, err := uc.CreateWriteOff(t.Context(), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Times(3)

		none := req
		none.Lines = nil
		_, err = uc.CreateWriteOff(t.Context(), none)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()

		_, err = uc.CreateWriteOff(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		resolved(d, partID, "", "3", "3")
		negative := usecase.CreateWriteOffRequest{
			GarageID: garageID, Reason: domain.WriteOffLost, Lines: []usecase.AdjustmentLineInput{{PartID: partID, EnteredQuantity: "3", UnitCost: -1}},
		}
		_, err = uc.CreateWriteOff(t.Context(), negative)
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("reports failing collaborators", func(t *testing.T) {
		t.Parallel()

		for step := range 9 {
			uc, d := setup(t)
			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).
				Return(usecase.ResolvedQuantity{Base: "1"}, fail(0)).Maybe()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "write_off", mock.Anything).Return(1, fail(1)).Maybe()
			d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(fail(2)).Maybe()
			d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).
				Return(map[string]usecase.Balance{partID: {OnHand: "100"}}, fail(3)).Maybe()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(4)).Maybe()
			d.Repo.EXPECT().CreateWriteOff(mock.Anything, mock.Anything).Return(fail(5)).Maybe()
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(fail(6)).Maybe()

			one := usecase.CreateWriteOffRequest{
				GarageID: garageID, ActorID: actorID, Reason: domain.WriteOffLost,
				Lines: []usecase.AdjustmentLineInput{{PartID: partID, EnteredQuantity: "1"}},
			}

			if step == 7 {
				d.Garages.ExpectedCalls = nil
				d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
			}

			if step == 8 {
				d.Directory.ExpectedCalls = nil
				d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", errBoom).Once()
			}

			_, err := uc.CreateWriteOff(t.Context(), one)
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}

func TestInventoryUseCase_WriteOffReadsAndCancel(t *testing.T) {
	t.Parallel()

	line := func() domain.AdjustmentLine {
		return domain.AdjustmentLine{ID: "l1", PartID: partID, Quantity: "3", EnteredQuantity: "3", AverageCost: new("50")}
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		expectCountNames(d)

		return uc, d
	}

	t.Run("get and list", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, false).Return(writeOffOf(domain.AdjustmentCompleted, line()), nil).Once()

		w, err := uc.GetWriteOff(t.Context(), garageID, writeOffID)
		require.NoError(t, err)
		assert.EqualValues(t, 150, w.TotalValue)
		assert.Equal(t, "BEER-330", w.Lines[0].PartSKU)

		d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, false).Return(nil, domain.ErrWriteOffNotFound).Once()
		_, err = uc.GetWriteOff(t.Context(), garageID, writeOffID)
		assertAppError(t, err, apperror.CodeNotFound)

		listed := writeOffOf(domain.AdjustmentCompleted)
		listed.TotalValue = 99
		d.Repo.EXPECT().ListWriteOffs(mock.Anything, garageID, usecase.WriteOffFilter{
			Query: "x", Reason: domain.WriteOffLost, Status: domain.AdjustmentCompleted, Limit: 20,
		}).Return([]*domain.WriteOff{listed}, 1, nil).Once()

		page, err := uc.ListWriteOffs(t.Context(), usecase.ListWriteOffsRequest{
			GarageID: garageID, Query: "x", Reason: domain.WriteOffLost, Status: domain.AdjustmentCompleted,
		})
		require.NoError(t, err)
		assert.EqualValues(t, 99, page.Items[0].TotalValue)
		assert.Equal(t, "Lan Nguyen", page.Items[0].CreatedByName)

		d.Repo.EXPECT().ListWriteOffs(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		_, err = uc.ListWriteOffs(t.Context(), usecase.ListWriteOffsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("cancelling puts the parts back at the cost they left with", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, true).Return(writeOffOf(domain.AdjustmentCompleted, line()), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionIn && tx.RefType == domain.RefWriteOff && tx.Quantity == "3" && *tx.UnitCost == "50" &&
				tx.Reason == "damaged"
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveWriteOff(mock.Anything, mock.MatchedBy(func(w *domain.WriteOff) bool {
			return w.Status == domain.AdjustmentCancelled && w.CancelledAt != nil
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		w, err := uc.CancelWriteOff(t.Context(), garageID, writeOffID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.AdjustmentCancelled, w.Status)
	})

	t.Run("a cancelled write-off cannot be cancelled again; failures are reported", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, true).Return(writeOffOf(domain.AdjustmentCancelled), nil).Once()
		_, err := uc.CancelWriteOff(t.Context(), garageID, writeOffID, actorID)
		assert.Equal(t, usecase.ReasonWriteOffAlreadyCancelled, mustReason(t, err))

		d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, true).Return(nil, domain.ErrWriteOffNotFound).Once()
		_, err = uc.CancelWriteOff(t.Context(), garageID, writeOffID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		for step := range 4 {
			uc, d := setup(t)
			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().FindWriteOff(mock.Anything, garageID, writeOffID, true).Return(writeOffOf(domain.AdjustmentCompleted, line()), nil).Once()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(0)).Maybe()
			d.Repo.EXPECT().SaveWriteOff(mock.Anything, mock.Anything).Return(fail(1)).Maybe()
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(fail(2)).Maybe()

			if step == 3 {
				d.Parts.ExpectedCalls = nil
				d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
			}

			_, err := uc.CancelWriteOff(t.Context(), garageID, writeOffID, actorID)
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}

func supplierReturnOf(status domain.AdjustmentStatus, lines ...domain.AdjustmentLine) *domain.SupplierReturn {
	return &domain.SupplierReturn{
		ID: returnID, GarageID: garageID, Number: "TN-202610-0001", Status: status, SupplierID: supplierA, GoodsReceiptID: new(receiptID),
		Lines: lines, CreatedBy: new(actorID),
	}
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_CreateSupplierReturn(t *testing.T) {
	t.Parallel()

	req := func() usecase.CreateSupplierReturnRequest {
		return usecase.CreateSupplierReturnRequest{
			GarageID: garageID, ActorID: actorID, SupplierID: supplierA, GoodsReceiptID: receiptID, CreditNoteNumber: "CN-1",
			Lines: []usecase.AdjustmentLineInput{{PartID: partID, EnteredQuantity: "2", UnitCost: 999}},
		}
	}

	completedReceipt := func(lines ...domain.GoodsReceiptLine) *domain.GoodsReceipt {
		r := draftReceipt(lines...)
		r.Status = domain.GoodsReceiptCompleted

		return r
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		expectCountNames(d)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().GoodsReceiptNumbers(mock.Anything, garageID, []string{receiptID}).Return(map[string]string{receiptID: "PN-1"}, nil).Maybe()

		return uc, d
	}

	finish := func(d *deps, credit int64, caller bool) {
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "supplier_return", "TN-202610-{SEQ:4}").Return(2, nil).Once()
		expectOut(d, domain.RefSupplierReturn, "2", "50")
		d.Repo.EXPECT().CreateSupplierReturn(mock.Anything, mock.MatchedBy(func(s *domain.SupplierReturn) bool {
			return s.Number == "TN-202610-0002" && s.Lines[0].CreditUnitCost == credit && *s.Lines[0].AverageCost == "50" &&
				s.CreditNoteNumber == "CN-1"
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		_ = caller
	}

	t.Run("a typed credit wins for a caller with cost access", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
			Return(completedReceipt(receiptLine(partID, supplierA, "10", 100)), nil).Once()
		resolved(d, partID, "", "2", "2")
		expectStock(d, "10", "50")
		finish(d, 999, true)

		r := req()
		r.CanSetCost = true
		s, err := uc.CreateSupplierReturn(t.Context(), r)
		require.NoError(t, err)
		assert.EqualValues(t, 1998, s.CreditAmount)
		assert.Equal(t, "Supplier A", s.SupplierName)
		assert.Equal(t, "PN-1", s.GoodsReceiptNumber)
	})

	t.Run("the credit defaults to the receipt cost, converted to the entered unit", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
			Return(completedReceipt(
				receiptLine(otherPartID, supplierB, "1", 7), // another supplier's line is ignored
				domain.GoodsReceiptLine{ID: "x", PartID: partID, SupplierID: new(supplierA), EnteredQuantity: "1", Quantity: "12", UnitCost: 1200},
			), nil).Once()
		resolved(d, partID, unitID, "1", "12")
		expectStock(d, "50", "50")
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "supplier_return", mock.Anything).Return(2, nil).Once()
		expectOut(d, domain.RefSupplierReturn, "12", "50")
		d.Repo.EXPECT().CreateSupplierReturn(mock.Anything, mock.MatchedBy(func(s *domain.SupplierReturn) bool {
			return s.Lines[0].CreditUnitCost == 1200
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		r := req()
		r.Lines = []usecase.AdjustmentLineInput{{PartID: partID, UnitID: unitID, EnteredQuantity: "1", UnitCost: 5}}
		s, err := uc.CreateSupplierReturn(t.Context(), r)
		require.NoError(t, err)
		assert.EqualValues(t, 1200, s.CreditAmount, "a caller without cost access cannot set the credit")
	})

	t.Run("without a receipt the credit is the average cost", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		resolved(d, partID, "", "2", "2")
		expectStock(d, "10", "50")
		finish(d, 50, false)

		r := req()
		r.GoodsReceiptID = ""
		s, err := uc.CreateSupplierReturn(t.Context(), r)
		require.NoError(t, err)
		assert.EqualValues(t, 100, s.CreditAmount)
	})

	t.Run("refuses an unknown supplier, a receipt that is not usable and too little stock", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Times(5)

		bad := req()
		bad.SupplierID = ""
		_, err := uc.CreateSupplierReturn(t.Context(), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, []string{supplierA}).Return(map[string]string{}, nil).Once()

		_, err = uc.CreateSupplierReturn(t.Context(), req())
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, []string{supplierA}).Return(map[string]string{supplierA: "A"}, nil).Times(3)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).Return(draftReceipt(), nil).Once()

		_, err = uc.CreateSupplierReturn(t.Context(), req())
		assert.Equal(t, usecase.ReasonGoodsReceiptNotCompleted, mustReason(t, err))

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
			Return(completedReceipt(receiptLine(partID, supplierB, "1", 1)), nil).Once()

		_, err = uc.CreateSupplierReturn(t.Context(), req())
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
			Return(completedReceipt(receiptLine(partID, supplierA, "1", 1)), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		resolved(d, partID, "", "2", "2")
		expectStock(d, "1", "50")

		_, err = uc.CreateSupplierReturn(t.Context(), req())
		assert.Equal(t, usecase.ReasonInsufficientStock, mustReason(t, err))
	})

	t.Run("reports failing collaborators", func(t *testing.T) {
		t.Parallel()

		for step := range 9 {
			uc, d := newUseCase(t)
			expectCountNames(d)

			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), fail(0)).Maybe()
			d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
				Return(completedReceipt(domain.GoodsReceiptLine{ID: "x", PartID: partID, SupplierID: new(supplierA), EnteredQuantity: "0", Quantity: "0", UnitCost: 1}), fail(1)).Maybe()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).
				Return(usecase.ResolvedQuantity{Base: "2"}, fail(2)).Maybe()
			d.Repo.EXPECT().LockBalances(mock.Anything, garageID, mock.Anything).Return(fail(3)).Maybe()
			d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "9"}}, nil).Maybe()
			d.Sequences.EXPECT().Next(mock.Anything, garageID, "supplier_return", mock.Anything).Return(1, fail(4)).Maybe()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(5)).Maybe()
			d.Repo.EXPECT().CreateSupplierReturn(mock.Anything, mock.Anything).Return(fail(6)).Maybe()
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(fail(7)).Maybe()

			if step == 8 {
				d.Repo.ExpectedCalls = nil
				d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).Return(completedReceipt(), nil).Once()
			}

			_, err := uc.CreateSupplierReturn(t.Context(), req())
			require.Error(t, err)
		}
	})
}

func TestInventoryUseCase_SupplierReturnReadsAndCancel(t *testing.T) {
	t.Parallel()

	line := func() domain.AdjustmentLine {
		return domain.AdjustmentLine{ID: "l1", PartID: partID, Quantity: "2", EnteredQuantity: "2", AverageCost: new("50"), CreditUnitCost: 70}
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		expectCountNames(d)
		d.Repo.EXPECT().GoodsReceiptNumbers(mock.Anything, garageID, mock.Anything).Return(map[string]string{receiptID: "PN-1"}, nil).Maybe()

		return uc, d
	}

	t.Run("get and list", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, false).Return(supplierReturnOf(domain.AdjustmentCompleted, line()), nil).Once()

		s, err := uc.GetSupplierReturn(t.Context(), garageID, returnID)
		require.NoError(t, err)
		assert.EqualValues(t, 140, s.CreditAmount)

		d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, false).Return(nil, domain.ErrSupplierReturnNotFound).Once()
		_, err = uc.GetSupplierReturn(t.Context(), garageID, returnID)
		assertAppError(t, err, apperror.CodeNotFound)

		listed := supplierReturnOf(domain.AdjustmentCompleted)
		listed.CreditAmount = 7
		d.Repo.EXPECT().ListSupplierReturns(mock.Anything, garageID, usecase.SupplierReturnFilter{
			Query: "cn", SupplierID: supplierA, Status: domain.AdjustmentCompleted, Limit: 20,
		}).Return([]*domain.SupplierReturn{listed}, 1, nil).Once()

		page, err := uc.ListSupplierReturns(t.Context(), usecase.ListSupplierReturnsRequest{
			GarageID: garageID, Query: "cn", SupplierID: supplierA, Status: domain.AdjustmentCompleted,
		})
		require.NoError(t, err)
		assert.EqualValues(t, 7, page.Items[0].CreditAmount)
		assert.Equal(t, "Supplier A", page.Items[0].SupplierName)

		d.Repo.EXPECT().ListSupplierReturns(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()
		_, err = uc.ListSupplierReturns(t.Context(), usecase.ListSupplierReturnsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().ListSupplierReturns(mock.Anything, garageID, mock.Anything).Return([]*domain.SupplierReturn{listed}, 1, nil).Once()
		d.Repo.EXPECT().GoodsReceiptNumbers(mock.Anything, garageID, mock.Anything).Unset()
		d.Repo.EXPECT().GoodsReceiptNumbers(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.ListSupplierReturns(t.Context(), usecase.ListSupplierReturnsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("cancelling puts the parts back and withdraws the credit", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, true).Return(supplierReturnOf(domain.AdjustmentCompleted, line()), nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionIn && tx.RefType == domain.RefSupplierReturn && *tx.UnitCost == "50"
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveSupplierReturn(mock.Anything, mock.MatchedBy(func(s *domain.SupplierReturn) bool {
			return s.Status == domain.AdjustmentCancelled && s.CancelledAt != nil
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		s, err := uc.CancelSupplierReturn(t.Context(), garageID, returnID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.AdjustmentCancelled, s.Status)
	})

	t.Run("refuses a second cancel and reports failures", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, true).Return(supplierReturnOf(domain.AdjustmentCancelled), nil).Once()
		_, err := uc.CancelSupplierReturn(t.Context(), garageID, returnID, actorID)
		assert.Equal(t, usecase.ReasonSupplierReturnAlreadyCancelled, mustReason(t, err))

		d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, true).Return(nil, domain.ErrSupplierReturnNotFound).Once()
		_, err = uc.CancelSupplierReturn(t.Context(), garageID, returnID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		for step := range 3 {
			uc, d := setup(t)
			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().FindSupplierReturn(mock.Anything, garageID, returnID, true).Return(supplierReturnOf(domain.AdjustmentCompleted, line()), nil).Once()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(0)).Maybe()
			d.Repo.EXPECT().SaveSupplierReturn(mock.Anything, mock.Anything).Return(fail(1)).Maybe()
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(fail(2)).Maybe()

			_, err := uc.CancelSupplierReturn(t.Context(), garageID, returnID, actorID)
			assertAppError(t, err, apperror.CodeInternal)
		}
	})
}

func postedIssue(returned string) *domain.StockIssue {
	issue := draftIssue(domain.StockIssueLine{
		ID: "il1", RepairOrderLineID: roLineID, PartID: partID, Quantity: "4", EnteredQuantity: "4", UnitCost: new("50"),
		ReturnedQuantity: returned,
	})
	issue.Status = domain.StockIssuePosted

	return issue
}

//nolint:funlen // one table of cases
func TestInventoryUseCase_ReturnRepairOrderPart(t *testing.T) {
	t.Parallel()

	req := usecase.ReturnRepairOrderPartRequest{
		GarageID: garageID, RepairOrderID: repairOrderID, ActorID: actorID, RepairOrderLineID: roLineID, EnteredQuantity: "1", Reason: " not needed ",
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		expectCountNames(d)

		return uc, d
	}

	t.Run("restores the stock at the cost snapshot", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue("2"), nil).Once()
		resolved(d, partID, "", "1", "1")
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionIn && tx.RefType == domain.RefRepairOrderReturn && *tx.UnitCost == "50" &&
				tx.Reason == "not needed" && tx.Quantity == "1"
		})).Return(nil).Once()
		d.Repo.EXPECT().CreatePartReturn(mock.Anything, mock.MatchedBy(func(r *domain.RepairOrderPartReturn) bool {
			return r.StockIssueLineID == "il1" && r.RepairOrderLineID == roLineID && r.Reason == "not needed" && r.RepairOrderNumber == "LSC-1"
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		r, err := uc.ReturnRepairOrderPart(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "BEER-330", r.PartSKU)
		assert.Equal(t, "Lan Nguyen", r.CreatedByName)
	})

	t.Run("keeps the packaging unit it was typed in", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue(""), nil).Once()
		resolved(d, partID, unitID, "0.25", "3")
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return *tx.UnitID == unitID && *tx.EnteredQuantity == "0.25"
		})).Return(nil).Once()
		d.Repo.EXPECT().CreatePartReturn(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		in := req
		in.UnitID, in.EnteredQuantity = unitID, "0.25"
		r, err := uc.ReturnRepairOrderPart(t.Context(), in)
		require.NoError(t, err)
		assert.Equal(t, "ctn", r.UnitAbbreviation)
	})

	t.Run("refuses the refusable", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)

		blank := req
		blank.Reason = " "
		_, err := uc.ReturnRepairOrderPart(t.Context(), blank)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		blank.Reason = strings.Repeat("x", 501)
		_, err = uc.ReturnRepairOrderPart(t.Context(), blank)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(nil, domain.ErrStockIssueNotFound).Once()

		_, err = uc.ReturnRepairOrderPart(t.Context(), req)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(draftIssue(), nil).Once()

		_, err = uc.ReturnRepairOrderPart(t.Context(), req)
		assert.Equal(t, usecase.ReasonStockIssueNotPosted, mustReason(t, err))

		other := req
		other.RepairOrderLineID = "nope"

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue(""), nil).Once()

		_, err = uc.ReturnRepairOrderPart(t.Context(), other)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue("3.5"), nil).Once()
		resolved(d, partID, "", "1", "1")

		_, err = uc.ReturnRepairOrderPart(t.Context(), req)
		assert.Equal(t, usecase.ReasonReturnExceedsIssued, mustReason(t, err))
	})

	t.Run("reports failing collaborators", func(t *testing.T) {
		t.Parallel()

		for step := range 4 {
			uc, d := setup(t)
			fail := func(i int) error {
				if i == step {
					return errBoom
				}

				return nil
			}

			d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue(""), nil).Once()
			d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, mock.Anything, mock.Anything).
				Return(usecase.ResolvedQuantity{Base: "1"}, fail(0)).Maybe()
			d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(fail(1)).Maybe()
			d.Repo.EXPECT().CreatePartReturn(mock.Anything, mock.Anything).Return(fail(2)).Maybe()
			d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(fail(3)).Maybe()

			_, err := uc.ReturnRepairOrderPart(t.Context(), req)
			assertAppError(t, err, apperror.CodeInternal)
		}

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(postedIssue(""), nil).Once()
		resolved(d, partID, "", "1", "1")
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().CreatePartReturn(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		_, err := uc.ReturnRepairOrderPart(t.Context(), req)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_ListRepairOrderPartReturns(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	expectCountNames(d)
	d.Repo.EXPECT().ListPartReturns(mock.Anything, garageID, repairOrderID).Return([]domain.RepairOrderPartReturn{
		{ID: "r1", PartID: partID, UnitID: new(unitID), CreatedBy: new(actorID)}, {ID: "r2", PartID: partID},
	}, nil).Once()

	list, err := uc.ListRepairOrderPartReturns(t.Context(), garageID, repairOrderID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "ctn", list[0].UnitAbbreviation)
	assert.Equal(t, "Lan Nguyen", list[0].CreatedByName)
	assert.Equal(t, "BEER-330", list[1].PartSKU)

	d.Repo.EXPECT().ListPartReturns(mock.Anything, garageID, repairOrderID).Return(nil, errBoom).Once()
	_, err = uc.ListRepairOrderPartReturns(t.Context(), garageID, repairOrderID)
	assertAppError(t, err, apperror.CodeInternal)
}

func TestInventoryUseCase_ListTransactionsNamesTheUser(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, usecase.TransactionFilter{RefType: domain.RefWriteOff, Limit: 20}).
		Return([]*domain.StockTransaction{{ID: "t1", PartID: partID, CreatedBy: new(actorID)}, {ID: "t2", PartID: partID}}, 2, nil).Once()
	expectCountNames(d)

	page, err := uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID, RefType: domain.RefWriteOff})
	require.NoError(t, err)
	assert.Equal(t, "Lan Nguyen", page.Items[0].CreatedByName)
	assert.Empty(t, page.Items[1].CreatedByName)

	d.Repo.EXPECT().ListTransactions(mock.Anything, garageID, mock.Anything).Return([]*domain.StockTransaction{{ID: "t1", PartID: partID, CreatedBy: new(actorID)}}, 1, nil).Once()
	d.Directory.ExpectedCalls = nil
	d.Directory.EXPECT().UserName(mock.Anything, mock.Anything).Return("", errBoom).Once()

	_, err = uc.ListTransactions(t.Context(), usecase.ListTransactionsRequest{GarageID: garageID})
	assertAppError(t, err, apperror.CodeInternal)
}
