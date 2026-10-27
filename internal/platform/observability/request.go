// Package observability makes the backend measurable and searchable
// (Release 9.1): Prometheus metrics per module, one structured log line per
// request carrying the trace id and the garage id (NFR-OB-02..04), and the
// health probes.
package observability

import (
	"context"
	"sync/atomic"
)

type requestKey struct{}

// request is filled while a request runs, so the outermost interceptor can
// log what an inner one learned: the garage whose access was checked.
type request struct{ garageID atomic.Value }

// withRequest returns ctx carrying an empty request record.
func withRequest(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestKey{}, &request{})
}

// SetGarage records the garage a request acts on. It does nothing outside a
// request seen by the Observe interceptor.
func SetGarage(ctx context.Context, garageID string) {
	if r, ok := ctx.Value(requestKey{}).(*request); ok {
		r.garageID.Store(garageID)
	}
}

// GarageID returns the garage recorded by SetGarage, or "".
func GarageID(ctx context.Context) string {
	r, ok := ctx.Value(requestKey{}).(*request)
	if !ok {
		return ""
	}

	id, _ := r.garageID.Load().(string)

	return id
}
