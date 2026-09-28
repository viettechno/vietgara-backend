package kafka_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/viettechno/vietgara-backend/internal/platform/kafka"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

func TestTopicFor(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"account.email_verified":     "vietgara.identity.events",
		"garage.created":             "vietgara.tenant.events",
		"invitation.created":         "vietgara.tenant.events",
		"subscription.expiring_soon": "vietgara.license.events",
		"customer.deleted":           "vietgara.customer.events",
		"settlement.created":         "vietgara.settlement.events",
	}

	for eventType, want := range tests {
		assert.Equal(t, want, kafka.TopicFor("", eventType), eventType)
	}

	assert.Equal(t, "staging.tenant.events", kafka.TopicFor("staging", "garage.created"))
	assert.Equal(t, "vietgara.tenant.events.dlq", kafka.DLQTopic("vietgara.tenant.events"))
}

func newCluster(t *testing.T) []string {
	t.Helper()

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1,
		"vietgara.tenant.events", "vietgara.identity.events", "vietgara.tenant.events.dlq", "vietgara.identity.events.dlq"))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	return cluster.ListenAddrs()
}

func event(id, eventType string) outbox.Event {
	return outbox.Event{
		ID: id, Type: eventType, OccurredAt: time.Now().UTC().Truncate(time.Second),
		GarageID: "g-1", Version: outbox.EnvelopeVersion, Data: json.RawMessage(`{"k":"v"}`),
	}
}

func readTopic(t *testing.T, brokers []string, topic string, n int) []*kgo.Record {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ConsumeTopics(topic), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	require.NoError(t, err)
	t.Cleanup(client.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var records []*kgo.Record
	for len(records) < n {
		fetches := client.PollFetches(ctx)
		require.NoError(t, ctx.Err(), "timed out reading %s", topic)

		records = append(records, fetches.Records()...)
	}

	return records
}

func TestProducer(t *testing.T) {
	t.Parallel()

	brokers := newCluster(t)

	producer, err := kafka.NewProducer(brokers, "")
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	require.NoError(t, producer.Publish(t.Context(), event("e-1", "garage.created")))

	records := readTopic(t, brokers, "vietgara.tenant.events", 1)
	assert.Equal(t, "g-1", string(records[0].Key))
	assert.Equal(t, "garage.created", string(records[0].Headers[0].Value))

	var got outbox.Event
	require.NoError(t, json.Unmarshal(records[0].Value, &got))
	assert.Equal(t, "e-1", got.ID)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, producer.Publish(ctx, event("e-2", "garage.created")))

	_, err = kafka.NewProducer(brokers, "", kgo.TransactionalID(""))
	require.Error(t, err)
}

func TestConsumer(t *testing.T) {
	t.Parallel()

	brokers := newCluster(t)

	producer, err := kafka.NewProducer(brokers, "")
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	consumer := kafka.NewConsumer(brokers, "test-group", "", kafka.WithRetry(3, time.Millisecond))

	var (
		mu       sync.Mutex
		handled  []string
		failures atomic.Int32
	)

	done := make(chan struct{}, 10)

	consumer.Subscribe("garage.created", func(_ context.Context, e outbox.Event) error {
		mu.Lock()

		handled = append(handled, e.ID)
		mu.Unlock()

		done <- struct{}{}

		return nil
	})
	consumer.Subscribe("account.email_verified", func(_ context.Context, e outbox.Event) error {
		if e.ID == "flaky" && failures.Add(1) < 2 {
			return errors.New("transient")
		}

		if e.ID == "broken" {
			return errors.New("permanent")
		}

		done <- struct{}{}

		return nil
	})
	assert.Equal(t, []string{"vietgara.identity.events", "vietgara.tenant.events"}, consumer.Topics())

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)

	go func() { stopped <- consumer.Run(ctx) }()

	require.NoError(t, producer.Publish(t.Context(), event("g-created", "garage.created")))
	require.NoError(t, producer.Publish(t.Context(), event("flaky", "account.email_verified")))
	require.NoError(t, producer.Publish(t.Context(), event("broken", "account.email_verified")))

	raw, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	require.NoError(t, err)
	t.Cleanup(raw.Close)
	require.NoError(t, raw.ProduceSync(t.Context(), &kgo.Record{Topic: "vietgara.tenant.events", Value: []byte("not json")}).FirstErr())

	for range 2 {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("handlers did not run")
		}
	}

	dlq := readTopic(t, brokers, "vietgara.identity.events.dlq", 1)
	assert.Contains(t, string(dlq[0].Value), `"broken"`)
	tenantDLQ := readTopic(t, brokers, "vietgara.tenant.events.dlq", 1)
	assert.Equal(t, "not json", string(tenantDLQ[0].Value))

	cancel()
	require.NoError(t, <-stopped)

	mu.Lock()
	assert.Equal(t, []string{"g-created"}, handled)
	mu.Unlock()
	assert.EqualValues(t, 2, failures.Load())
}

func TestConsumer_NoSubscriptionsOrBadOptions(t *testing.T) {
	t.Parallel()

	require.NoError(t, kafka.NewConsumer([]string{"127.0.0.1:1"}, "g", "").Run(t.Context()))

	bad := kafka.NewConsumer([]string{"127.0.0.1:1"}, "g", "", kafka.WithClientOpts(kgo.TransactionalID("")))
	bad.Subscribe("garage.created", func(context.Context, outbox.Event) error { return nil })
	require.Error(t, bad.Run(t.Context()))
}

func TestConsumer_CancelledWhileRetrying(t *testing.T) {
	t.Parallel()

	brokers := newCluster(t)

	producer, err := kafka.NewProducer(brokers, "")
	require.NoError(t, err)
	t.Cleanup(producer.Close)

	consumer := kafka.NewConsumer(brokers, "cancel-group", "", kafka.WithRetry(5, time.Hour))
	attempted := make(chan struct{}, 1)

	consumer.Subscribe("garage.created", func(context.Context, outbox.Event) error {
		select {
		case attempted <- struct{}{}:
		default:
		}

		return errors.New("down")
	})

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)

	go func() { stopped <- consumer.Run(ctx) }()

	require.NoError(t, producer.Publish(t.Context(), event("e-1", "garage.created")))

	select {
	case <-attempted:
	case <-time.After(10 * time.Second):
		t.Fatal("handler did not run")
	}

	cancel()
	require.NoError(t, <-stopped)
}
