package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/repairorder/domain"
)

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("", 5))
	require.NoError(t, domain.CheckLength("abc", 5))
	require.NoError(t, domain.CheckLength("損害賠", 3))
	require.ErrorIs(t, domain.CheckLength("abcdef", 5), domain.ErrFieldTooLong)
	require.ErrorIs(t, domain.CheckLength("損害賠償額", 3), domain.ErrFieldTooLong)
}

func TestParseQuantity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  int64
	}{
		{"whole", "1", 1000},
		{"fraction", "1.5", 1500},
		{"two decimals", "1.99", 1990},
		{"three decimals", "0.125", 125},
		{"padded", " 2 ", 2000},
		{"max digits", "9999999", 9999999000},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := domain.ParseQuantity(c.input)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	invalid := []string{"", "0", "0.00", "abc", "1.9999", "12345678", "-1", ".5", "1.", "1,5"}

	for _, input := range invalid {
		t.Run("invalid "+input, func(t *testing.T) {
			t.Parallel()

			_, err := domain.ParseQuantity(input)
			require.ErrorIs(t, err, domain.ErrInvalidQuantity)
		})
	}
}

func TestLineTotal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(150_000), domain.LineTotal(1500, 100_000))
	assert.Equal(t, int64(0), domain.LineTotal(0, 100_000))
	// Half a VND rounds up.
	assert.Equal(t, int64(1), domain.LineTotal(5, 100))
	assert.Equal(t, int64(0), domain.LineTotal(4, 100))
	// 0.125 x 1,000 = 125.
	assert.Equal(t, int64(125), domain.LineTotal(125, 1000))
}
