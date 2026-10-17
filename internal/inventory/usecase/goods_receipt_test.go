package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	receiptID   = "88888888-8888-8888-8888-888888888888"
	supplierA   = "99999999-9999-9999-9999-999999999991"
	supplierB   = "99999999-9999-9999-9999-999999999992"
	otherPartID = "22222222-2222-2222-2222-222222222223"
)

func draftReceipt(lines ...domain.GoodsReceiptLine) *domain.GoodsReceipt {
	return &domain.GoodsReceipt{
		ID: receiptID, GarageID: garageID, Number: "PN-202610-0001", Status: domain.GoodsReceiptDraft, ReceiptDate: "2026-10-15", Lines: lines,
	}
}

func receiptLine(part, supplier, quantity string, cost int64) domain.GoodsReceiptLine {
	l := domain.GoodsReceiptLine{ID: "line-" + part, PartID: part, EnteredQuantity: quantity, Quantity: quantity, UnitCost: cost}
	if supplier != "" {
		l.SupplierID = &supplier
	}

	return l
}

func supplierNames(ids ...string) map[string]string {
	names := map[string]string{}
	for _, id := range ids {
		names[id] = "Supplier " + id[len(id)-1:]
	}

	return names
}

// expectDescribe stubs the lookups that fill the derived names of a receipt
// of partID and the given suppliers.
func expectDescribe(d *deps, suppliers ...string) {
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(suppliers...), nil).Maybe()
}

//nolint:funlen // one table of receipt cases
func TestInventoryUseCase_CreateGoodsReceipt(t *testing.T) {
	t.Parallel()

	newReq := func() usecase.CreateGoodsReceiptRequest {
		return usecase.CreateGoodsReceiptRequest{
			GarageID: garageID, ActorID: actorID, CanSetCost: true, DefaultSupplierID: supplierA,
			DeliveryNoteNumber: "DN-9", Notes: "morning delivery",
			Lines: []usecase.ReceiptLineInput{
				{PartID: partID, EnteredQuantity: "10", UnitCost: 150000},
				{PartID: otherPartID, EnteredQuantity: "2", SupplierID: supplierB, UnitCost: 5000},
			},
		}
	}

	setup := func(t *testing.T) (*usecase.InventoryUseCase, *deps) {
		t.Helper()

		uc, d := newUseCase(t)
		other := samplePart()
		other.ID = otherPartID

		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", "PN-202610-{SEQ:4}").Return(1, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID, otherPartID}).
			Return([]usecase.PartInfo{samplePart(), other}, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, []string{partID, otherPartID}).
			Return(map[string]string{otherPartID: supplierA}, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "10").Return(usecase.ResolvedQuantity{Base: "10"}, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, otherPartID, "", "2").Return(usecase.ResolvedQuantity{Base: "2"}, nil).Once()

		return uc, d
	}

	t.Run("saves a numbered draft, each line with its own supplier", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA, supplierB), nil).Maybe()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Number == "PN-202610-0001" && r.Status == domain.GoodsReceiptDraft && r.ReceiptDate == "2026-10-15" &&
				*r.DefaultSupplierID == supplierA && len(r.Lines) == 2 &&
				*r.Lines[0].SupplierID == supplierA && *r.Lines[1].SupplierID == supplierB &&
				r.Lines[0].UnitCost == 150000 && r.Lines[0].Quantity == "10"
		})).Return(nil).Once()

		got, err := uc.CreateGoodsReceipt(t.Context(), newReq())
		require.NoError(t, err)
		assert.Equal(t, int64(1_510_000), got.TotalAmount)
		assert.Equal(t, "Supplier 1", got.DefaultSupplierName)
	})

	t.Run("a line without a supplier inherits the part's default supplier when the receipt has none", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		expectDescribe(d, supplierA, supplierB)
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.DefaultSupplierID == nil && r.Lines[0].SupplierID == nil && *r.Lines[1].SupplierID == supplierB
		})).Return(nil).Once()

		req := newReq()
		req.DefaultSupplierID = ""

		_, err := uc.CreateGoodsReceipt(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("a caller without cost permission cannot set costs", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		expectDescribe(d, supplierA, supplierB)
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Lines[0].UnitCost == 0 && r.Lines[1].UnitCost == 0
		})).Return(nil).Once()

		req := newReq()
		req.CanSetCost = false

		_, err := uc.CreateGoodsReceipt(t.Context(), req)
		require.NoError(t, err)
	})

	t.Run("an unknown supplier is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := setup(t)
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Once()

		_, err := uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("rejections", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Times(3)
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(1, nil).Twice()

		bad := newReq()
		bad.ReceiptDate = "15/10/2026"
		_, err := uc.CreateGoodsReceipt(t.Context(), bad)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID, otherPartID}).Return(nil, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()

		_, err = uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeNotFound)

		inactive := samplePart()
		inactive.IsActive = false
		other := inactive
		other.ID = otherPartID

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID, otherPartID}).Return([]usecase.PartInfo{inactive, other}, nil).Once()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()

		_, err = uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("storage and numbering failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()

		_, err := uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeInternal)

		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "goods_receipt", mock.Anything).Return(0, errBoom).Once()

		_, err = uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeInternal)

		uc, d = setup(t)
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA, supplierB), nil).Once()
		d.Repo.EXPECT().CreateGoodsReceipt(mock.Anything, mock.Anything).Return(errBoom).Once()
		_, err = uc.CreateGoodsReceipt(t.Context(), newReq())
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_UpdateGoodsReceipt(t *testing.T) {
	t.Parallel()

	t.Run("replaces the lines, keeping the stored cost for a caller without cost permission", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		stored := draftReceipt(receiptLine(partID, supplierA, "10", 150000))
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(stored, nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Twice()
		d.Repo.EXPECT().DefaultSuppliers(mock.Anything, garageID, mock.Anything).Return(nil, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, "", "12").Return(usecase.ResolvedQuantity{Base: "12"}, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Twice()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return len(r.Lines) == 1 && r.Lines[0].Quantity == "12" && r.Lines[0].UnitCost == 150000 && r.Notes == "changed"
		})).Return(nil).Once()

		lines := []usecase.ReceiptLineInput{{ID: "line-" + partID, PartID: partID, EnteredQuantity: "12", SupplierID: supplierA}}

		got, err := uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{
			GarageID: garageID, ReceiptID: receiptID, ActorID: actorID, Notes: new("changed"), Lines: &lines,
		})
		require.NoError(t, err)
		assert.Equal(t, "line-"+partID, got.Lines[0].ID, "the line keeps its id")
	})

	t.Run("sets and clears the default supplier and changes the header", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		stored := draftReceipt()
		stored.DefaultSupplierID = new(supplierA)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(stored, nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.DefaultSupplierID == nil && r.ReceiptDate == "2026-10-01" && r.DeliveryNoteNumber == "DN-1"
		})).Return(nil).Once()

		_, err := uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{
			GarageID: garageID, ReceiptID: receiptID, SetDefaultSupplier: true,
			ReceiptDate: new("2026-10-01"), DeliveryNoteNumber: new("DN-1"),
		})
		require.NoError(t, err)
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		completed := draftReceipt()
		completed.Status = domain.GoodsReceiptCompleted
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(completed, nil).Once()

		_, err := uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{GarageID: garageID, ReceiptID: receiptID})
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assert.Equal(t, usecase.ReasonGoodsReceiptNotDraft, apperror.ReasonOf(err))

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(nil, domain.ErrGoodsReceiptNotFound).Once()
		_, err = uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{GarageID: garageID, ReceiptID: receiptID})
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(draftReceipt(), nil).Once()
		_, err = uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{GarageID: garageID, ReceiptID: receiptID, ReceiptDate: new("soon")})
		assertAppError(t, err, apperror.CodeInvalidArgument)

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(draftReceipt(), nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.UpdateGoodsReceipt(t.Context(), usecase.UpdateGoodsReceiptRequest{GarageID: garageID, ReceiptID: receiptID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_GetAndListGoodsReceipts(t *testing.T) {
	t.Parallel()

	t.Run("get describes the lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).
			Return(draftReceipt(receiptLine(partID, supplierA, "3", 1000)), nil).Once()
		expectDescribe(d, supplierA)

		got, err := uc.GetGoodsReceipt(t.Context(), garageID, receiptID)
		require.NoError(t, err)
		assert.Equal(t, "BEER-330", got.Lines[0].PartSKU)
		assert.Equal(t, "Supplier 1", got.Lines[0].SupplierName)
		assert.Equal(t, int64(3000), got.TotalAmount)
	})

	t.Run("get failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, false).Return(nil, domain.ErrGoodsReceiptNotFound).Once()

		_, err := uc.GetGoodsReceipt(t.Context(), garageID, receiptID)
		assertAppError(t, err, apperror.CodeNotFound)
	})

	t.Run("list pages without lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		r := draftReceipt()
		r.DefaultSupplierID, r.TotalAmount = new(supplierA), 42
		d.Repo.EXPECT().ListGoodsReceipts(mock.Anything, garageID, usecase.GoodsReceiptFilter{
			Query: "pn", SupplierID: supplierA, Status: domain.GoodsReceiptDraft, Limit: 20,
		}).Return([]*domain.GoodsReceipt{r}, 1, nil).Once()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, []string{supplierA}).Return(supplierNames(supplierA), nil).Once()

		page, err := uc.ListGoodsReceipts(t.Context(), usecase.ListGoodsReceiptsRequest{
			GarageID: garageID, Query: "pn", SupplierID: supplierA, Status: domain.GoodsReceiptDraft,
		})
		require.NoError(t, err)
		assert.Equal(t, 1, page.Total)
		assert.Equal(t, "Supplier 1", page.Items[0].DefaultSupplierName)
		assert.Equal(t, int64(42), page.Items[0].TotalAmount, "the repository's total is kept for a list")
	})

	t.Run("list failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().ListGoodsReceipts(mock.Anything, garageID, mock.Anything).Return(nil, 0, errBoom).Once()

		_, err := uc.ListGoodsReceipts(t.Context(), usecase.ListGoodsReceiptsRequest{GarageID: garageID})
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_CompleteGoodsReceipt(t *testing.T) {
	t.Parallel()

	t.Run("posts one stamped movement per line and the supplier's last cost", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		r := draftReceipt(receiptLine(partID, supplierA, "10", 150000), receiptLine(otherPartID, supplierB, "2", 0))
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(r, nil).Once()

		other := samplePart()
		other.ID = otherPartID
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart(), other}, nil).Maybe()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA, supplierB), nil).Maybe()

		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.PartID == partID && tx.Type == domain.TransactionIn && tx.Quantity == "10" && *tx.UnitCost == "150000" &&
				tx.RefType == domain.RefGoodsReceipt && *tx.RefID == receiptID && *tx.SupplierID == supplierA
		})).Return(nil).Once()
		d.Repo.EXPECT().RecordLastCost(mock.Anything, garageID, partID, supplierA, "150000").Return(nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.PartID == otherPartID && tx.UnitCost == nil && *tx.SupplierID == supplierB
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.MatchedBy(func(g *domain.GoodsReceipt) bool {
			return g.Status == domain.GoodsReceiptCompleted && g.CompletedAt != nil
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		got, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.GoodsReceiptCompleted, got.Status)
	})

	t.Run("converts the cost typed per packaging unit to the base unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		line := receiptLine(partID, supplierA, "10", 150000)
		line.Quantity, line.UnitID = "240", new(unitID)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(draftReceipt(line), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Parts.EXPECT().UnitAbbreviations(mock.Anything, garageID, []string{unitID}).Return(map[string]string{unitID: "case"}, nil).Maybe()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Maybe()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Quantity == "240" && *tx.UnitCost == "6250" && *tx.EnteredQuantity == "10" && *tx.UnitID == unitID
		})).Return(nil).Once()
		d.Repo.EXPECT().RecordLastCost(mock.Anything, garageID, partID, supplierA, "6250").Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		_, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
	})

	t.Run("a line without a supplier blocks completion", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).
			Return(draftReceipt(receiptLine(partID, supplierA, "1", 1), receiptLine(otherPartID, "", "1", 1)), nil).Once()

		_, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assert.Equal(t, usecase.ReasonSupplierRequired, apperror.ReasonOf(err))
	})

	t.Run("refusals and failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(nil, domain.ErrGoodsReceiptNotFound).Once()
		_, err := uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		done := draftReceipt(receiptLine(partID, supplierA, "1", 1))
		done.Status = domain.GoodsReceiptCompleted
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(done, nil).Once()
		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assert.Equal(t, usecase.ReasonGoodsReceiptNotDraft, apperror.ReasonOf(err))

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(draftReceipt(), nil).Once()
		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInvalidArgument)

		one := draftReceipt(receiptLine(partID, supplierA, "1", 100))
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(one, nil).Times(4)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()
		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		inactive := samplePart()
		inactive.IsActive = false
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{inactive}, nil).Once()
		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Twice()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().RecordLastCost(mock.Anything, garageID, partID, supplierA, "100").Return(errBoom).Once()
		_, err = uc.CompleteGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestInventoryUseCase_CancelGoodsReceipt(t *testing.T) {
	t.Parallel()

	completed := func() *domain.GoodsReceipt {
		r := draftReceipt(receiptLine(partID, supplierA, "10", 150000))
		r.Status = domain.GoodsReceiptCompleted

		return r
	}

	t.Run("a draft is just closed", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(draftReceipt(), nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.MatchedBy(func(r *domain.GoodsReceipt) bool {
			return r.Status == domain.GoodsReceiptCancelled && r.CancelledAt != nil
		})).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

		got, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.GoodsReceiptCancelled, got.Status)
	})

	t.Run("a completed receipt is reversed with the cost unwound", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(completed(), nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "10"}}, nil).Once()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
			return tx.Type == domain.TransactionOut && tx.Quantity == "10" && *tx.UnitCost == "150000" &&
				tx.RefType == domain.RefGoodsReceipt && *tx.RefID == receiptID
		})).Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(nil).Once()
		d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
		d.Directory.EXPECT().SupplierNames(mock.Anything, garageID, mock.Anything).Return(supplierNames(supplierA), nil).Maybe()

		got, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		require.NoError(t, err)
		assert.Equal(t, domain.GoodsReceiptCancelled, got.Status)
	})

	t.Run("refused when the stock was already used", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(completed(), nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, []string{partID}).
			Return(map[string]usecase.Balance{partID: {OnHand: "9.999"}}, nil).Once()

		_, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assert.Equal(t, usecase.ReasonInsufficientStockToReverse, apperror.ReasonOf(err))
	})

	t.Run("a cancelled receipt cannot be cancelled again", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		done := draftReceipt()
		done.Status = domain.GoodsReceiptCancelled
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(done, nil).Once()

		_, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assert.Equal(t, usecase.ReasonGoodsReceiptAlreadyCancelled, apperror.ReasonOf(err))
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(nil, domain.ErrGoodsReceiptNotFound).Once()
		_, err := uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeNotFound)

		d.Repo.EXPECT().FindGoodsReceipt(mock.Anything, garageID, receiptID, true).Return(completed(), nil).Times(3)
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()
		_, err = uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(map[string]usecase.Balance{partID: {OnHand: "10"}}, nil).Twice()
		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInternal)

		d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().SaveGoodsReceipt(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.CancelGoodsReceipt(t.Context(), garageID, receiptID, actorID)
		assertAppError(t, err, apperror.CodeInternal)
	})
}
