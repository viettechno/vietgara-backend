package usecase_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
	"github.com/viettechno/vietgara-backend/internal/repairorder/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/partquantity"
)

func expectFind(d *deps, ro *domain.RepairOrder) {
	d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(ro, nil).Once()
}

func partLine(qty string) usecase.LineInput {
	ref := partID

	return usecase.LineInput{
		ItemType: domain.ItemTypePart, ItemRefID: &ref, Description: "Grease", Quantity: qty, UnitPrice: 5_000,
	}
}

//nolint:funlen // one table of scenarios
func TestRepairOrderUseCase_AddLine(t *testing.T) {
	t.Parallel()

	req := func(in usecase.LineInput) usecase.AddLineRequest {
		return usecase.AddLineRequest{GarageID: garageID, RepairOrderID: repairOrderID, ActorID: actorID, Line: in}
	}

	t.Run("a labor line is added without touching the stock issue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.MatchedBy(func(ro *domain.RepairOrder) bool {
			return len(ro.Lines) == 2 && ro.Lines[1].Position == 2 && ro.TotalAmount == 300_000
		})).Return(nil).Once()

		ro, err := uc.AddLine(t.Context(), req(usecase.LineInput{
			ItemType: domain.ItemTypeLabor, Description: "Wash", Quantity: "1", UnitPrice: 50_000,
		}))
		require.NoError(t, err)
		assert.Equal(t, domain.LineNotStarted, ro.Lines[1].Status)
	})

	t.Run("a part line reaches the draft stock issue", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		_, err := uc.AddLine(t.Context(), req(partLine("2")))
		require.NoError(t, err)
	})

	t.Run("a hidden consumable is priced at zero and carries its job", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		in := partLine("1")
		in.NonBillable, in.JobID = true, new(jobID)

		ro, err := uc.AddLine(t.Context(), req(in))
		require.NoError(t, err)
		assert.Equal(t, int64(0), ro.Lines[1].UnitPrice)
		assert.Equal(t, int64(250_000), ro.TotalAmount)
		assert.Equal(t, jobID, *ro.Lines[1].JobID)
	})

	t.Run("a part line is refused once the stock issue is posted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(true, nil).Once()

		_, err := uc.AddLine(t.Context(), req(partLine("2")))
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("a labor line is still accepted after posting", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		_, err := uc.AddLine(t.Context(), req(usecase.LineInput{
			ItemType: domain.ItemTypeLabor, Description: "Wash", Quantity: "1", UnitPrice: 1,
		}))
		require.NoError(t, err)
	})

	t.Run("a finished order takes new work back to open", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := openRepairOrder()
		ro.Lines[0].Status, ro.Status = domain.LineCompleted, domain.StatusCompleted
		expectFind(d, ro)
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		got, err := uc.AddLine(t.Context(), req(usecase.LineInput{
			ItemType: domain.ItemTypeLabor, Description: "Wash", Quantity: "1", UnitPrice: 1,
		}))
		require.NoError(t, err)
		assert.Equal(t, domain.StatusInProgress, got.Status)
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()

		closed, cancelled, full := openRepairOrder(), openRepairOrder(), openRepairOrder()
		closed.Status, cancelled.Status = domain.StatusClosed, domain.StatusCancelled

		for range domain.MaxLines {
			full.Lines = append(full.Lines, domain.Line{ID: "x"})
		}

		bad := partLine("2")
		bad.NonBillable, bad.ItemRefID = true, nil

		tests := []struct {
			name string
			ro   *domain.RepairOrder
			in   usecase.LineInput
			code apperror.Code
		}{
			{"closed", closed, partLine("1"), apperror.CodeFailedPrecondition},
			{"cancelled", cancelled, partLine("1"), apperror.CodeFailedPrecondition},
			{"full", full, partLine("1"), apperror.CodeInvalidArgument},
			{"invalid line", openRepairOrder(), usecase.LineInput{ItemType: domain.ItemTypeLabor}, apperror.CodeInvalidArgument},
			{"a consumable without a part", openRepairOrder(), bad, apperror.CodeInvalidArgument},
		}

		for _, tt := range tests {
			uc, d := newUseCase(t)
			expectFind(d, tt.ro)

			_, err := uc.AddLine(t.Context(), req(tt.in))
			assertAppError(t, err, tt.code)
		}
	})

	t.Run("failures", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrRepairOrderNotFound).Once()

		_, err := uc.AddLine(t.Context(), req(partLine("1")))
		assertAppError(t, err, apperror.CodeNotFound)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, errBoom).Once()

		_, err = uc.AddLine(t.Context(), req(partLine("1")))
		assertAppError(t, err, apperror.CodeInternal)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(errBoom).Once()

		_, err = uc.AddLine(t.Context(), req(partLine("1")))
		assertAppError(t, err, apperror.CodeInternal)

		expectFind(d, openRepairOrder())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(errBoom).Once()

		_, err = uc.AddLine(t.Context(), req(partLine("1")))
		assertAppError(t, err, apperror.CodeInternal)
	})
}

func TestRepairOrderUseCase_DeleteLine(t *testing.T) {
	t.Parallel()

	t.Run("removing a part line renumbers and tells Inventory", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		ro, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, partRef, actorID)
		require.NoError(t, err)
		require.Len(t, ro.Lines, 1)
		assert.Equal(t, int64(250_000), ro.TotalAmount)
	})

	t.Run("removing the first line renumbers the rest", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		ro, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, "line-1", actorID)
		require.NoError(t, err)
		require.Len(t, ro.Lines, 1)
		assert.Equal(t, 1, ro.Lines[0].Position)
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()

		onlyHidden := repairOrderWithPart()
		onlyHidden.Lines[1].NonBillable = true

		cancelled := openRepairOrder()
		cancelled.Status = domain.StatusCancelled

		tests := []struct {
			name string
			ro   *domain.RepairOrder
			line string
			code apperror.Code
		}{
			{"the last line", openRepairOrder(), "line-1", apperror.CodeFailedPrecondition},
			{"unknown line", openRepairOrder(), "nope", apperror.CodeNotFound},
			{"cancelled", cancelled, "line-1", apperror.CodeFailedPrecondition},
			{"leaving only a hidden consumable", onlyHidden, "line-1", apperror.CodeInvalidArgument},
		}

		for _, tt := range tests {
			uc, d := newUseCase(t)
			expectFind(d, tt.ro)

			_, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, tt.line, actorID)
			assertAppError(t, err, tt.code)
		}
	})

	t.Run("a part line stays once the stock issue is posted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(true, nil).Once()

		_, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, partRef, actorID)
		assertAppError(t, err, apperror.CodeFailedPrecondition)
		assertReason(t, err, usecase.ReasonStockIssuePosted)
	})

	t.Run("the last line has its own reason", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, openRepairOrder())

		_, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, "line-1", actorID)
		assertReason(t, err, usecase.ReasonLastLine)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.RepairOrders.EXPECT().FindByID(mock.Anything, garageID, repairOrderID).Return(nil, domain.ErrRepairOrderNotFound).Once()

		_, err := uc.DeleteLine(t.Context(), garageID, repairOrderID, "x", actorID)
		assertAppError(t, err, apperror.CodeNotFound)
	})
}

//nolint:funlen // one table of scenarios
func TestRepairOrderUseCase_EditLine(t *testing.T) {
	t.Parallel()

	edit := func(e usecase.LineEdit) usecase.UpdateLineRequest {
		return usecase.UpdateLineRequest{GarageID: garageID, RepairOrderID: repairOrderID, LineID: partRef, ActorID: actorID, Edit: &e}
	}

	t.Run("a price or description change leaves the stock issue alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()

		ro, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Description: new("Synthetic oil"), UnitPrice: new(int64(120_000))}))
		require.NoError(t, err)
		assert.Equal(t, "Synthetic oil", ro.Lines[1].Description)
		assert.Equal(t, int64(480_000), ro.Lines[1].LineTotal)
		assert.Equal(t, int64(730_000), ro.TotalAmount)
		assert.Equal(t, partRef, ro.Lines[1].ID)
	})

	t.Run("a new quantity is checked against the stock issue and published", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		ro, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Quantity: new("5")}))
		require.NoError(t, err)
		assert.Equal(t, "5", ro.Lines[1].Quantity)
	})

	t.Run("a packaging unit is resolved and kept", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.Parts.EXPECT().ResolveQuantity(mock.Anything, garageID, partID, unitID, "2").
			Return(partquantity.Resolved{Base: "48", UnitID: new(unitID), Entered: new("2"), UnitAbbreviation: "case"}, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		ro, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{UnitID: new(unitID), EnteredQuantity: new("2")}))
		require.NoError(t, err)
		assert.Equal(t, "48", ro.Lines[1].Quantity)
		assert.Equal(t, "case", ro.Lines[1].UnitAbbreviation)
	})

	t.Run("only the quantity of a line in a packaging unit is refused", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := repairOrderWithPart()
		ro.Lines[1].UnitID, ro.Lines[1].EnteredQuantity = new(unitID), new("2")
		expectFind(d, ro)

		_, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Quantity: new("5")}))
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("moving a packaged line back to the base unit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := repairOrderWithPart()
		ro.Lines[1].UnitID, ro.Lines[1].EnteredQuantity = new(unitID), new("2")
		expectFind(d, ro)
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		got, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{UnitID: new(""), Quantity: new("3")}))
		require.NoError(t, err)
		assert.Nil(t, got.Lines[1].UnitID)
		assert.Equal(t, "3", got.Lines[1].Quantity)
	})

	t.Run("the part itself can change", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(nil).Once()

		ro, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{ItemRefID: new("another-part")}))
		require.NoError(t, err)
		assert.Equal(t, "another-part", *ro.Lines[1].ItemRefID)
	})

	t.Run("the parts are fixed once the stock issue is posted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(true, nil).Once()

		_, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Quantity: new("9")}))
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})

	t.Run("an edit that fails validation changes nothing", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())

		_, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Description: new(" ")}))
		assertAppError(t, err, apperror.CodeInvalidArgument)
	})

	t.Run("a failing publish is returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		expectFind(d, repairOrderWithPart())
		d.Issues.EXPECT().Posted(mock.Anything, garageID, repairOrderID).Return(false, nil).Once()
		d.RepairOrders.EXPECT().ReplaceLines(mock.Anything, mock.Anything).Return(nil).Once()
		d.Events.EXPECT().Publish(mock.Anything, usecase.EventRepairOrderLinesChanged, garageID, "", mock.Anything).Return(errBoom).Once()

		_, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Quantity: new("5")}))
		assertAppError(t, err, apperror.CodeInternal)
	})

	t.Run("a cancelled order cannot be edited", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		ro := repairOrderWithPart()
		ro.Status = domain.StatusCancelled
		expectFind(d, ro)

		_, err := uc.UpdateLine(t.Context(), edit(usecase.LineEdit{Quantity: new("5")}))
		assertAppError(t, err, apperror.CodeFailedPrecondition)
	})
}
