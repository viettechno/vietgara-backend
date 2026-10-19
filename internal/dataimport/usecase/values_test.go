package usecase //nolint:testpackage // white-box tests of the unexported value parsers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitList(t *testing.T) {
	require.Equal(t, []string{"a", "b", "c"}, splitList(" a ; b ;c;; "))
	require.Empty(t, splitList(" ; "))
}

func TestParseQuantity(t *testing.T) {
	tests := []struct {
		in        string
		aboveZero bool
		want      string
		ok        bool
	}{
		{"10", false, "10", true},
		{"10.500", false, "10.5", true},
		{"0", false, "0", true},
		{"0", true, "", false},
		{"1.001", true, "1.001", true},
		{"1.0001", false, "", false},
		{"-1", false, "", false},
		{"abc", false, "", false},
		{"", false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseQuantity(tt.in, tt.aboveZero)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseCost(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"", 0, true},
		{"1000", 1000, true},
		{"1.000", 1000, true},
		{"1 000 đ", 1000, true},
		{"1.250.000", 1250000, true},
		{"1,250,000", 1250000, true},
		{"25000₫", 25000, true},
		{"1.5", 0, false},
		{"12,50", 0, false},
		{"0.5", 0, false},
		{"1.2500", 0, false},
		{"1.25.0", 0, false},
		{"-1", 0, false},
		{"abc", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseCost(tt.in)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"2026-02-03", "2026-02-03", true},
		{"03/02/2026", "2026-02-03", true},
		{"2026/02/03", "2026-02-03", true},
		{"46056", "2026-02-03", true},
		{"31/13/2020", "", false},
		{"not a date", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseDate(tt.in)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidBarcode(t *testing.T) {
	require.True(t, validBarcode("ABC-123 . /+$%"))
	require.False(t, validBarcode(""))
	require.False(t, validBarcode("AB@C"))
	require.False(t, validBarcode(string(make([]rune, 65))))
}

func TestValidateUnitName(t *testing.T) {
	require.True(t, validateUnitName("Case"))
	require.False(t, validateUnitName(""))
}

func TestUnitAbbreviation(t *testing.T) {
	got, ok := unitAbbreviation("Case")
	require.True(t, ok)
	require.Equal(t, "Case", got)

	_, ok = unitAbbreviation(string(make([]rune, 21)))
	require.False(t, ok)
}

func TestParseFactor(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"24", "24", true},
		{"1.500000", "1.5", true},
		{"0", "", false},
		{"1.1234567", "", false},
		{"abc", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, ok := parseFactor(tt.in)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestExcelDateLeapBug(t *testing.T) {
	// Serial 60 is the phantom 29 Feb 1900; 59 is 28 Feb 1900.
	d, err := excelDate(59)
	require.NoError(t, err)
	require.Equal(t, "1900-02-28", d.Format("2006-01-02"))

	d, err = excelDate(61)
	require.NoError(t, err)
	require.Equal(t, "1900-03-01", d.Format("2006-01-02"))
}
