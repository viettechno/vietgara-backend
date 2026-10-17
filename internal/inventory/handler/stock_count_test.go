package handler_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
	"github.com/viettechno/vietgara-backend/internal/inventory/usecase"
)

const (
	countID     = "c0c0c0c0-0000-0000-0000-000000000001"
	secondCount = "c0c0c0c0-0000-0000-0000-000000000002"
)

func (e *env) expectCountNames() {
	e.parts.EXPECT().Parts(anyArg, garageID, anyArg).Return([]usecase.PartInfo{samplePart()}, nil).Maybe()
	e.parts.EXPECT().UnitAbbreviations(anyArg, garageID, anyArg).Return(map[string]string{unitID: "ctn"}, nil).Maybe()
	e.directory.EXPECT().UserName(anyArg, anyArg).Return("Lan Nguyen", nil).Maybe()
	e.directory.EXPECT().SupplierNames(anyArg, garageID, anyArg).Return(map[string]string{supplierID: "Phu Tung Sai Gon"}, nil).Maybe()
}

func storedCount() *domain.StockCount {
	at := time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC)

	return &domain.StockCount{
		ID: countID, GarageID: garageID, Number: "KK-202610-0001", Name: "Shelf A", Notes: "n", Status: domain.StockCountCompleted,
		CompletedAt: &at, CompletedBy: new(actorID), CreatedBy: new(actorID), CreatedAt: at, UpdatedAt: at,
		Lines: []domain.StockCountLine{
			{ID: "l1", PartID: partID, SystemQuantity: "10", Counted: new("12"), EnteredCounted: new("1"), UnitID: new(unitID), AverageCost: new("50.5"), CountedAt: &at},
			{ID: "l2", Position: 1, PartID: partID, SystemQuantity: "4"},
		},
	}
}

func TestGetStockCount(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, false).Return(storedCount(), nil).Twice()
	e.expectCountNames()

	resp, err := e.srv.GetStockCount(signedInWithCost(t), &inventoryv1.GetStockCountRequest{GarageId: garageID, StockCountId: countID})
	require.NoError(t, err)

	c := resp.GetStockCount()
	assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED, c.GetStatus())
	assert.Equal(t, "Lan Nguyen", c.GetCompletedByName())
	assert.Equal(t, int32(1), c.GetOverLines())
	assert.Equal(t, int32(1), c.GetUncountedLines())
	assert.Equal(t, int64(101), c.GetTotalDifferenceValue())

	counted, uncounted := c.GetLines()[0], c.GetLines()[1]
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_OVER, counted.GetResult())
	assert.Equal(t, "2", counted.GetDifferenceQuantity())
	assert.Equal(t, "12", counted.GetCountedQuantity())
	assert.Equal(t, "1", counted.GetEnteredCountedQuantity())
	assert.Equal(t, "ctn", counted.GetUnitAbbreviation())
	assert.Equal(t, int64(51), counted.GetAverageCost())
	assert.Equal(t, int64(101), counted.GetDifferenceValue())
	assert.NotNil(t, counted.GetCountedAt())
	assert.Equal(t, inventoryv1.StockCountLineResult_STOCK_COUNT_LINE_RESULT_UNCOUNTED, uncounted.GetResult())
	assert.Empty(t, uncounted.GetCountedQuantity())
	assert.Empty(t, uncounted.GetDifferenceQuantity())

	resp, err = e.srv.GetStockCount(signedIn(t), &inventoryv1.GetStockCountRequest{GarageId: garageID, StockCountId: countID})
	require.NoError(t, err)
	assert.Nil(t, resp.GetStockCount().TotalDifferenceValue, "costs are omitted without inventory.cost.read")
	assert.Nil(t, resp.GetStockCount().GetLines()[0].AverageCost)
	assert.Nil(t, resp.GetStockCount().GetLines()[0].DifferenceValue)

	e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, false).Return(nil, domain.ErrStockCountNotFound).Once()
	_, err = e.srv.GetStockCount(signedIn(t), &inventoryv1.GetStockCountRequest{GarageId: garageID, StockCountId: countID})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestListStockCounts(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	listed := storedCount()
	listed.Lines, listed.Tally, listed.TotalDifferenceValue = nil, domain.Tally{Matched: 3, Short: 1}, -77

	e.repo.EXPECT().ListStockCounts(anyArg, garageID, usecase.StockCountFilter{
		Query: "shelf", Status: domain.StockCountCompleted, Limit: 10, Offset: 10,
	}).Return([]*domain.StockCount{listed}, 11, nil).Once()
	e.expectCountNames()

	resp, err := e.srv.ListStockCounts(signedInWithCost(t), &inventoryv1.ListStockCountsRequest{
		GarageId: garageID, Query: "shelf", Status: inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED, Page: 2, PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(11), resp.GetPagination().GetTotal())
	assert.Equal(t, int32(3), resp.GetData()[0].GetMatchedLines())
	assert.Equal(t, int32(1), resp.GetData()[0].GetShortLines())
	assert.Equal(t, int64(-77), resp.GetData()[0].GetTotalDifferenceValue())
	assert.Empty(t, resp.GetData()[0].GetLines())

	e.repo.EXPECT().ListStockCounts(anyArg, garageID, anyArg).Return(nil, 0, errBoom).Once()
	_, err = e.srv.ListStockCounts(signedIn(t), &inventoryv1.ListStockCountsRequest{GarageId: garageID})
	assert.Equal(t, codes.Internal, status.Code(err))

	for _, s := range []inventoryv1.StockCountStatus{
		inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED,
		inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_UNSPECIFIED, inventoryv1.StockCountStatus(99),
	} {
		e.repo.EXPECT().ListStockCounts(anyArg, garageID, anyArg).Return(nil, 0, nil).Once()
		_, err = e.srv.ListStockCounts(signedIn(t), &inventoryv1.ListStockCountsRequest{GarageId: garageID, Status: s})
		require.NoError(t, err)
	}
}

func draftOf(lines ...domain.StockCountLine) *domain.StockCount {
	c := storedCount()
	c.Status, c.CompletedAt, c.CompletedBy, c.Lines = domain.StockCountDraft, nil, nil, lines

	return c
}

//nolint:funlen // one table of stock count calls
func TestStockCountWrites(t *testing.T) {
	t.Parallel()

	t.Run("create", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{Timezone: "Asia/Ho_Chi_Minh", StockCountNumberTemplate: "KK-{SEQ:4}"}, nil).Once()
		e.sequences.EXPECT().Next(anyArg, garageID, "stock_count", "KK-{SEQ:4}").Return(1, nil).Once()
		e.repo.EXPECT().CreateStockCount(anyArg, mock.MatchedBy(func(c *domain.StockCount) bool { return c.Name == "Shelf A" && c.Notes == "n" })).Return(nil).Once()
		e.expectCountNames()

		resp, err := e.srv.CreateStockCount(signedIn(t), &inventoryv1.CreateStockCountRequest{
			GarageId: garageID, StockCount: &inventoryv1.StockCount{Name: "Shelf A", Notes: "n"},
		})
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_DRAFT, resp.GetStockCount().GetStatus())

		e.garages.EXPECT().Get(anyArg, garageID).Return(usecase.GarageSettings{}, errBoom).Once()
		_, err = e.srv.CreateStockCount(signedIn(t), &inventoryv1.CreateStockCountRequest{GarageId: garageID})
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(), nil).Twice()
		e.repo.EXPECT().SaveStockCount(anyArg, mock.MatchedBy(func(c *domain.StockCount) bool { return c.Name == "B" && c.Notes == "m" })).Return(nil).Twice()
		e.expectCountNames()

		for _, mask := range []*fieldmaskpb.FieldMask{nil, {Paths: []string{"name", "notes"}}} {
			_, err := e.srv.UpdateStockCount(signedIn(t), &inventoryv1.UpdateStockCountRequest{
				GarageId: garageID, StockCountId: countID, StockCount: &inventoryv1.StockCount{Name: "B", Notes: "m"}, UpdateMask: mask,
			})
			require.NoError(t, err)
		}

		_, err := e.srv.UpdateStockCount(signedIn(t), &inventoryv1.UpdateStockCountRequest{
			GarageId: garageID, StockCountId: countID, StockCount: &inventoryv1.StockCount{}, UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"status"}},
		})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(nil, domain.ErrStockCountNotFound).Once()
		_, err = e.srv.UpdateStockCount(signedIn(t), &inventoryv1.UpdateStockCountRequest{GarageId: garageID, StockCountId: countID})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("add, count, delete lines", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.expectCountNames()

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(), nil).Once()
		e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(map[string]usecase.Balance{partID: {OnHand: "3"}}, nil).Once()
		e.repo.EXPECT().InsertStockCountLines(anyArg, garageID, countID, anyArg, anyArg).Return(nil).Once()
		e.repo.EXPECT().SaveStockCount(anyArg, anyArg).Return(nil).Times(3)

		added, err := e.srv.AddStockCountLines(signedIn(t), &inventoryv1.AddStockCountLinesRequest{
			GarageId: garageID, StockCountId: countID, PartIds: []string{partID},
		})
		require.NoError(t, err)
		assert.Len(t, added.GetStockCount().GetLines(), 1)

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(domain.StockCountLine{ID: "l1", PartID: partID, SystemQuantity: "1"}), nil).Twice()
		e.parts.EXPECT().ResolveQuantity(anyArg, garageID, partID, unitID, "2").
			Return(usecase.ResolvedQuantity{Base: "24", UnitID: new(unitID), Entered: new("2")}, nil).Once()
		e.repo.EXPECT().Balances(anyArg, garageID, []string{partID}).Return(nil, nil).Once()
		e.repo.EXPECT().SaveStockCountLine(anyArg, garageID, countID, anyArg).Return(nil).Once()

		typed, err := e.srv.UpdateStockCountLine(signedIn(t), &inventoryv1.UpdateStockCountLineRequest{
			GarageId: garageID, StockCountId: countID, LineId: "l1", Line: &inventoryv1.StockCountLine{UnitId: new(unitID), EnteredCountedQuantity: "2"},
		})
		require.NoError(t, err)
		assert.Equal(t, "24", typed.GetStockCount().GetLines()[0].GetCountedQuantity())

		e.repo.EXPECT().DeleteStockCountLine(anyArg, garageID, countID, "l1").Return(nil).Once()
		removed, err := e.srv.DeleteStockCountLine(signedIn(t), &inventoryv1.DeleteStockCountLineRequest{GarageId: garageID, StockCountId: countID, LineId: "l1"})
		require.NoError(t, err)
		assert.Empty(t, removed.GetStockCount().GetLines())

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(), nil).Times(3)
		_, err = e.srv.AddStockCountLines(signedIn(t), &inventoryv1.AddStockCountLinesRequest{GarageId: garageID, StockCountId: countID})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		_, err = e.srv.UpdateStockCountLine(signedIn(t), &inventoryv1.UpdateStockCountLineRequest{
			GarageId: garageID, StockCountId: countID, LineId: "nope", Line: &inventoryv1.StockCountLine{},
		})
		assert.Equal(t, codes.NotFound, status.Code(err))
		_, err = e.srv.DeleteStockCountLine(signedIn(t), &inventoryv1.DeleteStockCountLineRequest{GarageId: garageID, StockCountId: countID, LineId: "nope"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("merge", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.expectCountNames()

		other := draftOf(domain.StockCountLine{ID: "l9", PartID: partID, SystemQuantity: "1"})
		other.ID = secondCount

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(), nil).Once()
		e.repo.EXPECT().FindStockCount(anyArg, garageID, secondCount, true).Return(other, nil).Once()
		e.repo.EXPECT().ClearStockCountLines(anyArg, garageID, secondCount).Return(nil).Once()
		e.repo.EXPECT().SaveStockCount(anyArg, anyArg).Return(nil).Twice()
		e.repo.EXPECT().InsertStockCountLines(anyArg, garageID, countID, anyArg, anyArg).Return(nil).Once()
		e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Once()

		resp, err := e.srv.MergeStockCounts(signedIn(t), &inventoryv1.MergeStockCountsRequest{GarageId: garageID, StockCountIds: []string{countID, secondCount}})
		require.NoError(t, err)
		assert.Len(t, resp.GetStockCount().GetLines(), 1)

		_, err = e.srv.MergeStockCounts(signedIn(t), &inventoryv1.MergeStockCountsRequest{GarageId: garageID, StockCountIds: []string{countID}})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("complete and cancel", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		e.expectCountNames()
		e.audit.EXPECT().Record(anyArg, anyArg).Return(nil).Twice()

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).
			Return(draftOf(domain.StockCountLine{ID: "l1", PartID: partID, SystemQuantity: "1", Counted: new("1"), EnteredCounted: new("1")}), nil).Once()
		e.repo.EXPECT().LockBalances(anyArg, garageID, anyArg).Return(nil).Once()
		e.repo.EXPECT().Balances(anyArg, garageID, anyArg).Return(map[string]usecase.Balance{partID: {OnHand: "1"}}, nil).Once()
		e.repo.EXPECT().SaveStockCountLine(anyArg, garageID, countID, anyArg).Return(nil).Once()
		e.repo.EXPECT().SaveStockCount(anyArg, anyArg).Return(nil).Twice()

		done, err := e.srv.CompleteStockCount(signedIn(t), &inventoryv1.CompleteStockCountRequest{GarageId: garageID, StockCountId: countID, SkipUncounted: true})
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_COMPLETED, done.GetStockCount().GetStatus())

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(draftOf(), nil).Once()
		gone, err := e.srv.CancelStockCount(signedIn(t), &inventoryv1.CancelStockCountRequest{GarageId: garageID, StockCountId: countID})
		require.NoError(t, err)
		assert.Equal(t, inventoryv1.StockCountStatus_STOCK_COUNT_STATUS_CANCELLED, gone.GetStockCount().GetStatus())

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(storedCount(), nil).Once()
		_, err = e.srv.CompleteStockCount(signedIn(t), &inventoryv1.CompleteStockCountRequest{GarageId: garageID, StockCountId: countID})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))

		e.repo.EXPECT().FindStockCount(anyArg, garageID, countID, true).Return(nil, errBoom).Once()
		_, err = e.srv.CancelStockCount(signedIn(t), &inventoryv1.CancelStockCountRequest{GarageId: garageID, StockCountId: countID})
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}
