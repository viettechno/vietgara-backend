package converter_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/converter"
)

func TestFormatVND(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		amount int64
		want   string
	}{
		{name: "zero", amount: 0, want: "0"},
		{name: "hundreds", amount: 250, want: "250"},
		{name: "thousands", amount: 730_000, want: "730.000"},
		{name: "millions", amount: 12_345_678, want: "12.345.678"},
		{name: "negative", amount: -1_000, want: "-1.000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, converter.FormatVND(tt.amount))
		})
	}
}
