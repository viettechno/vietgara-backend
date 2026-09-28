package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/db/dbtest"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

var (
	errBoom  = errors.New("boom")
	occurred = time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
)

func TestEventPublisher(t *testing.T) {
	t.Parallel()

	tx := dbtest.New(t).
		ExpectExec("INSERT INTO outbox_events", dbtest.Tag(1), nil).
		WithArgs(func(args []any) {
			assert.Equal(t, "garage.created", args[1])
			assert.Equal(t, "g-1", args[2])
			assert.Empty(t, args[3])
			payload, _ := args[4].([]byte)
			assert.JSONEq(t, `{"k":"v"}`, string(payload))
		})

	p := outbox.NewEventPublisher(nil)
	require.NoError(t, p.Publish(tx.Context(t.Context()), "garage.created", "g-1", "", map[string]string{"k": "v"}))
	require.Error(t, p.Publish(t.Context(), "x", "", "", make(chan int)), "unmarshalable payload")
}

func TestEventKey(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "g", outbox.Event{GarageID: "g", AccountID: "a"}.Key())
	assert.Equal(t, "a", outbox.Event{AccountID: "a"}.Key())
}

func TestDispatcher(t *testing.T) {
	t.Parallel()

	d := outbox.NewDispatcher()

	var calls []string

	d.Subscribe("a", func(context.Context, outbox.Event) error { calls = append(calls, "a1"); return nil })
	d.Subscribe("a", func(context.Context, outbox.Event) error { calls = append(calls, "a2"); return errBoom })
	d.Subscribe("a", func(context.Context, outbox.Event) error { calls = append(calls, "a3"); return nil })

	require.NoError(t, d.Publish(t.Context(), outbox.Event{Type: "nobody"}))
	require.ErrorIs(t, d.Publish(t.Context(), outbox.Event{Type: "a"}), errBoom)
	assert.Equal(t, []string{"a1", "a2"}, calls)
	assert.Len(t, d.Handlers("a"), 3)
}

// scopeTx runs fn in the fake transaction ctx and records scopes.
type scopeTx struct {
	ctx    context.Context
	scopes []platformdb.Scope
	err    error
}

func (s *scopeTx) WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error {
	s.scopes = append(s.scopes, scope)
	if s.err != nil {
		return s.err
	}

	if !platformdb.InTransaction(ctx) {
		ctx = s.ctx
	}

	return fn(ctx)
}

func TestProcessor(t *testing.T) {
	t.Parallel()

	event := outbox.Event{ID: "e-1", Type: "x", GarageID: "g", AccountID: "a"}

	t.Run("runs once", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).ExpectExec("INSERT INTO processed_events", dbtest.Tag(1), nil)
		tx := &scopeTx{ctx: db.Context(t.Context())}
		ran := false

		h := outbox.NewProcessor(nil, tx).Wrap("tenant", func(context.Context, outbox.Event) error { ran = true; return nil })
		require.NoError(t, h(t.Context(), event))
		assert.True(t, ran)
		assert.Equal(t, []platformdb.Scope{{GarageID: "g", AccountID: "a"}}, tx.scopes)
	})

	t.Run("skips processed events", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).ExpectExec("INSERT INTO processed_events", dbtest.Tag(0), nil)
		h := outbox.NewProcessor(nil, &scopeTx{ctx: db.Context(t.Context())}).Wrap("tenant", func(context.Context, outbox.Event) error {
			t.Fatal("must not run")

			return nil
		})
		require.NoError(t, h(t.Context(), event))
	})

	t.Run("reports failures", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).ExpectExec("INSERT INTO processed_events", dbtest.Tag(0), errBoom)
		h := outbox.NewProcessor(nil, &scopeTx{ctx: db.Context(t.Context())}).Wrap("tenant", nil)
		require.ErrorIs(t, h(t.Context(), event), errBoom)
	})
}

type recordingPublisher struct {
	events []outbox.Event
	err    error
}

func (p *recordingPublisher) Publish(_ context.Context, e outbox.Event) error {
	p.events = append(p.events, e)

	return p.err
}

func TestRelay(t *testing.T) {
	t.Parallel()

	eventRow := dbtest.Row("e-1", "garage.created", "g-1", "a-1", []byte(`{"k":1}`), occurred)

	t.Run("publishes and marks", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).
			ExpectQuery("SELECT id FROM outbox_events", dbtest.NewRows([]any{"e-1"}, []any{"e-2"}), nil).
			ExpectQueryRow("FOR UPDATE SKIP LOCKED", eventRow).
			ExpectExec("SET status = 'published'", dbtest.Tag(1), nil).
			ExpectQueryRow("FOR UPDATE SKIP LOCKED", dbtest.ErrRow(pgx.ErrNoRows))

		ctx := db.Context(t.Context())
		tx := &scopeTx{ctx: ctx}
		pub := &recordingPublisher{}

		n, err := outbox.NewRelay(nil, tx, pub, outbox.WithMaxAttempts(3)).RelayPending(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		require.Len(t, pub.events, 1)
		assert.Equal(t, outbox.Event{
			ID: "e-1", Type: "garage.created", GarageID: "g-1", AccountID: "a-1",
			OccurredAt: occurred, Version: 1, Data: []byte(`{"k":1}`),
		}, pub.events[0])
		assert.Contains(t, tx.scopes, platformdb.Scope{GarageID: "g-1", AccountID: "a-1"})
	})

	t.Run("records failures", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).
			ExpectQuery("SELECT id FROM outbox_events", dbtest.NewRows([]any{"e-1"}, []any{"e-2"}, []any{"e-3"}), nil).
			ExpectQueryRow("FOR UPDATE SKIP LOCKED", eventRow).
			ExpectExec("SET attempts = attempts + 1", dbtest.Tag(1), nil).
			ExpectQueryRow("FOR UPDATE SKIP LOCKED", dbtest.ErrRow(errBoom)).
			ExpectExec("SET attempts = attempts + 1", dbtest.Tag(0), errBoom).
			ExpectQueryRow("FOR UPDATE SKIP LOCKED", eventRow).
			ExpectExec("SET status = 'published'", dbtest.Tag(0), errBoom).
			ExpectExec("SET attempts = attempts + 1", dbtest.Tag(1), nil)

		ctx := db.Context(t.Context())
		pub := &recordingPublisher{}
		calls := 0
		pubFail := publisherFunc(func(ctx context.Context, e outbox.Event) error {
			calls++
			if calls == 1 {
				return errBoom
			}

			return pub.Publish(ctx, e)
		})

		n, err := outbox.NewRelay(nil, &scopeTx{ctx: ctx}, pubFail).RelayPending(ctx)
		require.NoError(t, err)
		assert.Zero(t, n)
	})

	t.Run("listing errors", func(t *testing.T) {
		t.Parallel()

		for _, db := range []*dbtest.Tx{
			dbtest.New(t).ExpectQuery("SELECT id", nil, errBoom),
			dbtest.New(t).ExpectQuery("SELECT id", dbtest.NewRows([]any{"e"}).WithScanError(errBoom), nil),
			dbtest.New(t).ExpectQuery("SELECT id", dbtest.NewRows().WithErr(errBoom), nil),
		} {
			ctx := db.Context(t.Context())
			_, err := outbox.NewRelay(nil, &scopeTx{ctx: ctx}, &recordingPublisher{}).RelayPending(ctx)
			require.ErrorIs(t, err, errBoom)
		}
	})

	t.Run("run loop stops with the context", func(t *testing.T) {
		t.Parallel()

		db := dbtest.New(t).ExpectQuery("SELECT id", nil, errBoom)

		ctx, cancel := context.WithCancel(db.Context(t.Context()))
		relay := outbox.NewRelay(nil, &scopeTx{ctx: ctx}, &recordingPublisher{}, outbox.WithInterval(time.Hour))
		done := make(chan struct{})

		go func() {
			relay.Run(ctx)
			close(done)
		}()

		time.Sleep(20 * time.Millisecond)
		cancel()
		<-done
	})
}

type publisherFunc func(ctx context.Context, e outbox.Event) error

func (f publisherFunc) Publish(ctx context.Context, e outbox.Event) error { return f(ctx, e) }
