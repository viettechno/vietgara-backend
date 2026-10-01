package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/inventory/domain"
)

func TestItem_LowStock(t *testing.T) {
	t.Parallel()

	assert.True(t, (&domain.Item{QuantityOnHand: "2", ReorderThreshold: 5}).LowStock())
	assert.True(t, (&domain.Item{QuantityOnHand: "5", ReorderThreshold: 5}).LowStock())
	assert.False(t, (&domain.Item{QuantityOnHand: "6", ReorderThreshold: 5}).LowStock())
	assert.False(t, (&domain.Item{QuantityOnHand: "not-a-number", ReorderThreshold: 5}).LowStock())
}

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("short", 10))
	require.ErrorIs(t, domain.CheckLength("too long a value", 5), domain.ErrFieldTooLong)
}

func TestParseQuantity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "integer", in: "3", want: "3"},
		{name: "two decimals", in: "1.25", want: "1.25"},
		{name: "trims whitespace", in: " 2 ", want: "2"},
		{name: "zero rejected", in: "0", wantErr: true},
		{name: "negative rejected", in: "-1", wantErr: true},
		{name: "too many decimals rejected", in: "1.234", wantErr: true},
		{name: "not a number rejected", in: "abc", wantErr: true},
		{name: "empty rejected", in: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseQuantity(tt.in)
			if tt.wantErr {
				require.ErrorIs(t, err, domain.ErrInvalidQuantity)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
