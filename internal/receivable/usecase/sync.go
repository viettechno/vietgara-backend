package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// settlementEvent is the part of the Billing & Settlement events the module
// needs: which settlement changed. The rest it reads from the settlement.
type settlementEvent struct {
	SettlementID string `json:"settlement_id"`
}

// HandleSettlementChanged synchronizes the receivable of a settlement after
// settlement.created, payment_voucher.issued or payment_voucher.voided. It
// reads the settlement and adds what the receivable lacks, so it gives the
// same result whatever order the events arrive in and however often they are
// redelivered.
func (uc *ReceivableUseCase) HandleSettlementChanged(ctx context.Context, event outbox.Event) error {
	var p settlementEvent
	if err := json.Unmarshal(event.Data, &p); err != nil || p.SettlementID == "" || event.GarageID == "" {
		return nil //nolint:nilerr // a malformed event cannot succeed on retry: drop it
	}

	err := uc.Sync(ctx, event.GarageID, p.SettlementID)

	// A settlement that no longer exists cannot be read on any retry: drop the event.
	if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
		return nil
	}

	return err
}

// Sync brings the receivable of a settlement up to date with the settlement.
func (uc *ReceivableUseCase) Sync(ctx context.Context, garageID, settlementID string) error {
	// Events of one settlement arrive on separate topics and may be handled by
	// different instances at once: each sync reads the settlement after the
	// one before it has written, so a stale read never overwrites a newer one.
	if err := uc.repo.LockSettlement(ctx, settlementID); err != nil {
		return err
	}

	view, err := uc.settlements.Get(ctx, garageID, settlementID)
	if err != nil {
		return err
	}

	existing, err := uc.repo.FindBySettlement(ctx, garageID, settlementID)
	if err != nil && !errors.Is(err, domain.ErrReceivableNotFound) {
		return err
	}

	r := existing
	if r == nil {
		if r, err = uc.newReceivable(ctx, garageID, view); err != nil {
			return err
		}
	}

	r.SettlementNumber = view.Number

	// The settlement keeps the name and plate of when it was issued; the
	// receivable, once its customer was anonymized, does not take them back.
	if !r.Anonymized() {
		r.CustomerName, r.VehicleLicensePlate = view.CustomerName, view.VehicleLicensePlate
	}

	r.TotalAmount, r.PaidAmount = view.TotalAmount, view.PaidAmount

	if view.CustomerID != "" {
		r.CustomerID = view.CustomerID
	}

	r.Derive()

	if err := uc.repo.Save(ctx, r); err != nil {
		return err
	}

	return uc.repo.AddEntries(ctx, garageID, r.ID, entriesOf(view))
}

// newReceivable starts the receivable of a settlement: its due date is the
// settlement's day in the garage's timezone plus the payment terms.
func (uc *ReceivableUseCase) newReceivable(ctx context.Context, garageID string, view SettlementView) (*domain.Receivable, error) {
	settings, err := uc.repo.GetSettings(ctx, garageID)
	if err != nil {
		return nil, err
	}

	loc, err := uc.location(ctx, garageID)
	if err != nil {
		return nil, err
	}

	return &domain.Receivable{
		GarageID: garageID, SettlementID: view.ID, CustomerID: view.CustomerID,
		DueDate: domain.DueDateFor(view.CreatedAt, loc, settings.PaymentTermsDays),
	}, nil
}

// entriesOf lists the ledger entries a settlement stands for: its charge,
// each payment, and each payment voided.
func entriesOf(view SettlementView) []domain.Entry {
	entries := []domain.Entry{{
		Kind: domain.EntryCharge, Amount: view.TotalAmount, Reference: view.Number, OccurredAt: view.CreatedAt,
		SourceKey: "charge:" + view.ID,
	}}
	if view.TotalAmount <= 0 {
		entries = nil // nothing is owed: a zero charge is not a ledger line
	}

	for _, p := range view.Payments {
		entries = append(entries, domain.Entry{
			Kind: domain.EntryPayment, Amount: p.Amount, Reference: p.VoucherNumber, Method: p.Method,
			OccurredAt: p.CreatedAt, SourceKey: "payment:" + p.ID,
		})

		if p.VoidedAt != nil {
			entries = append(entries, domain.Entry{
				Kind: domain.EntryVoid, Amount: p.Amount, Reference: p.VoucherNumber, Method: p.Method,
				OccurredAt: *p.VoidedAt, SourceKey: "void:" + p.ID,
			})
		}
	}

	return entries
}

// HandleCustomerAnonymized replaces the name and the plate that the customer's
// receivables copied from their settlements with placeholders (PD-06). It is
// idempotent. The reminder log holds no personal data.
func (uc *ReceivableUseCase) HandleCustomerAnonymized(ctx context.Context, event outbox.Event) error {
	var p struct {
		CustomerID string `json:"customer_id"`
	}

	if err := json.Unmarshal(event.Data, &p); err != nil || p.CustomerID == "" || event.GarageID == "" {
		slog.Error("receivable: dropped a malformed customer.anonymized event", "event_id", event.ID, "error", err)

		return nil
	}

	return uc.repo.AnonymizeCustomer(ctx, event.GarageID, p.CustomerID)
}
