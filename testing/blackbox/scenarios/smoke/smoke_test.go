// Package smoke_test holds read-only checks that run on every target,
// including staging: they create nothing and change nothing.
package smoke_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

func TestProtectedAPIsRejectAnonymousCalls(t *testing.T) {
	t.Parallel()

	_, err := fixture.Anonymous(t).Me(t.Context())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "UNAUTHENTICATED")

	_, err = fixture.Anonymous(t).WithToken("not-a-jwt").ListGarages(t.Context())
	fixture.RequireAPIError(t, err, http.StatusUnauthorized, "UNAUTHENTICATED")
}

func TestOwnerReadsUserGaragesAndSubscription(t *testing.T) {
	t.Parallel()

	owner := fixture.Owner(t)

	me, err := owner.API.Me(t.Context())
	require.NoError(t, err)
	assert.Equal(t, owner.Email, me.GetEmail())
	assert.True(t, me.GetEmailVerified())

	_, err = owner.API.ListGarages(t.Context())
	require.NoError(t, err)

	sub, err := owner.API.MySubscription(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, sub.GetPlan().GetCode(), "every owner has a plan, a trial at least")
}
