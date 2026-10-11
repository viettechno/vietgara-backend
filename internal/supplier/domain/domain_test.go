package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
)

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("name", "Nguyễn", 6))

	err := domain.CheckLength("name", "Nguyễn Văn", 6)
	require.ErrorIs(t, err, domain.ErrFieldTooLong)
	assert.EqualError(t, err, "name must be at most 6 characters")
}
