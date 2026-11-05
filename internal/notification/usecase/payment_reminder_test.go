package usecase_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
)

func TestPaymentReminder(t *testing.T) {
	t.Parallel()

	req := usecase.PaymentReminderRequest{
		GarageID: garageID, CustomerID: customerID, ReceivableID: "rec-1", Number: "QT-0001", Remaining: 600_000,
		DueDate: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC), DaysOverdue: 3,
	}

	t.Run("e-mails the customer the amount owed and the days overdue, with the opt-out link", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")
		msg := d.sentMessage()
		rows := d.logged()

		res, err := uc.PaymentReminder(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, res.Sent)
		assert.Empty(t, res.Reason)
		assert.Contains(t, msg.Subject, "QT-0001")
		assert.Contains(t, msg.Text, "600.000")
		assert.Contains(t, msg.Text, "3 ngày")
		assert.Contains(t, msg.Text, "17/11/2026")
		assert.Contains(t, msg.Text, "/api/v1/public/email-opt-out/", "a reminder is not essential: it carries the opt-out")
		require.Len(t, *rows, 1)
		assert.Equal(t, domain.TypePaymentReminder, (*rows)[0].Type)
		assert.Equal(t, "receivable", (*rows)[0].EntityType)
	})

	skips := []struct {
		name     string
		settings domain.Settings
		customer func() usecase.CustomerInfo
		suppress bool
		reason   string
	}{
		{"the garage switched the type off", domain.Settings{Off: map[domain.Type]bool{domain.TypePaymentReminder: true}}, customerAn, false, "DISABLED"},
		{"the customer has no address", domain.Settings{}, func() usecase.CustomerInfo { c := customerAn(); c.Email = ""; return c }, false, "NO_EMAIL"},
		{"the customer opted out", domain.Settings{}, func() usecase.CustomerInfo { c := customerAn(); c.OptedOut = true; return c }, false, "OPTED_OUT"},
		{"the address bounced", domain.Settings{}, customerAn, true, "SUPPRESSED"},
	}

	for _, tt := range skips {
		t.Run("skipped when "+tt.name, func(t *testing.T) {
			t.Parallel()

			uc, d := newGarageUseCase(t)
			d.garageReads(tt.settings)
			d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(tt.customer(), nil).Once()

			if tt.suppress {
				d.Suppressions.EXPECT().Suppressed(mock.Anything, "customer.an@example.com").Return(true, nil).Once()
			}

			rows := d.logged()

			res, err := uc.PaymentReminder(t.Context(), req)
			require.NoError(t, err)
			assert.False(t, res.Sent)
			assert.Equal(t, tt.reason, res.Reason)
			require.Len(t, *rows, 1)
			assert.Equal(t, domain.StatusSkipped, (*rows)[0].Status)
		})
	}

	t.Run("a send failure is returned so the job logs it and tries again", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(customerAn(), nil).Once()
		d.notSuppressed("customer.an@example.com")
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom2).Once()
		d.Log.EXPECT().Failed(mock.Anything, mock.Anything).Return(nil).Once()

		res, err := uc.PaymentReminder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)
		assert.False(t, res.Sent)
	})

	t.Run("reading the garage or the customer fails", func(t *testing.T) {
		t.Parallel()

		uc, d := newGarageUseCase(t)
		d.Settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{}, errBoom2).Once()

		_, err := uc.PaymentReminder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)

		uc, d = newGarageUseCase(t)
		d.garageReads(domain.Settings{})
		d.Customers.EXPECT().Get(mock.Anything, garageID, customerID).Return(usecase.CustomerInfo{}, errBoom2).Once()

		_, err = uc.PaymentReminder(t.Context(), req)
		require.ErrorIs(t, err, errBoom2)
	})
}
