package outbox

import (
	"context"
	"fmt"
	"sync"
)

// Handler consumes one event. It must be idempotent: delivery is
// at-least-once (Event Design Section 2); wrap it with Processor.Wrap to
// get exactly-once effects.
type Handler func(ctx context.Context, event Event) error

// Subscriber registers handlers for event types. Implemented by the
// in-process Dispatcher and the Kafka consumer, so modules subscribe the
// same way whichever transport runs.
type Subscriber interface {
	Subscribe(eventType string, h Handler)
}

// Dispatcher is the in-process event bus used when no Kafka brokers are
// configured (local development without Kafka, unit tests): the relay
// hands events straight to the modules that subscribed. Events nobody
// subscribed to are simply acknowledged.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// NewDispatcher returns a Dispatcher without subscribers.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{handlers: map[string][]Handler{}}
}

// Subscribe registers h for every event of eventType.
func (d *Dispatcher) Subscribe(eventType string, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.handlers[eventType] = append(d.handlers[eventType], h)
}

// Handlers returns the handlers of eventType.
func (d *Dispatcher) Handlers(eventType string) []Handler {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return append([]Handler(nil), d.handlers[eventType]...)
}

// Publish runs the subscribers of event.Type in registration order,
// stopping at the first error.
func (d *Dispatcher) Publish(ctx context.Context, event Event) error {
	for _, h := range d.Handlers(event.Type) {
		if err := h(ctx, event); err != nil {
			return fmt.Errorf("handle %s: %w", event.Type, err)
		}
	}

	return nil
}
