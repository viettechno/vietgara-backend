package system_test

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/testing/blackbox/client"
	"github.com/viettechno/vietgara-backend/testing/blackbox/fixture"
)

// junkBodies are bodies no endpoint should answer with a server error:
// JSON of the wrong shape, deeply nested, and a NUL character in a string.
func junkBodies() [][]byte {
	return [][]byte{
		[]byte(`null`),
		[]byte(`[]`),
		[]byte(`"text"`),
		[]byte(`{"x":` + strings.Repeat("[", 50) + `1` + strings.Repeat("]", 50) + `}`),
		[]byte(`{"name":"\u0000","fullName":"\u0000","description":"\u0000"}`),
		[]byte(`{"name":"` + strings.Repeat("x", 100_000) + `"}`),
	}
}

// session hands out an authenticated client and signs in again after a few
// hundred requests: the gateway allows 600 a minute per token, and a probe
// answered 429 would prove nothing.
type session struct {
	signIn func() *client.Client
	client *client.Client
	used   int
}

func (s *session) next() *client.Client {
	if s.client == nil || s.used >= 400 {
		s.client, s.used = s.signIn(), 0
	}

	s.used++

	return s.client
}

func newSession(t *testing.T, email, password string) *session {
	t.Helper()

	return &session{signIn: func() *client.Client {
		login, err := fixture.Anonymous(t).Login(t.Context(), email, password)
		require.NoError(t, err)

		return fixture.Anonymous(t).WithToken(login.GetAccessToken())
	}}
}

// serverErrors probes every operation with the id parameters set to junk
// and returns the operations that answered 5xx; with bodies it also sends
// bodies of the wrong shape to the writes.
func serverErrors(t *testing.T, caller *session, garageID, junk string, withBodies bool) []string {
	t.Helper()

	failed := map[string]bool{}

	for _, op := range operations(t) {
		path := strings.ReplaceAll(op.Path, "{garageId}", garageID)

		for strings.Contains(path, "{") {
			start, end := strings.Index(path, "{"), strings.Index(path, "}")

			value, ok := op.Enums[path[start+1:end]]
			if !ok {
				value = junk
			}

			path = path[:start] + value + path[end+1:]
		}

		bodies := [][]byte{op.body()}
		if withBodies && op.writes() && op.Method != http.MethodDelete {
			bodies = append(bodies, junkBodies()...)
		}

		for _, body := range bodies {
			probe, err := caller.next().Probe(t.Context(), op.Method, path, body)
			require.NoError(t, err, op.String())

			if !strings.Contains(op.Path, "mfa") && !strings.Contains(op.Path, "recovery-codes") { // a second-step lockout answers 429 by design
				require.NotEqual(t, http.StatusTooManyRequests, probe.Status, "%s was throttled, the probe proves nothing", op)
			}

			if probe.Status >= http.StatusInternalServerError {
				failed[op.String()] = true
			}
		}
	}

	out := make([]string, 0, len(failed))
	for op := range failed {
		out = append(out, op)
	}

	sort.Strings(out)

	return out
}

// junkIDs are what a caller might put where an id belongs: an id that is
// well formed but unknown, text that is not a UUID, something very long, an
// escaped path segment.
var junkIDs = []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid", "0", strings.Repeat("a", 300), "%2e%2e"}

// knownServerErrors are the operations that answer 5xx to junk input, all
// for one cause: a value the database rejects (text that is not a UUID, as
// an id in the path or an empty id in the body, or a NUL character in a
// name) reaches it, and nothing turns that into "not found" or a validation
// error. Backlog G-26; the fix is a single translation of those database
// errors. The test fails when the list
// changes, so a new offender is noticed and a fixed one is crossed out.
var knownServerErrors = []string{
	"DELETE /api/v1/admin/platform-groups/{groupId}",
	"DELETE /api/v1/admin/platform-groups/{groupId}/members/{userId}",
	"DELETE /api/v1/admin/platform-roles/{roleId}",
	"DELETE /api/v1/garages/{garageId}/quotes/{quoteId}",
	"DELETE /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/lines/{lineId}",
	"DELETE /api/v1/garages/{garageId}/staff-groups/{groupId}",
	"DELETE /api/v1/garages/{garageId}/staff/{staffId}",
	"DELETE /api/v1/garages/{garageId}/stock-counts/{stockCountId}/lines/{lineId}",
	"DELETE /api/v1/roles/{roleId}",
	"GET /api/v1/admin/platform-groups/{groupId}",
	"GET /api/v1/admin/platform-roles/{roleId}",
	"GET /api/v1/admin/support-grants/{grantId}/notifications",
	"GET /api/v1/admin/support-grants/{grantId}/outbox-events",
	"GET /api/v1/garages/{garageId}/goods-receipts/{goodsReceiptId}",
	"GET /api/v1/garages/{garageId}/goods-receipts/{goodsReceiptId}/pdf",
	"GET /api/v1/garages/{garageId}/quotes/{quoteId}",
	"GET /api/v1/garages/{garageId}/quotes/{quoteId}/pdf",
	"GET /api/v1/garages/{garageId}/repair-orders/{repairOrderId}",
	"GET /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/part-returns",
	"GET /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/pdf",
	"GET /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/stock-issue",
	"GET /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/stock-issue/pdf",
	"GET /api/v1/garages/{garageId}/settlements/{settlementId}",
	"GET /api/v1/garages/{garageId}/settlements/{settlementId}/payments/{paymentId}/voucher-pdf",
	"GET /api/v1/garages/{garageId}/settlements/{settlementId}/receipt-pdf",
	"GET /api/v1/garages/{garageId}/stock-counts/{stockCountId}",
	"GET /api/v1/garages/{garageId}/stock-counts/{stockCountId}/excel",
	"GET /api/v1/garages/{garageId}/supplier-returns/{supplierReturnId}",
	"GET /api/v1/garages/{garageId}/write-offs/{writeOffId}",
	"GET /api/v1/roles/{roleId}",
	"PATCH /api/v1/garages/{garageId}/stock-counts/{stockCountId}/lines/{lineId}",
	"POST /api/v1/admin/platform-invitations",
	"POST /api/v1/admin/platform-invitations/{invitationId}/revoke",
	"POST /api/v1/admin/platform-roles",
	"POST /api/v1/admin/subscriptions/{subscriptionId}/extensions",
	"POST /api/v1/admin/support-grants/{grantId}:end",
	"POST /api/v1/garages",
	"POST /api/v1/garages/{garageId}/customers",
	"POST /api/v1/garages/{garageId}/goods-receipts/{goodsReceiptId}/pdf:print",
	"POST /api/v1/garages/{garageId}/goods-receipts/{goodsReceiptId}:cancel",
	"POST /api/v1/garages/{garageId}/goods-receipts/{goodsReceiptId}:complete",
	"POST /api/v1/garages/{garageId}/invitations",
	"POST /api/v1/garages/{garageId}/jobs",
	"POST /api/v1/garages/{garageId}/part-categories",
	"POST /api/v1/garages/{garageId}/quotes/{quoteId}/conversion",
	"POST /api/v1/garages/{garageId}/quotes/{quoteId}/decision",
	"POST /api/v1/garages/{garageId}/quotes/{quoteId}/lines:fromJob",
	"POST /api/v1/garages/{garageId}/quotes/{quoteId}/pdf:print",
	"POST /api/v1/garages/{garageId}/quotes/{quoteId}/sending",
	"POST /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/cancellation",
	"POST /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/closing",
	"POST /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/lines",
	"POST /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/pdf:print",
	"POST /api/v1/garages/{garageId}/repair-orders/{repairOrderId}/stock-issue/pdf:print",
	"POST /api/v1/garages/{garageId}/settlements/{settlementId}/payments/{paymentId}/voucher-pdf:print",
	"POST /api/v1/garages/{garageId}/settlements/{settlementId}/receipt-pdf:print",
	"POST /api/v1/garages/{garageId}/stock-counts",
	"POST /api/v1/garages/{garageId}/stock-counts/{stockCountId}/lines",
	"POST /api/v1/garages/{garageId}/stock-counts/{stockCountId}:cancel",
	"POST /api/v1/garages/{garageId}/stock-counts/{stockCountId}:complete",
	"POST /api/v1/garages/{garageId}/stock-issues/{stockIssueId}:post",
	"POST /api/v1/garages/{garageId}/supplier-returns/{supplierReturnId}:cancel",
	"POST /api/v1/garages/{garageId}/suppliers",
	"POST /api/v1/garages/{garageId}/support-grants/{grantId}:approve",
	"POST /api/v1/garages/{garageId}/support-grants/{grantId}:revoke",
	"POST /api/v1/garages/{garageId}/write-offs/{writeOffId}:cancel",
	"POST /api/v1/me/platform-invitations/{invitationId}/accept",
	"POST /api/v1/me/platform-invitations/{invitationId}/decline",
	"POST /api/v1/roles",
}

// TestJunkInputNeverCausesAServerError: whatever a caller sends, the API
// answers with a client error, not a 5xx (a 5xx pages the on-call channel,
// NFR-OB-02, and hides the cause from the client). Every operation is tried
// with junk ids and bodies of the wrong shape, as the owner and as a
// platform admin; the operations that still answer 5xx are the known ones.
//
// Scenario: E2E-SYS-009: junk ids and bodies are refused with a client
// error on every operation; the known exceptions are listed (Backlog G-26).
func TestJunkInputNeverCausesAServerError(t *testing.T) {
	t.Parallel()

	business := fixture.NewBusiness(t, "junk")
	admin := fixture.PlatformAdmin(t)

	var got []string

	callers := []*session{newSession(t, business.Owner.Email, fixture.Password), newSession(t, admin.Email, admin.Password)}

	for _, caller := range callers {
		for index, junk := range junkIDs {
			// Bodies of the wrong shape go with the first id only.
			got = append(got, serverErrors(t, caller, business.GarageID, junk, index == 0)...)
		}
	}

	sort.Strings(got)

	assert.Equal(t, knownServerErrors, compact(got), "the operations that answer 5xx to junk input")
}

func compact(in []string) []string {
	out := in[:0]

	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}

	return out
}
