// Package kafka is the event bus of VietGara (ADR-004): the outbox relay
// publishes domain events to Kafka topics and module consumers read them
// in one consumer group.
//
// Topics: one per bounded context, "<prefix>.<context>.events" (e.g.
// vietgara.tenant.events), keyed by garage_id (or user_id) so the
// events of one tenant stay ordered. Handlers that keep failing are
// parked on "<topic>.dlq".
package kafka

import (
	"strings"
)

// DefaultTopicPrefix prefixes every topic.
const DefaultTopicPrefix = "vietgara"

// Bounded contexts (HLD Section 3) owning the event topics.
const (
	contextIdentity = "identity"
	contextTenant   = "tenant"
	contextLicense  = "license"
	contextCustomer = "customer"
	contextSupplier = "supplier"
)

// contexts maps an event's first name segment to its bounded context.
var contexts = map[string]string{
	"user":         contextIdentity,
	"garage":       contextTenant,
	"staff":        contextTenant,
	"invitation":   contextTenant,
	"role":         contextTenant,
	"subscription": contextLicense,
	"entitlement":  contextLicense,
	"plan":         contextLicense,
	"customer":     contextCustomer,
	"vehicle":      contextCustomer,
	"supplier":     contextSupplier,
}

// TopicFor returns the topic of an event type, e.g. "garage.created" ->
// "vietgara.tenant.events".
func TopicFor(prefix, eventType string) string {
	if prefix == "" {
		prefix = DefaultTopicPrefix
	}

	head, _, _ := strings.Cut(eventType, ".")

	context, ok := contexts[head]
	if !ok {
		context = head
	}

	return prefix + "." + context + ".events"
}

// DLQTopic is where records of topic go when their handler keeps failing.
func DLQTopic(topic string) string {
	return topic + ".dlq"
}
