package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

func TestRepairOrderStatus_IsValid(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.RepairOrderStatus{
		domain.StatusOpen, domain.StatusInProgress, domain.StatusWaitingOnParts, domain.StatusCompleted, domain.StatusClosed,
		domain.StatusCancelled,
	} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.RepairOrderStatus("archived").IsValid())
	assert.False(t, domain.RepairOrderStatus("").IsValid())
}

func TestItemType_IsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ItemTypeLabor.IsValid())
	assert.True(t, domain.ItemTypePart.IsValid())
	assert.False(t, domain.ItemType("consumable").IsValid())
}

func TestLineStatus_IsValid(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.LineStatus{domain.LineNotStarted, domain.LineInProgress, domain.LineCompleted} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.LineStatus("blocked").IsValid())
}

func TestRepairOrder_Recalculate(t *testing.T) {
	t.Parallel()

	ro := &domain.RepairOrder{Lines: []domain.Line{{LineTotal: 250_000}, {LineTotal: 480_000}}}
	ro.Recalculate()
	assert.Equal(t, int64(730_000), ro.TotalAmount)

	ro = &domain.RepairOrder{}
	ro.Recalculate()
	assert.Equal(t, int64(0), ro.TotalAmount)
}

func TestRepairOrder_DeriveStatus(t *testing.T) {
	t.Parallel()

	t.Run("no lines is open", func(t *testing.T) {
		t.Parallel()

		ro := &domain.RepairOrder{Status: domain.StatusOpen}
		ro.DeriveStatus()
		assert.Equal(t, domain.StatusOpen, ro.Status)
	})

	t.Run("any line started moves to in_progress", func(t *testing.T) {
		t.Parallel()

		ro := &domain.RepairOrder{Lines: []domain.Line{{Status: domain.LineInProgress}, {Status: domain.LineNotStarted}}}
		ro.DeriveStatus()
		assert.Equal(t, domain.StatusInProgress, ro.Status)
	})

	t.Run("every line completed moves to completed", func(t *testing.T) {
		t.Parallel()

		ro := &domain.RepairOrder{Lines: []domain.Line{{Status: domain.LineCompleted}, {Status: domain.LineCompleted}}}
		ro.DeriveStatus()
		assert.Equal(t, domain.StatusCompleted, ro.Status)
	})

	t.Run("closed never changes", func(t *testing.T) {
		t.Parallel()

		ro := &domain.RepairOrder{Status: domain.StatusClosed, Lines: []domain.Line{{Status: domain.LineNotStarted}}}
		ro.DeriveStatus()
		assert.Equal(t, domain.StatusClosed, ro.Status)
	})
}

func TestRepairOrder_ReadyToClose(t *testing.T) {
	t.Parallel()

	assert.False(t, (&domain.RepairOrder{}).ReadyToClose())
	assert.False(t, (&domain.RepairOrder{Lines: []domain.Line{{Status: domain.LineInProgress}}}).ReadyToClose())
	assert.True(t, (&domain.RepairOrder{Lines: []domain.Line{{Status: domain.LineCompleted}, {Status: domain.LineCompleted}}}).ReadyToClose())
}

func TestRepairOrder_PartLines(t *testing.T) {
	t.Parallel()

	itemID := "item-1"
	empty := ""
	ro := &domain.RepairOrder{Lines: []domain.Line{
		{ItemType: domain.ItemTypeLabor, ItemRefID: &itemID},
		{ItemType: domain.ItemTypePart, ItemRefID: &itemID},
		{ItemType: domain.ItemTypePart, ItemRefID: nil},
		{ItemType: domain.ItemTypePart, ItemRefID: &empty},
	}}

	lines := ro.PartLines()
	assert.Len(t, lines, 1)
	assert.Equal(t, itemID, *lines[0].ItemRefID)
}
