package testusers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/testusers"
)

func TestParse(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"blackbox.vietgara.test", " QA.Example ", "x.invalid", "a-b.localhost"} {
		d, err := testusers.Parse(raw)
		require.NoError(t, err, raw)
		assert.False(t, d.IsZero())
	}

	for _, raw := range []string{
		"", "test", ".test", "gmail.com", "viettechno.com", "evil.test.com", "a..test", "a_b.test", "a@b.test", "x.testing",
	} {
		_, err := testusers.Parse(raw)
		require.ErrorIs(t, err, testusers.ErrDomain, raw)
	}
}

func TestOwns(t *testing.T) {
	t.Parallel()

	d, err := testusers.Parse("blackbox.vietgara.test")
	require.NoError(t, err)

	assert.Equal(t, "blackbox.vietgara.test", d.String())
	assert.True(t, d.Owns("bb.owner.1@blackbox.vietgara.test"))
	assert.True(t, d.Owns(" BB@Blackbox.Vietgara.Test "))
	assert.False(t, d.Owns("someone@vietgara.test"), "a parent domain is not the test domain")
	assert.False(t, d.Owns("x@evilblackbox.vietgara.test"), "only the exact domain")
	assert.False(t, d.Owns("blackbox.vietgara.test"))

	var zero testusers.Domain
	assert.True(t, zero.IsZero())
	assert.False(t, zero.Owns("a@blackbox.vietgara.test"))
}
