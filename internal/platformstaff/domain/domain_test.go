package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/platformstaff/domain"
)

func TestValidName(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ValidName("Billing support", domain.MaxRoleNameLength))
	assert.False(t, domain.ValidName("", domain.MaxRoleNameLength))
	assert.False(t, domain.ValidName(string(make([]rune, 101)), domain.MaxRoleNameLength))
	assert.True(t, domain.ValidName("Kỹ thuật viên", 13), "length is counted in characters")
}

func TestInvitationLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	fresh := func() *domain.Invitation {
		return &domain.Invitation{Status: domain.InvitationPending, ExpiresAt: now.Add(domain.InvitationTTL)}
	}

	for name, respond := range map[string]func(*domain.Invitation) error{
		"accept":  func(i *domain.Invitation) error { return i.Accept(now) },
		"decline": func(i *domain.Invitation) error { return i.Decline(now) },
		"revoke":  func(i *domain.Invitation) error { return i.Revoke(now) },
	} {
		inv := fresh()
		require.NoError(t, respond(inv), name)
		require.NotNil(t, inv.RespondedAt)
		assert.NotEqual(t, domain.InvitationPending, inv.Status)
		require.ErrorIs(t, respond(inv), domain.ErrInvitationNotPending, "%s twice", name)
	}

	expired := fresh()
	assert.Equal(t, domain.InvitationExpired, expired.EffectiveStatus(now.Add(domain.InvitationTTL)))
	require.ErrorIs(t, expired.Accept(now.Add(domain.InvitationTTL)), domain.ErrInvitationExpired)
	assert.Equal(t, domain.InvitationPending, expired.Status, "an expired invitation stays unchanged")

	assert.True(t, domain.InvitationRevoked.IsValid())
	assert.False(t, domain.InvitationStatus("nope").IsValid())

	odd := &domain.Invitation{Status: "weird", ExpiresAt: now.Add(time.Hour)}
	require.ErrorIs(t, odd.Accept(now), domain.ErrInvitationNotPending)
}
