package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

// HeaderEventType carries the event type on every record.
const HeaderEventType = "event_type"

// Producer publishes outbox events to Kafka; it is the outbox.Publisher of
// the relay when brokers are configured.
type Producer struct {
	client *kgo.Client
	prefix string
}

// NewProducer connects to brokers. Records are acknowledged by all
// in-sync replicas before the relay marks an event published.
func NewProducer(brokers []string, prefix string, opts ...kgo.Opt) (*Producer, error) {
	client, err := kgo.NewClient(append([]kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.AllowAutoTopicCreation(),
	}, opts...)...)
	if err != nil {
		return nil, fmt.Errorf("kafka: producer: %w", err)
	}

	return &Producer{client: client, prefix: prefix}, nil
}

// Publish writes event to its topic and waits for the acknowledgement.
func (p *Producer) Publish(ctx context.Context, event outbox.Event) error {
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("kafka: encode %s: %w", event.ID, err)
	}

	record := &kgo.Record{
		Topic:   TopicFor(p.prefix, event.Type),
		Key:     []byte(event.Key()),
		Value:   value,
		Headers: []kgo.RecordHeader{{Key: HeaderEventType, Value: []byte(event.Type)}},
	}

	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("kafka: produce %s: %w", event.ID, err)
	}

	return nil
}

// Close flushes and disconnects.
func (p *Producer) Close() {
	p.client.Close()
}
