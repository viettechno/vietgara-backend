package usecase

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
)

// DefaultReminderInterval is how often the reminder job runs.
const DefaultReminderInterval = time.Hour

// Schedule runs the reminder job now and then every interval until ctx is
// cancelled (FR-REC-03).
func (uc *ReceivableUseCase) Schedule(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := uc.RunReminders(ctx); err != nil && ctx.Err() == nil {
			slog.Error("receivable: reminder job failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// RunReminders sends the payment reminders that have come due. Each
// receivable is handled in a transaction of its own that first takes an
// advisory lock on it, so a second instance running the job at the same time
// skips what the first is sending. A failure on one receivable does not stop
// the others; all failures are returned together.
func (uc *ReceivableUseCase) RunReminders(ctx context.Context) error {
	garages, err := uc.repo.ReminderGarages(ctx)
	if err != nil {
		return err
	}

	var errs []error

	for _, garageID := range garages {
		if err := uc.remindGarage(ctx, garageID); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (uc *ReceivableUseCase) remindGarage(ctx context.Context, garageID string) error {
	var open []*domain.Receivable

	settings := domain.DefaultSettings()

	var today time.Time

	err := uc.tx.InGarage(ctx, garageID, func(ctx context.Context) error {
		var err error
		if settings, err = uc.repo.GetSettings(ctx, garageID); err != nil {
			return err
		}

		if !settings.RemindersEnabled {
			return nil
		}

		if today, err = uc.today(ctx, garageID); err != nil {
			return err
		}

		open, err = uc.repo.OpenWithCustomer(ctx, garageID)

		return err
	})
	if err != nil {
		return err
	}

	var errs []error

	for _, r := range open {
		if len(settings.DueOffsets(r.DaysOverdue(today), doneOffsets(r.Reminders))) == 0 {
			continue
		}

		if err := uc.tx.InGarage(ctx, garageID, func(ctx context.Context) error {
			return uc.remind(ctx, garageID, r.ID, settings, today)
		}); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// doneOffsets are the offsets that need no further work: sent, skipped, or
// failed too often.
func doneOffsets(reminders []domain.Reminder) map[int]bool {
	done := map[int]bool{}

	for _, rem := range reminders {
		if rem.Outcome != domain.OutcomeFailed || rem.Attempts >= domain.MaxSendAttempts {
			done[rem.OffsetDays] = true
		}
	}

	return done
}

// remind sends the latest reminder due for one receivable and logs the
// earlier ones due with it as superseded. It runs in the receivable's own
// transaction; the receivable is read again once the lock is held.
func (uc *ReceivableUseCase) remind(ctx context.Context, garageID, receivableID string, settings domain.Settings, today time.Time) error {
	locked, err := uc.repo.TryLock(ctx, "receivable-reminder:"+receivableID)
	if err != nil || !locked {
		return err
	}

	r, err := uc.repo.FindByID(ctx, garageID, receivableID)
	if err != nil {
		return err
	}

	days := r.DaysOverdue(today)

	due := settings.DueOffsets(days, doneOffsets(r.Reminders))
	if r.Status != domain.StatusOpen || len(due) == 0 {
		return nil
	}

	latest := due[len(due)-1]

	for _, offset := range due[:len(due)-1] {
		if err := uc.repo.SaveReminder(ctx, garageID, r.ID, &domain.Reminder{
			OffsetDays: offset, Channel: domain.ChannelEmail, Outcome: domain.OutcomeSkipped, Reason: domain.ReasonSuperseded, Attempts: 1,
		}); err != nil {
			return err
		}
	}

	return uc.send(ctx, r, latest, days)
}

// send sends one reminder, logs what became of it and counts it when it went
// out. A failed send is logged and left for the next run; it must not roll the
// log of the attempt back, so it is not returned.
func (uc *ReceivableUseCase) send(ctx context.Context, r *domain.Receivable, offset, daysOverdue int) error {
	reminder := domain.Reminder{OffsetDays: offset, Channel: domain.ChannelEmail, Attempts: attemptsAfter(r.Reminders, offset)}

	delivery, sendErr := uc.sender.SendPaymentReminder(ctx, Reminder{
		GarageID: r.GarageID, CustomerID: r.CustomerID, ReceivableID: r.ID, SettlementNumber: r.SettlementNumber,
		Remaining: r.Remaining(), DueDate: r.DueDate, DaysOverdue: daysOverdue,
	})

	switch {
	case sendErr != nil:
		reminder.Outcome = domain.OutcomeFailed

		slog.Warn("receivable: payment reminder failed", "receivable_id", r.ID, "offset_days", offset, "error", sendErr)
	case delivery.Sent:
		reminder.Outcome = domain.OutcomeSent
	default:
		reminder.Outcome, reminder.Reason = domain.OutcomeSkipped, delivery.Reason
	}

	if err := uc.repo.SaveReminder(ctx, r.GarageID, r.ID, &reminder); err != nil {
		return err
	}

	if reminder.Outcome != domain.OutcomeSent {
		return nil
	}

	return uc.repo.MarkReminded(ctx, r.GarageID, r.ID, uc.now())
}

// attemptsAfter is the attempt number the next try of an offset will be.
func attemptsAfter(reminders []domain.Reminder, offset int) int {
	for _, rem := range reminders {
		if rem.OffsetDays == offset {
			return rem.Attempts + 1
		}
	}

	return 1
}
