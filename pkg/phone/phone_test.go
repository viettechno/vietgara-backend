package phone_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/phone"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "national mobile", in: "0901234567", want: "+84901234567"},
		{name: "national landline", in: "028 1234 5678", want: "+842812345678"},
		{name: "international with plus", in: "+84 90-123.4567", want: "+84901234567"},
		{name: "country code without plus", in: "84901234567", want: "+84901234567"},
		{name: "brackets and surrounding space", in: "  (090) 123 4567 ", want: "+84901234567"},
		{name: "another country", in: "+1 (415) 555-0132", want: "+14155550132"},
		{name: "already normalized", in: "+84901234567", want: "+84901234567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := phone.Normalize(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalize_Invalid(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"",
		"   ",
		"abc",
		"0912",                // too short
		"090123456789012",     // national number too long
		"09123abc78",          // letters
		"0912+345678",         // plus in the middle
		"+0841234567",         // country code cannot start with 0
		"+1234567",            // too short for E.164
		"+1234567890123456",   // too long for E.164
		"901234567",           // no 0, no country code, no plus
		"8490123",             // country code but too short
		"849012345678901",     // country code but too long
		"+84 90 123 45 6A",    // letter after the plus
		"0901234567; DROP --", // junk
	} {
		_, err := phone.Normalize(in)
		require.ErrorIs(t, err, phone.ErrInvalid, in)
	}
}

func TestSearchKey(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"0912 345":      "912345",
		"+84 912 345":   "912345",
		"84912":         "84912",
		"912":           "912",
		"0":             "",
		"":              "",
		"  ":            "",
		"abc":           "",
		"51F-123.45":    "",
		"0912x345":      "",
		"(091) 234-567": "91234567",
	} {
		assert.Equal(t, want, phone.SearchKey(in), in)
	}
}
