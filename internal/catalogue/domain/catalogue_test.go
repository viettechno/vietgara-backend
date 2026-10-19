package domain_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/catalogue/domain"
)

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("abc", 3))
	require.NoError(t, domain.CheckLength("損害賠償額", 5))
	require.ErrorIs(t, domain.CheckLength("損害賠償額", 3), domain.ErrFieldTooLong)
}

func TestParseDecimal(t *testing.T) {
	t.Parallel()

	r, ok := domain.ParseDecimal("  2.5 ")
	require.True(t, ok, "surrounding blanks are trimmed")
	assert.Equal(t, "5/2", r.String())

	for in, want := range map[string]string{"0": "0/1", "12": "12/1", "1.5": "3/2", "2.25": "9/4", "0.001": "1/1000"} {
		r, ok := domain.ParseDecimal(in)
		require.True(t, ok, in)
		assert.Equal(t, want, r.String(), in)
	}

	for _, bad := range []string{"", " ", "abc", "-1", "+1", "1e3", "1 000", "1/2", ".", "5.", ".5", "0x10", "1_0"} {
		_, ok := domain.ParseDecimal(bad)
		assert.False(t, ok, bad)
	}
}

func TestFormatDecimal(t *testing.T) {
	t.Parallel()

	for want, r := range map[string]*big.Rat{"240": big.NewRat(240, 1), "8.75": big.NewRat(35, 4), "0.001": big.NewRat(1, 1000), "0": big.NewRat(0, 1)} {
		got, ok := domain.FormatDecimal(r)
		require.True(t, ok)
		assert.Equal(t, want, got)
	}

	_, ok := domain.FormatDecimal(big.NewRat(1, 3))
	assert.False(t, ok)

	_, ok = domain.FormatDecimal(big.NewRat(1, 10000))
	assert.False(t, ok)
}

func TestConvertToBase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entered, factor, want string
	}{
		{"10", "24", "240"},
		{"1", "1", "1"},
		{"0.5", "24", "12"},
		{"2", "0.25", "0.5"},
		{"0.125", "1", "0.125"},
		{"1.5", "12.5", "18.75"},
	}

	for _, tt := range tests {
		got, err := domain.ConvertToBase(tt.entered, tt.factor)
		require.NoError(t, err, tt.entered)
		assert.Equal(t, tt.want, got, tt.entered+" x "+tt.factor)
	}

	_, err := domain.ConvertToBase("0.0005", "1")
	require.ErrorIs(t, err, domain.ErrQuantityPrecision, "more than three decimals is refused")

	_, err = domain.ConvertToBase("1", "0.0005")
	require.ErrorIs(t, err, domain.ErrQuantityPrecision)

	for _, bad := range []string{"", "0", "-1", "abc"} {
		_, err = domain.ConvertToBase(bad, "24")
		require.ErrorIs(t, err, domain.ErrInvalidQuantity, bad)
	}

	for _, bad := range []string{"", "0", "-1", "abc"} {
		_, err = domain.ConvertToBase("1", bad)
		require.ErrorIs(t, err, domain.ErrInvalidFactor, bad)
	}
}

func TestNormalizeFactor(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{"24": "24", "24.000": "24", "0.5": "0.5", "1.123456": "1.123456", "12": "12"} {
		got, err := domain.NormalizeFactor(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", "0", "-2", "x", "1.1234567"} {
		_, err := domain.NormalizeFactor(bad)
		require.ErrorIs(t, err, domain.ErrInvalidFactor, bad)
	}
}

func TestNormalizeThreshold(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{"": "0", "  ": "0", "0": "0", "5": "5", "5.500": "5.5", "0.001": "0.001"} {
		got, err := domain.NormalizeThreshold(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"-1", "abc", "1.2345"} {
		_, err := domain.NormalizeThreshold(bad)
		require.ErrorIs(t, err, domain.ErrInvalidThreshold, bad)
	}
}

func TestDerivedPrice(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(360_000), domain.DerivedPrice(15_000, "24"))
	assert.Equal(t, int64(7_500), domain.DerivedPrice(15_000, "0.5"))
	assert.Equal(t, int64(2), domain.DerivedPrice(3, "0.5"), "half a dong rounds up")
	assert.Equal(t, int64(0), domain.DerivedPrice(15_000, "bad"))
	assert.Equal(t, int64(0), domain.DerivedPrice(0, "24"))
}

func TestNormalizeBarcode(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"8934567890123": "8934567890123", "  AB-12/3.4+5$6%7 8  ": "AB-12/3.4+5$6%7 8", "abc": "abc",
	} {
		got, err := domain.NormalizeBarcode(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	tooLong := strings.Repeat("9", domain.MaxBarcodeLen+1)

	for _, bad := range []string{"", "   ", "ab_cd", "ab@cd", "ab;cd", "tên", tooLong} {
		_, err := domain.NormalizeBarcode(bad)
		require.ErrorIs(t, err, domain.ErrBarcodeInvalid, bad)
	}

	_, err := domain.NormalizeBarcode(strings.Repeat("9", domain.MaxBarcodeLen))
	require.NoError(t, err, "64 characters is allowed")
}
