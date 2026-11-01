package system_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// TestOutsidersCannotReachAnotherGarage: a signed-in user who is not a
// member of a garage is turned away from every garage-scoped endpoint, and
// an anonymous caller from all of them, whatever the method. The check is
// on the status only: 403 or 404, never a 2xx, a validation error (the
// request must be refused before it is read) or a server error.
//
// Scenario: E2E-SYS-001: every endpoint under /garages/{garageId} refuses
// a user outside the garage.
func TestOutsidersCannotReachAnotherGarage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	home := fixture.NewBusiness(t, "iso-home")
	outsider := fixture.NewBusiness(t, "iso-out")

	garageOps := 0
	declaredButNotServed := "GET /api/v1/garages/{garageId}/customers/lookup"

	for _, op := range operations(t) {
		if !op.garageScoped() {
			continue
		}

		garageOps++

		t.Run(op.String(), func(t *testing.T) {
			t.Parallel()

			target := op.url(home.GarageID)

			if op.String() == declaredButNotServed {
				// The contract declares it (FR-CUS-06, Backlog G-23) but no
				// handler or policy serves it yet: every caller, the owner included,
				// is refused with NO_POLICY. When it is implemented this
				// branch fails and must go.
				own, err := home.Owner.API.Probe(ctx, op.Method, target, nil)
				require.NoError(t, err)
				assert.Equal(t, http.StatusForbidden, own.Status)
				assert.Contains(t, string(own.Body), "NO_POLICY", "the endpoint is served now: remove this exception")

				return
			}

			probe, err := outsider.Owner.API.Probe(ctx, op.Method, target, op.body())
			require.NoError(t, err)
			assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, probe.Status,
				"an outsider got %d: %s", probe.Status, probe.Body)

			anonymous, err := fixture.Anonymous(t).Probe(ctx, op.Method, target, op.body())
			require.NoError(t, err)
			assert.Equal(t, http.StatusUnauthorized, anonymous.Status, "an anonymous caller got %d: %s", anonymous.Status, anonymous.Body)
		})
	}

	require.Greater(t, garageOps, 100, "the garage-scoped operations were found")
}
