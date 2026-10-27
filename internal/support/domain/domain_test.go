package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/support/domain"
)

func TestGrantLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)

	g := domain.NewGrant("g", "u", "r", 2*time.Hour, false, now)
	assert.Equal(t, domain.StatusPending, g.Status(now))
	assert.Equal(t, domain.StatusExpired, g.Status(now.Add(domain.RequestLifetime+time.Second)))

	later := now.Add(time.Hour)
	require.NoError(t, g.Approve("owner", later))
	assert.Equal(t, domain.StatusActive, g.Status(later))
	assert.Equal(t, later.Add(2*time.Hour), *g.ExpiresAt)
	assert.Equal(t, domain.StatusExpired, g.Status(later.Add(2*time.Hour)))
	require.ErrorIs(t, g.Approve("owner", later), domain.ErrGrantNotPending)

	require.NoError(t, g.Revoke(later))
	assert.Equal(t, domain.StatusRevoked, g.Status(later))
	require.ErrorIs(t, g.Revoke(later), domain.ErrGrantNotOpen)

	admin := domain.NewGrant("g", "u", "r", time.Hour, true, now)
	assert.Equal(t, domain.StatusActive, admin.Status(now))
	assert.True(t, admin.GrantedByPlatformAdmin)
}

func TestValidTTLAndPage(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ValidTTL(domain.MinTTL))
	assert.True(t, domain.ValidTTL(domain.MaxTTL))
	assert.False(t, domain.ValidTTL(domain.MinTTL-1))
	assert.False(t, domain.ValidTTL(domain.MaxTTL+1))
	assert.Equal(t, 40, domain.Page{Page: 3, PageSize: 20}.Offset())
}
