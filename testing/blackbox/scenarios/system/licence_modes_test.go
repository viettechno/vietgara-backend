package system_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"

	"github.com/viettechno/vietgara-backend/testing/blackbox/env"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
	"github.com/viettechno/vietgara-backend/testing/blackbox/timeshift"
)

const modeWait = 90 * time.Second

// exemptFromLicense are the operations the licence check skips on purpose:
// the owner can always read and edit the garage's own profile and settings
// and see the entitlements that explain the banner, and the support grants
// stay manageable so an incident can still be handled.
var exemptFromLicense = map[string]bool{
	"GET /api/v1/garages/{garageId}":                                   true,
	"PATCH /api/v1/garages/{garageId}":                                 true,
	"GET /api/v1/garages/{garageId}/entitlements":                      true,
	"GET /api/v1/garages/{garageId}/support-access-log":                true,
	"GET /api/v1/garages/{garageId}/support-grants":                    true,
	"POST /api/v1/garages/{garageId}/support-grants/{grantId}:approve": true,
	"POST /api/v1/garages/{garageId}/support-grants/{grantId}:revoke":  true,
	"GET /api/v1/garages/{garageId}/customers/lookup":                  true, // declared, not served: see TestOutsidersCannotReachAnotherGarage
}

// downloads are the reads that return a file. The read-only rule treats a
// method as a read only when its name starts with Get, List or Lookup, and
// these are named Download or Export, so they are refused with
// SUBSCRIPTION_READ_ONLY although Business Procedures P2 says a past-due
// owner can "read everything". Backlog G-22 tracks the fix; when it lands
// this list empties and the subtest below fails until the exception goes.
func isDownload(o operation) bool {
	return o.Method == http.MethodGet && (strings.HasSuffix(o.Path, "/pdf") || strings.HasSuffix(o.Path, "-pdf") ||
		strings.HasSuffix(o.Path, "/excel") || strings.HasSuffix(o.Path, "/template"))
}

func isLicenceError(body []byte) bool {
	text := string(body)

	return strings.Contains(text, "SUBSCRIPTION_READ_ONLY") || strings.Contains(text, "SUBSCRIPTION_SUSPENDED")
}

// TestReadOnlyAndSuspendedModesCoverEveryEndpoint: when an owner's period
// ends, every garage-scoped endpoint that changes data is refused with
// SUBSCRIPTION_READ_ONLY (reads keep working); when the grace period ends
// every endpoint is refused with SUBSCRIPTION_SUSPENDED. The two modes are
// checked on the same garage, one after the other, across the whole API.
//
// Scenario: E2E-SYS-003: read-only and suspended modes refuse every write
// and, once suspended, every read, on every garage-scoped endpoint.
func TestReadOnlyAndSuspendedModesCoverEveryEndpoint(t *testing.T) {
	t.Parallel()

	env.RequireStubbed(t, "moves the stored subscription dates")

	ctx := t.Context()
	business := fixture.NewBusiness(t, "modes")

	owner := business.Owner
	ops := operations(t)

	// Past due: read-only.
	timeshift.EndPeriod(t, owner.Email)
	fixture.WaitForSubscriptionStatus(t, owner, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_PAST_DUE, modeWait)
	waitForEntitlement(t, business, func(readOnly bool) bool { return readOnly })

	t.Run("read-only", func(t *testing.T) {
		var downloads []string

		for _, op := range ops {
			if !op.garageScoped() || exemptFromLicense[op.String()] {
				continue
			}

			probe, err := owner.API.Probe(ctx, op.Method, op.url(business.GarageID), op.body())
			require.NoError(t, err, op.String())

			switch {
			case op.writes():
				assert.Equal(t, http.StatusForbidden, probe.Status, "%s: %s", op, probe.Body)
				assert.Contains(t, string(probe.Body), "SUBSCRIPTION_READ_ONLY", "%s", op)
			case isDownload(op):
				if isLicenceError(probe.Body) {
					downloads = append(downloads, op.String())
				}
			default:
				assert.False(t, isLicenceError(probe.Body), "a read is refused in read-only mode: %s: %s", op, probe.Body)
			}
		}

		// Known gap (G-22): every file download is refused. Delete this
		// expectation, and assert the downloads are not refused above,
		// when the rule is fixed.
		assert.NotEmpty(t, downloads, "downloads are no longer refused in read-only mode: make them reads in the loop above")
		t.Logf("downloads refused while read-only (G-22): %d", len(downloads))
	})

	// Suspended: locked.
	timeshift.EndGrace(t, owner.Email)
	fixture.WaitForSubscriptionStatus(t, owner, licensev1.SubscriptionStatus_SUBSCRIPTION_STATUS_SUSPENDED, modeWait)
	waitForEntitlement(t, business, func(bool) bool { return true })

	t.Run("suspended", func(t *testing.T) {
		for _, op := range ops {
			if !op.garageScoped() || exemptFromLicense[op.String()] {
				continue
			}

			require.Eventually(t, func() bool {
				probe, err := owner.API.Probe(ctx, op.Method, op.url(business.GarageID), op.body())

				return err == nil && probe.Status == http.StatusPaymentRequired && strings.Contains(string(probe.Body), "SUBSCRIPTION_SUSPENDED")
			}, 20*time.Second, time.Second, "%s is refused as suspended", op)
		}
	})

	t.Run("the exempt operations still answer", func(t *testing.T) {
		for _, op := range ops {
			if !exemptFromLicense[op.String()] || op.String() == "GET /api/v1/garages/{garageId}/customers/lookup" {
				continue
			}

			probe, err := owner.API.Probe(ctx, op.Method, op.url(business.GarageID), op.body())
			require.NoError(t, err)
			assert.False(t, isLicenceError(probe.Body), "%s: %s", op, probe.Body)
		}
	})
}

// waitForEntitlement waits until the garage's entitlements (cached for a
// second) reflect the new mode.
func waitForEntitlement(t *testing.T, business *fixture.Business, ok func(readOnly bool) bool) {
	t.Helper()

	require.Eventually(t, func() bool {
		entitlements, err := business.Owner.API.GetGarageEntitlements(t.Context(), business.GarageID)
		if err != nil {
			return false
		}

		return ok(entitlements.GetReadOnly())
	}, 30*time.Second, time.Second)
}
