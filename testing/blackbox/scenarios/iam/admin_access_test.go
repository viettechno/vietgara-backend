package iam_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// Scenario: E2E-IAM-020 (partial): the platform admin API serves the
// platform admin, and refuses an owner and an anonymous caller — the
// backend is the authority behind the admin web's access-denied page.
// The pages and the owner web's missing /admin routes are web-E2E.
func TestPlatformAdminAPIIsAdminOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	admin := fixture.PlatformAdmin(t)
	owner := fixture.NewVerifiedOwner(t, "iam020")

	plans, err := admin.API.ListAllPlans(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, plans)

	_, err = admin.API.ListSubscriptions(ctx, "", 0)
	require.NoError(t, err)

	_, err = owner.API.ListAllPlans(ctx)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = owner.API.ListSubscriptions(ctx, "", 0)
	fixture.RequireAPIError(t, err, http.StatusForbidden, "")

	_, err = fixture.Anonymous(t).ListAllPlans(ctx)
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "")
}
