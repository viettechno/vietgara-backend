package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
)

func TestStatuses(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.GarageStatusActive.IsValid())
	assert.False(t, domain.GarageStatus("suspended").IsValid())
	assert.True(t, domain.StaffStatusInactive.IsValid())
	assert.False(t, domain.StaffStatus("removed").IsValid())

	for _, s := range []domain.InvitationStatus{domain.InvitationPending, domain.InvitationAccepted, domain.InvitationDeclined, domain.InvitationRevoked, domain.InvitationExpired} {
		assert.True(t, s.IsValid())
	}

	assert.False(t, domain.InvitationStatus("x").IsValid())

	assert.True(t, domain.QuoteApprovalModeLink.IsValid())
	assert.True(t, domain.QuoteApprovalModeManual.IsValid())
	assert.False(t, domain.QuoteApprovalMode("email").IsValid())
}

func TestGarageValidation(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.ValidName("Gara"))
	assert.False(t, domain.ValidName(""))
	assert.False(t, domain.ValidName(strings.Repeat("x", 256)))

	assert.True(t, domain.ValidTimezone("Asia/Ho_Chi_Minh"))
	assert.False(t, domain.ValidTimezone("Mars/Base"))
	assert.False(t, domain.ValidTimezone(""))
	assert.False(t, domain.ValidTimezone("Local"))
	assert.False(t, domain.ValidTimezone(strings.Repeat("x", 65)))

	assert.True(t, domain.ValidCurrency("VND"))
	assert.False(t, domain.ValidCurrency("vnd"))

	hours := domain.DefaultOperatingHours()
	assert.Len(t, hours, 7)
	assert.True(t, domain.ValidOperatingHours(hours))
	assert.True(t, hours[6].Closed)

	for _, bad := range [][]domain.DailyHours{
		{{Day: 0, Open: "08:00", Close: "09:00"}},
		{{Day: 1, Open: "08:00", Close: "09:00"}, {Day: 1, Closed: true}},
		{{Day: 2, Open: "8:00", Close: "09:00"}},
		{{Day: 3, Open: "10:00", Close: "09:00"}},
	} {
		assert.False(t, domain.ValidOperatingHours(bad), bad)
	}

	for tmpl, want := range map[string]bool{
		domain.DefaultQuoteNumberTemplate:   true,
		"Q-{YY}{DD}-{SEQ:3}":                true,
		"Q-{SEQ:3}-{SEQ:2}":                 false,
		"Q-{YYYY}":                          false,
		"Q-{HH}-{SEQ:3}":                    false,
		"Q-{SEQ:0}":                         false,
		"Q-{SEQ:3}}":                        false,
		"":                                  false,
		strings.Repeat("x", 45) + "{SEQ:3}": false,
	} {
		assert.Equal(t, want, domain.ValidNumberingTemplate(tmpl), tmpl)
	}
}

func TestDefaultRoles(t *testing.T) {
	t.Parallel()

	assert.Len(t, domain.DefaultRoleTemplates, 5)
	assert.Len(t, domain.DefaultRoleTemplates[0].Permissions, 17)

	name, desc := domain.DefaultRoleTemplates[1].Localized("en")
	assert.Equal(t, "Accountant", name)
	assert.NotEmpty(t, desc)

	name, _ = domain.DefaultRoleTemplates[1].Localized("fr")
	assert.Equal(t, "Kế toán", name)

	tmpl, ok := domain.DefaultTechnician.Template()
	require.True(t, ok)
	assert.Equal(t, domain.DefaultTechnician, tmpl.Key)

	_, ok = domain.DefaultRole("cashier").Template()
	assert.False(t, ok)

	assert.True(t, domain.ValidRoleName("Kỹ thuật", domain.MaxRoleNameLength))
	assert.False(t, domain.ValidRoleName("", domain.MaxRoleNameLength))
}

func TestInvitationLifecycle(t *testing.T) {
	t.Parallel()

	now := time.Now()
	inv := func() *domain.Invitation {
		return &domain.Invitation{Status: domain.InvitationPending, ExpiresAt: now.Add(domain.InvitationTTL)}
	}

	accepted := inv()
	require.NoError(t, accepted.Accept(now))
	assert.Equal(t, domain.InvitationAccepted, accepted.Status)
	assert.Equal(t, now, *accepted.RespondedAt)
	require.ErrorIs(t, accepted.Decline(now), domain.ErrInvitationNotPending)

	declined := inv()
	require.NoError(t, declined.Decline(now))

	revoked := inv()
	require.NoError(t, revoked.Revoke(now))
	assert.Equal(t, domain.InvitationRevoked, revoked.Status)

	expired := inv()
	later := now.Add(domain.InvitationTTL)
	assert.Equal(t, domain.InvitationExpired, expired.EffectiveStatus(later))
	require.ErrorIs(t, expired.Accept(later), domain.ErrInvitationExpired)
	assert.Equal(t, domain.InvitationPending, expired.EffectiveStatus(now))
}
