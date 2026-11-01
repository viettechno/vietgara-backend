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

// TestJunkInputNeverCausesAServerError: whatever a caller sends, the API
// answers with a client error, not a 5xx (a 5xx pages the on-call channel,
// NFR-OB-02, and hides the cause from the client). Every operation is tried
// with junk ids and bodies of the wrong shape, as the owner and as a
// platform admin. A value the database cannot store (text that is not a
// UUID, a NUL character) is a bad request, not a server error.
//
// Scenario: E2E-SYS-009: junk ids and bodies are refused with a client
// error on every operation.
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

	assert.Empty(t, compact(got), "the operations that answer 5xx to junk input")
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
