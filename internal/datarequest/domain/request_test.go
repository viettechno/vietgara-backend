package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viettechno/vietgara-backend/internal/datarequest/domain"
)

func TestType_IsValid(t *testing.T) {
	t.Parallel()

	for _, typ := range []domain.Type{domain.TypeView, domain.TypeCorrect, domain.TypeDelete} {
		assert.True(t, typ.IsValid(), typ)
	}

	assert.False(t, domain.Type("erase").IsValid())
	assert.False(t, domain.Type("").IsValid())
}

func TestRequest_Pending(t *testing.T) {
	t.Parallel()

	assert.True(t, (&domain.Request{Status: domain.StatusPending}).Pending())
	assert.False(t, (&domain.Request{Status: domain.StatusCompleted}).Pending())
	assert.False(t, (&domain.Request{Status: domain.StatusRejected}).Pending())
}
