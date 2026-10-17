package usecase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

const (
	repairOrderID = "55555555-5555-5555-5555-555555555555"
	roLineID      = "66666666-6666-6666-6666-666666666666"
	issueID       = "77777777-7777-7777-7777-777777777777"
)

func orderPayload(partRef, quantity string) []byte {
	return []byte(`{"repair_order_id":"` + repairOrderID + `","repair_order_number":"LSC-1","part_lines":[` +
		`{"line_id":"` + roLineID + `","item_ref_id":"` + partRef + `","quantity":"` + quantity + `"}]}`)
}

func orderEvent(payload []byte) outbox.Event { return outbox.Event{GarageID: garageID, Data: payload} }

// expectNewIssue stubs the reads and writes that create a draft stock issue.
func expectNewIssue(d *deps) {
	d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).
		Return(nil, domain.ErrStockIssueNotFound).Once()
	d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_issue", "PX-202610-{SEQ:4}").Return(7, nil).Once()
}

func garageSettings(mode string) usecase.GarageSettings {
	return usecase.GarageSettings{
		Timezone: "Asia/Ho_Chi_Minh", GoodsReceiptNumberTemplate: "PN-{YYYY}{MM}-{SEQ:4}",
		StockIssueNumberTemplate: "PX-{YYYY}{MM}-{SEQ:4}", StockIssueMode: mode,
		StockCountNumberTemplate: "KK-{YYYY}{MM}-{SEQ:4}", WriteOffNumberTemplate: "XH-{YYYY}{MM}-{SEQ:4}",
		SupplierReturnNumberTemplate: "TN-{YYYY}{MM}-{SEQ:4}",
	}
}

func draftIssue(lines ...domain.StockIssueLine) *domain.StockIssue {
	return &domain.StockIssue{
		ID: issueID, GarageID: garageID, Number: "PX-202610-0007", RepairOrderID: repairOrderID, RepairOrderNumber: "LSC-1",
		Status: domain.StockIssueDraft, Lines: lines,
	}
}

func issueLine(id, part, quantity string) domain.StockIssueLine {
	return domain.StockIssueLine{ID: id, RepairOrderLineID: roLineID, PartID: part, Quantity: quantity, EnteredQuantity: quantity}
}

// expectPosting stubs the writes of posting an issue of one part.
func expectPosting(d *deps, quantity, cost string) {
	d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.MatchedBy(func(tx *domain.StockTransaction) bool {
		return tx.PartID == partID && tx.Type == domain.TransactionOut && tx.Quantity == quantity &&
			tx.RefType == domain.RefStockIssue && *tx.RefID == issueID
	})).Run(func(_ context.Context, tx *domain.StockTransaction) { tx.UnitCost = &cost }).Return(nil).Once()
	d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
		return i.Status == domain.StockIssuePosted && i.PostedAt != nil && *i.Lines[0].UnitCost == cost
	})).Return(nil).Once()
	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
	d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, mock.Anything, mock.Anything).Return(nil).Once()
}

func TestInventoryUseCase_HandleRepairOrderCreated(t *testing.T) {
	t.Parallel()

	t.Run("creates the numbered draft stock issue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
			return i.Number == "PX-202610-0007" && i.RepairOrderID == repairOrderID && i.RepairOrderNumber == "LSC-1" &&
				i.Status == domain.StockIssueDraft && len(i.Lines) == 1 && i.Lines[0].PartID == partID &&
				i.Lines[0].RepairOrderLineID == roLineID && i.Lines[0].Quantity == "2"
		})).Return(true, nil).Once()

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("keeps the packaging unit the line was typed in", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
			l := i.Lines[0]

			return l.Quantity == "48" && l.EnteredQuantity == "2" && l.UnitID != nil && *l.UnitID == unitID
		})).Return(true, nil).Once()

		payload := []byte(`{"repair_order_id":"` + repairOrderID + `","part_lines":[{"line_id":"` + roLineID + `","item_ref_id":"` +
			partID + `","quantity":"48","unit_id":"` + unitID + `","entered_quantity":"2"}]}`)

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(payload)))
	})

	t.Run("an order that already has one is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(draftIssue(), nil).Once()

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("an order without part lines has no stock issue", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent([]byte(`{"repair_order_id":"`+repairOrderID+`","part_lines":[]}`))))
	})

	t.Run("a line whose part no longer resolves is skipped", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, nil).Once()

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("a bad quantity is skipped", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "two"))))
	})

	t.Run("failures are returned for a retry", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()
		assertAppError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))), "INTERNAL")

		uc, d = newUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(false, errBoom).Once()
		assertAppError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))), "INTERNAL")
	})

	t.Run("drops a malformed payload", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent([]byte(`not json`))))
		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent([]byte(`{"part_lines":[]}`))))
	})
}

func TestInventoryUseCase_HandleRepairOrderStarted(t *testing.T) {
	t.Parallel()

	t.Run("posts the issue when the garage issues on start", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_start"), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		expectPosting(d, "2", "10000")

		require.NoError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("does nothing when the garage issues on close", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()

		require.NoError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("an order without an issue, or with one posted, is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_start"), nil).Twice()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(nil, domain.ErrStockIssueNotFound).Once()

		require.NoError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent(orderPayload(partID, "2"))))

		posted := draftIssue()
		posted.Status = domain.StockIssuePosted
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(posted, nil).Once()

		require.NoError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("failures and malformed payloads", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
		assertAppError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent(orderPayload(partID, "2"))), "INTERNAL")
		require.NoError(t, uc.HandleRepairOrderStarted(t.Context(), orderEvent([]byte(`not json`))))
	})
}

func TestInventoryUseCase_HandleRepairOrderClosed(t *testing.T) {
	t.Parallel()

	t.Run("posts the draft stock issue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(draftIssue(), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		expectPosting(d, "2", "10000")

		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("creates the issue first when the order has none", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(true, nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		expectPosting(d, "2", "10000")

		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("a redelivered event does not post twice", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		posted := draftIssue()
		posted.Status = domain.StockIssuePosted

		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(posted, nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(posted, nil).Once()

		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("an order without part lines posts nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(nil, domain.ErrStockIssueNotFound).Once()

		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent([]byte(`{"repair_order_id":"`+repairOrderID+`","part_lines":[]}`))))
	})

	t.Run("failures are returned for a retry", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()
		assertAppError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent(orderPayload(partID, "2"))), "INTERNAL")

		uc, d = newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(nil, errBoom).Once()
		assert.Error(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent([]byte(`{"repair_order_id":"`+repairOrderID+`"}`))))
	})

	t.Run("drops a malformed payload", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent([]byte(`not json`))))
		require.NoError(t, uc.HandleRepairOrderClosed(t.Context(), orderEvent([]byte(`{"part_lines":[]}`))))
	})
}
