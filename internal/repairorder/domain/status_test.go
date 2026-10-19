package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

func orderOf(lines ...domain.Line) *domain.RepairOrder {
	return &domain.RepairOrder{Status: domain.StatusOpen, Lines: lines}
}

func line(status domain.LineStatus) domain.Line { return domain.Line{Status: status} }

func hidden() domain.Line { return domain.Line{NonBillable: true, Status: domain.LineNotStarted} }

func TestDeriveStatus_HiddenConsumablesNeverHoldTheOrderBack(t *testing.T) {
	t.Parallel()

	ro := orderOf(line(domain.LineCompleted), hidden())
	ro.DeriveStatus()
	assert.Equal(t, domain.StatusCompleted, ro.Status)
	assert.True(t, ro.ReadyToClose())

	ro = orderOf(line(domain.LineNotStarted), hidden())
	ro.DeriveStatus()
	assert.Equal(t, domain.StatusOpen, ro.Status)
	assert.False(t, ro.WorkStarted())
	assert.False(t, ro.ReadyToClose())

	// A hidden line that somehow moved does not start the order.
	moved := hidden()
	moved.Status = domain.LineInProgress
	ro = orderOf(line(domain.LineNotStarted), moved)
	ro.DeriveStatus()
	assert.Equal(t, domain.StatusOpen, ro.Status)
	assert.False(t, ro.WorkStarted())
}

func TestDeriveStatus_WaitingOnParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lines []domain.Line
		want  domain.RepairOrderStatus
	}{
		{"open", []domain.Line{line(domain.LineNotStarted)}, domain.StatusWaitingOnParts},
		{"in progress", []domain.Line{line(domain.LineInProgress), line(domain.LineNotStarted)}, domain.StatusWaitingOnParts},
		{"finished work is not waiting", []domain.Line{line(domain.LineCompleted)}, domain.StatusCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ro := orderOf(tt.lines...)
			ro.WaitingOnParts = true
			ro.DeriveStatus()
			assert.Equal(t, tt.want, ro.Status)
		})
	}

	// Clearing the mark brings the progress status back.
	ro := orderOf(line(domain.LineInProgress))
	ro.WaitingOnParts = true
	ro.DeriveStatus()
	ro.WaitingOnParts = false
	ro.DeriveStatus()
	assert.Equal(t, domain.StatusInProgress, ro.Status)
}

func TestDeriveStatus_FinalStatusesStay(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.RepairOrderStatus{domain.StatusClosed, domain.StatusCancelled} {
		ro := orderOf(line(domain.LineNotStarted))
		ro.Status, ro.WaitingOnParts = s, true
		ro.DeriveStatus()
		assert.Equal(t, s, ro.Status)
	}
}

func TestReadyToClose_NeedsABillableLine(t *testing.T) {
	t.Parallel()

	assert.False(t, orderOf().ReadyToClose())
	assert.False(t, orderOf(hidden()).ReadyToClose())
}
