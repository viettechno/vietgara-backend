package usecase

import (
	"context"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
)

// EventCustomerAnonymized is published when a customer's personal data was
// erased: the modules that copied a name or a plate follow it (PD-06).
const EventCustomerAnonymized = "customer.anonymized"

const (
	actionCustomerAnonymized = "customer.anonymized"
	entityCustomer           = "customer"
)

// anonymizedPayload is the data of customer.anonymized.
type anonymizedPayload struct {
	CustomerID string `json:"customer_id"`
	GarageID   string `json:"garage_id"`
}

// EventPublisher writes domain events to the outbox in the caller's
// transaction.
type EventPublisher interface {
	Publish(ctx context.Context, eventType, garageID, userID string, payload any) error
}

// WithEvents lets the use case publish customer.anonymized.
func WithEvents(p EventPublisher) Option {
	return func(o *options) {
		o.events = p
	}
}

// Anonymize erases the personal data of a customer (FR-CUS-05, PD-06) and
// tells the other modules. What the law requires the garage to keep, the
// financial documents with the data snapshotted when they were issued and the
// consent log, is not touched. It cannot be undone, and a second call is
// refused with domain.ErrCustomerAnonymized.
func (uc *CustomerUseCase) Anonymize(ctx context.Context, garageID, customerID, actorID string) (domain.AnonymizedCounts, error) {
	const op = "anonymize customer"

	c, err := uc.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		return domain.AnonymizedCounts{}, toAppError(op, err)
	}

	if c.IsAnonymized() {
		return domain.AnonymizedCounts{}, toAppError(op, domain.ErrCustomerAnonymized)
	}

	counts, err := uc.customers.Anonymize(ctx, garageID, customerID, actorID)
	if err != nil {
		return domain.AnonymizedCounts{}, toAppError(op, err)
	}

	if uc.opts.audit != nil {
		err = uc.opts.audit.Record(ctx, audit.Entry{
			Actor: actorID, GarageID: garageID, Action: actionCustomerAnonymized, EntityType: entityCustomer, EntityID: customerID,
			After: map[string]any{"vehicles": counts.Vehicles, "tax_profiles": counts.TaxProfiles}, IP: middleware.ClientIP(ctx),
		})
		if err != nil {
			return domain.AnonymizedCounts{}, toAppError(op, err)
		}
	}

	if uc.opts.events != nil {
		err = uc.opts.events.Publish(ctx, EventCustomerAnonymized, garageID, actorID, anonymizedPayload{CustomerID: customerID, GarageID: garageID})
		if err != nil {
			return domain.AnonymizedCounts{}, toAppError(op, err)
		}
	}

	return counts, nil
}
