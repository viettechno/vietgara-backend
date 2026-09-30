package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

func TestRepairOrderStatus_IsValid(t *testing.T) {
	t.Parallel()

	for _, s := range []domain.RepairOrderStatus{domain.StatusOpen, domain.StatusInProgress, domain.StatusCompleted, domain.StatusClosed} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.RepairOrderStatus("cancelled").IsValid())
	assert.False(t, domain.RepairOrderStatus("").IsValid())
}
