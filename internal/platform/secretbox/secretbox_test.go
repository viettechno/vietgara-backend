package secretbox_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platform/secretbox"
)

var testKey = strings.Repeat("ab", secretbox.KeySize)

func TestSealOpen(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(testKey)
	require.NoError(t, err)

	first, err := box.Seal([]byte("totp-secret"))
	require.NoError(t, err)

	second, err := box.Seal([]byte("totp-secret"))
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "random nonce")
	assert.NotContains(t, string(first), "totp-secret")

	plain, err := box.Open(first)
	require.NoError(t, err)
	assert.Equal(t, "totp-secret", string(plain))
}

func TestOpenRejectsTamperedAndForeign(t *testing.T) {
	t.Parallel()

	box, err := secretbox.New(testKey)
	require.NoError(t, err)

	sealed, err := box.Seal([]byte("x"))
	require.NoError(t, err)

	tampered := append([]byte(nil), sealed...)
	tampered[len(tampered)-1] ^= 1
	_, err = box.Open(tampered)
	require.ErrorIs(t, err, secretbox.ErrCorrupt)

	_, err = box.Open([]byte("short"))
	require.ErrorIs(t, err, secretbox.ErrCorrupt)

	other, err := secretbox.New(strings.Repeat("cd", secretbox.KeySize))
	require.NoError(t, err)
	_, err = other.Open(sealed)
	require.ErrorIs(t, err, secretbox.ErrCorrupt)
}

func TestNewRejectsBadKeys(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", "zz", strings.Repeat("ab", 16), strings.Repeat("ab", 33)} {
		_, err := secretbox.New(key)
		require.ErrorIs(t, err, secretbox.ErrInvalidKey, key)
	}
}
