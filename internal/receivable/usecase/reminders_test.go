package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

// today is the 20th; a receivable due on the 13th is 7 days overdue.
func overdueBy(days int) *domain.Receivable {
	return openReceivable(day(2026, 11, 20).AddDate(0, 0, -days))
}

func expectRun(d *deps, open ...*domain.Receivable) {
	d.inGarage()
	d.timezone()
	d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
	d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
	d.Repo.EXPECT().OpenWithCustomer(mock.Anything, garageID, day(2026, 11, 17)).Return(open, nil).Once()
}

func expectLoaded(d *deps, r *domain.Receivable) {
	d.Repo.EXPECT().TryLock(mock.Anything, "receivable-reminder:"+receivableID).Return(true, nil).Once()
	d.Repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(r, nil).Once()
}

func TestReceivableUseCase_RunReminders(t *testing.T) {
	t.Parallel()

	t.Run("sends the reminder that came due and counts it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(3)
		expectRun(d, r)
		expectLoaded(d, r)
		d.Sender.EXPECT().SendPaymentReminder(mock.Anything, usecase.Reminder{
			GarageID: garageID, CustomerID: customerID, ReceivableID: receivableID, SettlementNumber: "QT-0001",
			Remaining: 600_000, DueDate: r.DueDate, DaysOverdue: 3,
		}).Return(usecase.Delivery{Sent: true}, nil).Once()
		d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, &domain.Reminder{
			OffsetDays: 3, Channel: domain.ChannelEmail, Outcome: domain.OutcomeSent, Attempts: 1,
		}).Return(nil).Once()
		d.Repo.EXPECT().MarkReminded(mock.Anything, garageID, receivableID, now).Return(nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("sends nothing before the first offset or after every offset was handled", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		early, handled := overdueBy(2), overdueBy(8)
		handled.Reminders = []domain.Reminder{{OffsetDays: 3, Outcome: domain.OutcomeSent}, {OffsetDays: 7, Outcome: domain.OutcomeSkipped}}
		expectRun(d, early, handled)

		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("only the latest of several due offsets is sent, the earlier ones are superseded", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(10)
		expectRun(d, r)
		expectLoaded(d, r)
		d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, &domain.Reminder{
			OffsetDays: 3, Channel: domain.ChannelEmail, Outcome: domain.OutcomeSkipped, Reason: domain.ReasonSuperseded, Attempts: 1,
		}).Return(nil).Once()
		d.Sender.EXPECT().SendPaymentReminder(mock.Anything, mock.MatchedBy(func(r usecase.Reminder) bool { return r.DaysOverdue == 10 })).
			Return(usecase.Delivery{Sent: true}, nil).Once()
		d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, &domain.Reminder{
			OffsetDays: 7, Channel: domain.ChannelEmail, Outcome: domain.OutcomeSent, Attempts: 1,
		}).Return(nil).Once()
		d.Repo.EXPECT().MarkReminded(mock.Anything, garageID, receivableID, now).Return(nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()))
	})
}

func TestReceivableUseCase_RunReminders_Part2(t *testing.T) {
	t.Parallel()

	t.Run("a skipped reminder keeps its reason and is not counted", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(3)
		expectRun(d, r)
		expectLoaded(d, r)
		d.Sender.EXPECT().SendPaymentReminder(mock.Anything, mock.Anything).Return(usecase.Delivery{Reason: domain.ReasonOptedOut}, nil).Once()
		d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, &domain.Reminder{
			OffsetDays: 3, Channel: domain.ChannelEmail, Outcome: domain.OutcomeSkipped, Reason: domain.ReasonOptedOut, Attempts: 1,
		}).Return(nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("a failed send is logged and tried again by the next run, up to the limit", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(3)
		r.Reminders = []domain.Reminder{{OffsetDays: 3, Outcome: domain.OutcomeFailed, Attempts: 1}}
		expectRun(d, r)
		expectLoaded(d, r)
		d.Sender.EXPECT().SendPaymentReminder(mock.Anything, mock.Anything).Return(usecase.Delivery{}, errBoom).Once()
		d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, &domain.Reminder{
			OffsetDays: 3, Channel: domain.ChannelEmail, Outcome: domain.OutcomeFailed, Attempts: 2,
		}).Return(nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()), "the failure is logged, not returned: the log of the attempt must commit")

		uc, d = newUseCase(t)
		d.noMissing()

		exhausted := overdueBy(3)
		exhausted.Reminders = []domain.Reminder{{OffsetDays: 3, Outcome: domain.OutcomeFailed, Attempts: domain.MaxSendAttempts}}
		expectRun(d, exhausted)
		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("nothing is sent while reminders are off", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()
		d.inGarage()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{PaymentTermsDays: 7}, nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()))
	})
}

func TestReceivableUseCase_RunReminders_Part3(t *testing.T) {
	t.Parallel()

	t.Run("a receivable another instance holds, or that was paid meanwhile, is left alone", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(3)
		expectRun(d, r)
		d.Repo.EXPECT().TryLock(mock.Anything, "receivable-reminder:"+receivableID).Return(false, nil).Once()
		require.NoError(t, uc.RunReminders(t.Context()))

		uc, d = newUseCase(t)
		d.noMissing()
		expectRun(d, r)

		paid := overdueBy(3)
		paid.Status = domain.StatusPaid
		expectLoaded(d, paid)
		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("one failing receivable does not stop the others; all errors are returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()

		r := overdueBy(3)
		expectRun(d, r)
		d.Repo.EXPECT().TryLock(mock.Anything, "receivable-reminder:"+receivableID).Return(false, errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		uc, d = newUseCase(t)
		d.noMissing()
		expectRun(d, r)
		d.Repo.EXPECT().TryLock(mock.Anything, mock.Anything).Return(true, nil).Once()
		d.Repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(nil, errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		for _, fail := range []string{"save", "mark", "supersede"} {
			uc, d = newUseCase(t)
			d.noMissing()

			rr := overdueBy(10)
			expectRun(d, rr)
			expectLoaded(d, rr)

			switch fail {
			case "supersede":
				d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, mock.Anything).Return(errBoom).Once()
			case "save":
				d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, mock.MatchedBy(func(x *domain.Reminder) bool { return x.OffsetDays == 3 })).Return(nil).Once()
				d.Sender.EXPECT().SendPaymentReminder(mock.Anything, mock.Anything).Return(usecase.Delivery{Sent: true}, nil).Once()
				d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, mock.MatchedBy(func(x *domain.Reminder) bool { return x.OffsetDays == 7 })).Return(errBoom).Once()
			case "mark":
				d.Repo.EXPECT().SaveReminder(mock.Anything, garageID, receivableID, mock.Anything).Return(nil).Twice()
				d.Sender.EXPECT().SendPaymentReminder(mock.Anything, mock.Anything).Return(usecase.Delivery{Sent: true}, nil).Once()
				d.Repo.EXPECT().MarkReminded(mock.Anything, garageID, receivableID, mock.Anything).Return(errBoom).Once()
			}

			require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom, fail)
		}
	})

	t.Run("failures reading the garages or the first look are returned", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return(nil, errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		uc, d = newUseCase(t)
		d.noMissing()
		d.inGarage()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		uc, d = newUseCase(t)
		d.noMissing()
		d.inGarage()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("", errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		uc, d = newUseCase(t)
		d.noMissing()
		d.inGarage()
		d.timezone()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Once()
		d.Repo.EXPECT().OpenWithCustomer(mock.Anything, garageID, day(2026, 11, 17)).Return(nil, errBoom).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)
	})
}

func TestReceivableUseCase_Schedule(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)
	d.noMissing()

	ctx, cancel := context.WithCancel(t.Context())

	d.Repo.EXPECT().ReminderGarages(mock.Anything).RunAndReturn(func(context.Context) ([]string, error) {
		cancel()

		return nil, errBoom
	}).Once()

	done := make(chan struct{})

	go func() {
		uc.Schedule(ctx, time.Hour)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the schedule did not stop with its context")
	}
}

func TestReceivableUseCase_RunReminders_Heal(t *testing.T) {
	t.Parallel()

	missing := []usecase.MissingSettlement{{GarageID: garageID, SettlementID: settlementID}}

	t.Run("creates the receivable of an unpaid settlement that has none", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.inGarage()
		d.lock()
		d.Repo.EXPECT().MissingSettlements(mock.Anything, mock.Anything).Return(missing, nil).Once()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(settlementView(), nil).Once()
		d.Repo.EXPECT().FindBySettlement(mock.Anything, garageID, settlementID).Return(openReceivable(day(2026, 11, 17)), nil).Once()
		d.Repo.EXPECT().Save(mock.Anything, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().AddEntries(mock.Anything, garageID, receivableID, mock.Anything).Return(nil).Once()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return(nil, nil).Once()

		require.NoError(t, uc.RunReminders(t.Context()))
	})

	t.Run("a failed heal is reported and the reminders still run", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.inGarage()
		d.lock()
		d.Repo.EXPECT().MissingSettlements(mock.Anything, mock.Anything).Return(missing, nil).Once()
		d.Settlements.EXPECT().Get(mock.Anything, garageID, settlementID).Return(usecase.SettlementView{}, errBoom).Once()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return(nil, nil).Once()

		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)

		uc, d = newUseCase(t)
		d.Repo.EXPECT().MissingSettlements(mock.Anything, mock.Anything).Return(nil, errBoom).Once()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return(nil, nil).Once()
		require.ErrorIs(t, uc.RunReminders(t.Context()), errBoom)
	})

	t.Run("a garage with no reminder days has nothing to read", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.noMissing()
		d.inGarage()
		d.Repo.EXPECT().ReminderGarages(mock.Anything).Return([]string{garageID}, nil).Once()
		d.Repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{PaymentTermsDays: 7, RemindersEnabled: true}, nil).Once()
		d.timezone()

		require.NoError(t, uc.RunReminders(t.Context()))
	})
}
