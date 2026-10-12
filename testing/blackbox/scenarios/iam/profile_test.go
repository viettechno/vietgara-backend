package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-019 (partial): the signed-in user edits its own
// profile; a taken phone is refused and the e-mail is not editable. Page
// copy is web-E2E.
func TestEditOwnProfile(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	api := fixture.Anonymous(t)

	owner := fixture.NewVerifiedOwner(t, "profile")
	phone := "0900000901"
	edited, err := owner.API.UpdateMe(ctx, &identityv1.User{FullName: "Blackbox Edited", Phone: &phone},
		"full_name", "phone")
	require.NoError(t, err)
	assert.Equal(t, "Blackbox Edited", edited.GetFullName())
	// Phones are stored normalized to E.164.
	assert.Equal(t, "+84900000901", edited.GetPhone())

	me, err := owner.API.Me(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Blackbox Edited", me.GetFullName())
	assert.Equal(t, "+84900000901", me.GetPhone())

	// A taken phone belongs to another user: refused.
	otherPhone := "0900000902"
	_, err = api.Register(ctx, &identityv1.RegisterRequest{
		Email: fixture.UniqueEmail(t, "profile-other"), Password: fixture.Password,
		FullName: "Blackbox Other", Phone: &otherPhone,
	})
	require.NoError(t, err)

	_, err = owner.API.UpdateMe(ctx, &identityv1.User{Phone: &otherPhone}, "phone")
	fixture.RequireAPIError(t, err, http.StatusConflict, "PHONE_TAKEN")

	// The e-mail cannot be edited: it is ignored...
	renamed, err := owner.API.UpdateMe(ctx, &identityv1.User{
		FullName: "Blackbox Edited", Email: "new.address@vietgara.test",
	}, "full_name")
	require.NoError(t, err)
	assert.Equal(t, owner.Email, renamed.GetEmail())

	// ...and rejected as a mask path.
	_, err = owner.API.UpdateMe(ctx, &identityv1.User{Email: "new.address@vietgara.test"}, "email")
	fixture.RequireAPIError(t, err, http.StatusUnprocessableEntity, "VALIDATION_ERROR")
}

// Scenario: E2E-IAM-018 (partial): the interface language is saved on the
// user, and e-mails follow it. Switching the UI itself is web-E2E.
func TestInterfaceLanguage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	mail := fixture.Mailpit(t)
	api := fixture.Anonymous(t)

	owner := fixture.NewVerifiedOwner(t, "locale")

	english, err := owner.API.UpdateMe(ctx, &identityv1.User{Locale: identityv1.Locale_LOCALE_EN}, "locale")
	require.NoError(t, err)
	assert.Equal(t, identityv1.Locale_LOCALE_EN, english.GetLocale())

	me, err := owner.API.Me(ctx)
	require.NoError(t, err)
	assert.Equal(t, identityv1.Locale_LOCALE_EN, me.GetLocale())

	// The reset e-mail follows the saved language (stops before changing
	// the password, as the scenario does).
	_, err = api.RequestPasswordReset(ctx, owner.Email)
	require.NoError(t, err)

	msg, err := mail.WaitForMessage(ctx, owner.Email)
	require.NoError(t, err)
	assert.Contains(t, msg.Subject, "Your VietGara password reset code:")
	assert.Contains(t, msg.Text, "Hello "+owner.Session.GetUser().GetFullName())

	back, err := owner.API.UpdateMe(ctx, &identityv1.User{Locale: identityv1.Locale_LOCALE_VI}, "locale")
	require.NoError(t, err)
	assert.Equal(t, identityv1.Locale_LOCALE_VI, back.GetLocale())
}
