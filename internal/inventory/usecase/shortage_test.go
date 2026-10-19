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

const otherIssueID = "88888888-8888-8888-8888-888888888888"

func draftRow(issue, order string, short, affected bool, line, part, quantity string) domain.DraftLine {
	return domain.DraftLine{
		IssueID: issue, RepairOrderID: order, IssueIsShort: short, Affected: affected, LineID: line, PartID: part, Quantity: quantity,
	}
}

func expectDrafts(d *deps, onHand string, rows ...domain.DraftLine) {
	d.Repo.EXPECT().DraftLines(mock.Anything, garageID, mock.Anything).Return(rows, nil).Once()
	d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).
		Return(map[string]usecase.Balance{partID: {OnHand: onHand}}, nil).Once()
}

func TestInventoryUseCase_CreateStockIssueMarksAShortage(t *testing.T) {
	t.Parallel()

	t.Run("an issue the stock cannot cover is short, and the order is told", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(true, nil).Once()
		expectDrafts(d, "1", draftRow(issueID, repairOrderID, false, true, "l1", partID, "2"))
		d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, issueID, true).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventShortageChanged, garageID, "", map[string]any{
			"repair_order_id": repairOrderID, "garage_id": garageID, "short": true,
		}).Return(nil).Once()

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("an issue the stock covers stays as it is", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(true, nil).Once()
		expectDrafts(d, "5", draftRow(issueID, repairOrderID, false, true, "l1", partID, "2"))

		require.NoError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("a failing check fails the creation", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		expectNewIssue(d)
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(true, nil).Once()
		d.Repo.EXPECT().DraftLines(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		assertAppError(t, uc.HandleRepairOrderCreated(t.Context(), orderEvent(orderPayload(partID, "2"))), apperror.CodeInternal)
	})
}

func TestInventoryUseCase_RecheckAfterAMovement(t *testing.T) {
	t.Parallel()

	t.Run("stock that arrives covers a waiting order, and only that order is told", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		// A waiting order first in line, another order that is only listed to serve its lines.
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(nil).Once()
		expectDrafts(d, "9",
			draftRow(otherIssueID, "other-order", true, true, "l9", partID, "3"),
			draftRow(issueID, repairOrderID, false, false, "l1", partID, "2"),
		)
		d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, otherIssueID, false).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventShortageChanged, garageID, "", map[string]any{
			"repair_order_id": "other-order", "garage_id": garageID, "short": false,
		}).Return(nil).Once()

		require.NoError(t, uc.HandleRepairOrderCancelled(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("an issue that is not affected is never touched", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(nil).Once()
		// The other order is short and stays short: nothing to store.
		expectDrafts(d, "0", draftRow(otherIssueID, "other-order", true, true, "l9", partID, "3"))

		require.NoError(t, uc.HandleRepairOrderCancelled(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})

	t.Run("a failing store or event is returned", func(t *testing.T) {
		t.Parallel()

		for _, fail := range []string{"short", "event"} {
			uc, d := newBareUseCase(t)
			d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
				Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
			d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(nil).Once()
			expectDrafts(d, "9", draftRow(otherIssueID, "other-order", true, true, "l9", partID, "3"))

			if fail == "short" {
				d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, otherIssueID, false).Return(errBoom).Once()
			} else {
				d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, otherIssueID, false).Return(nil).Once()
				d.Events.EXPECT().Publish(mock.Anything, usecase.EventShortageChanged, garageID, "", mock.Anything).Return(errBoom).Once()
			}

			require.Error(t, uc.HandleRepairOrderCancelled(t.Context(), orderEvent(orderPayload(partID, "2"))))
		}
	})

	t.Run("balances that cannot be read fail the check", func(t *testing.T) {
		t.Parallel()

		uc, d := newBareUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("l1", partID, "2")), nil).Once()
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().DraftLines(mock.Anything, garageID, mock.Anything).
			Return([]domain.DraftLine{draftRow(otherIssueID, "o", false, true, "l9", partID, "1")}, nil).Once()
		d.Repo.EXPECT().Balances(mock.Anything, garageID, mock.Anything).Return(nil, errBoom).Once()

		require.Error(t, uc.HandleRepairOrderCancelled(t.Context(), orderEvent(orderPayload(partID, "2"))))
	})
}

func TestInventoryUseCase_ResyncStockIssue(t *testing.T) {
	t.Parallel()

	t.Run("the draft follows the changed part lines", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(issueLine("old", partID, "1")), nil).Once()
		d.Repo.EXPECT().ReplaceIssueLines(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
			return len(i.Lines) == 1 && i.Lines[0].Quantity == "5" && i.Lines[0].RepairOrderLineID == roLineID
		})).Return(nil).Once()

		require.NoError(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))
	})

	t.Run("an order without an issue gets one", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Garages.EXPECT().Get(mock.Anything, garageID).Return(garageSettings("on_close"), nil).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(nil, domain.ErrStockIssueNotFound).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).
			Return(nil, domain.ErrStockIssueNotFound).Once()
		d.Sequences.EXPECT().Next(mock.Anything, garageID, "stock_issue", "PX-202610-{SEQ:4}").Return(7, nil).Once()
		d.Repo.EXPECT().CreateStockIssue(mock.Anything, mock.Anything).Return(true, nil).Once()

		require.NoError(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))
	})

	t.Run("a posted issue is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		posted := draftIssue()
		posted.Status = domain.StockIssuePosted
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(posted, nil).Once()

		require.NoError(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))
	})

	t.Run("failures are returned for a retry", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(nil, errBoom).Once()
		require.Error(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return(nil, errBoom).Once()
		require.Error(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(draftIssue(), nil).Once()
		d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
		d.Repo.EXPECT().ReplaceIssueLines(mock.Anything, mock.Anything).Return(errBoom).Once()
		require.Error(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent(orderPayload(partID, "5"))))
	})

	t.Run("a malformed event is dropped", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		require.NoError(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent([]byte(`nope`))))
		require.NoError(t, uc.HandleRepairOrderCancelled(t.Context(), orderEvent([]byte(`nope`))))
	})
}

func TestInventoryUseCase_CancelStockIssue(t *testing.T) {
	t.Parallel()

	t.Run("a draft is cancelled and its reservation released", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		short := draftIssue(issueLine("l1", partID, "2"))
		short.IsShort = true
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(short, nil).Once()
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
			return i.Status == domain.StockIssueCancelled && !i.IsShort
		})).Return(nil).Once()

		require.NoError(t, uc.CancelStockIssue(t.Context(), garageID, repairOrderID))
	})

	t.Run("an order without an issue, or with a posted one, is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		posted := draftIssue()
		posted.Status = domain.StockIssuePosted

		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).
			Return(nil, domain.ErrStockIssueNotFound).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(posted, nil).Once()

		require.NoError(t, uc.CancelStockIssue(t.Context(), garageID, repairOrderID))
		require.NoError(t, uc.CancelStockIssue(t.Context(), garageID, repairOrderID))
	})

	t.Run("failures are returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(nil, errBoom).Once()
		d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(draftIssue(), nil).Once()
		d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.Anything).Return(errBoom).Once()

		require.Error(t, uc.CancelStockIssue(t.Context(), garageID, repairOrderID))
		require.Error(t, uc.CancelStockIssue(t.Context(), garageID, repairOrderID))
	})
}

func TestInventoryUseCase_ResyncThatEmptiesAShortIssueClearsItsMark(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	short := draftIssue(issueLine("old", partID, "9"))
	short.IsShort = true

	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(short, nil).Once()
	d.Repo.EXPECT().ReplaceIssueLines(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool { return len(i.Lines) == 0 })).Return(nil).Once()
	d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, issueID, false).Return(nil).Once()
	d.Events.EXPECT().Publish(mock.Anything, usecase.EventShortageChanged, garageID, "", map[string]any{
		"repair_order_id": repairOrderID, "garage_id": garageID, "short": false,
	}).Return(nil).Once()

	require.NoError(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent([]byte(`{"repair_order_id":"`+repairOrderID+`","part_lines":[]}`))))

	// A failure to clear it is returned.
	again := draftIssue(issueLine("old", partID, "9"))
	again.IsShort = true
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, true).Return(again, nil).Once()
	d.Repo.EXPECT().ReplaceIssueLines(mock.Anything, mock.Anything).Return(nil).Once()
	d.Repo.EXPECT().SetIssueShort(mock.Anything, garageID, issueID, false).Return(errBoom).Once()

	require.Error(t, uc.HandleRepairOrderLinesChanged(t.Context(), orderEvent([]byte(`{"repair_order_id":"`+repairOrderID+`","part_lines":[]}`))))
}

func TestInventoryUseCase_StockIssueShort(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	short, covered, posted := draftIssue(), draftIssue(), draftIssue()
	short.IsShort, posted.IsShort, posted.Status = true, true, domain.StockIssuePosted

	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(short, nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(covered, nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(posted, nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(nil, domain.ErrStockIssueNotFound).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(nil, errBoom).Once()

	for _, want := range []bool{true, false, false, false} {
		got, err := uc.StockIssueShort(t.Context(), garageID, repairOrderID)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	_, err := uc.StockIssueShort(t.Context(), garageID, repairOrderID)
	require.Error(t, err)
}

func TestInventoryUseCase_StockIssuePosted(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	posted := draftIssue()
	posted.Status = domain.StockIssuePosted

	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(posted, nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(draftIssue(), nil).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).
		Return(nil, domain.ErrStockIssueNotFound).Once()
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).Return(nil, errBoom).Once()

	for _, want := range []bool{true, false, false} {
		got, err := uc.StockIssuePosted(t.Context(), garageID, repairOrderID)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}

	_, err := uc.StockIssuePosted(t.Context(), garageID, repairOrderID)
	require.Error(t, err)
}

func TestInventoryUseCase_PostingAShortIssueClearsTheMark(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	short := draftIssue(issueLine("l1", partID, "2"))
	short.IsShort = true

	d.Repo.EXPECT().FindStockIssue(mock.Anything, garageID, issueID, true).Return(short, nil).Once()
	d.Repo.EXPECT().RecordTransaction(mock.Anything, mock.Anything).Return(nil).Once()
	d.Repo.EXPECT().SaveStockIssue(mock.Anything, mock.MatchedBy(func(i *domain.StockIssue) bool {
		return i.Status == domain.StockIssuePosted && !i.IsShort
	})).Return(nil).Once()
	d.Events.EXPECT().Publish(mock.Anything, usecase.EventShortageChanged, garageID, "", map[string]any{
		"repair_order_id": repairOrderID, "garage_id": garageID, "short": false,
	}).Return(nil).Once()
	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()
	d.Events.EXPECT().Publish(mock.Anything, usecase.EventStockIssued, garageID, mock.Anything, mock.Anything).Return(nil).Once()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, mock.Anything).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	d.Directory.EXPECT().UserName(mock.Anything, actorID).Return("An", nil).Once()

	issue, err := uc.PostStockIssue(t.Context(), garageID, issueID, actorID)
	require.NoError(t, err)
	assert.False(t, issue.IsShort)
}

func TestInventoryUseCase_GetRepairOrderStockIssueShowsWhatIsShort(t *testing.T) {
	t.Parallel()

	uc, d := newBareUseCase(t)
	d.Repo.EXPECT().FindStockIssueByRepairOrder(mock.Anything, garageID, repairOrderID, false).
		Return(draftIssue(issueLine("l1", partID, "4")), nil).Once()
	d.Parts.EXPECT().Parts(mock.Anything, garageID, []string{partID}).Return([]usecase.PartInfo{samplePart()}, nil).Once()
	expectDrafts(d, "1", draftRow(issueID, repairOrderID, false, true, "l1", partID, "4"))

	issue, err := uc.GetRepairOrderStockIssue(t.Context(), garageID, repairOrderID)
	require.NoError(t, err)
	assert.Equal(t, "3", issue.Lines[0].ShortQuantity)
}
