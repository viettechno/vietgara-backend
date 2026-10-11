package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/supplier/domain"
)

func TestNormalizePhone(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"0912345678":         "0912345678",
		"+84 912-345-678":    "0912345678",
		" (028) 1234.5678 ":  "02812345678",
		"+442071234567":      "+442071234567",
		"+84 (0) 912 345 67": "0091234567",
	} {
		got, err := domain.NormalizePhone(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, in := range []string{"", "abc", "1234567", "1234567890123456", "0912-ABC-678"} {
		_, err := domain.NormalizePhone(in)
		require.ErrorIs(t, err, domain.ErrInvalidPhone, in)
	}
}

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	got, err := domain.NormalizeEmail("  Sales@PhuTung.VN ")
	require.NoError(t, err)
	assert.Equal(t, "sales@phutung.vn", got)

	for _, in := range []string{"", "sales", "Sales <sales@phutung.vn>"} {
		_, err := domain.NormalizeEmail(in)
		require.ErrorIs(t, err, domain.ErrInvalidEmail, in)
	}

	_, err = domain.NormalizeEmail("a@" + string(make([]byte, 300)))
	require.ErrorIs(t, err, domain.ErrInvalidEmail)
}

func TestCheckLength(t *testing.T) {
	t.Parallel()

	require.NoError(t, domain.CheckLength("name", "Nguyễn", 6))

	err := domain.CheckLength("name", "Nguyễn Văn", 6)
	require.ErrorIs(t, err, domain.ErrFieldTooLong)
	assert.EqualError(t, err, "name must be at most 6 characters")
}
