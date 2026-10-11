package email_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/pkg/email"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	got, err := email.Normalize("  Sales@PhuTung.VN ")
	require.NoError(t, err)
	assert.Equal(t, "sales@phutung.vn", got)

	got, err = email.Normalize("a+tag@sub.mail.vn")
	require.NoError(t, err)
	assert.Equal(t, "a+tag@sub.mail.vn", got)
}

func TestNormalize_Invalid(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"",
		"   ",
		"sales",
		"sales@",
		"@phutung.vn",
		"sales@localhost",
		"Sales <sales@phutung.vn>",
		"a@b.vn, c@d.vn",
		strings.Repeat("a", 250) + "@mail.vn",
	} {
		_, err := email.Normalize(in)
		require.ErrorIs(t, err, email.ErrInvalid, in)
	}
}
