package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

var errBoom2 = errors.New("boom2")

const (
	garageID     = "22222222-2222-2222-2222-222222222222"
	customerID   = "33333333-3333-3333-3333-333333333333"
	vehicleID    = "44444444-4444-4444-4444-444444444444"
	settlementID = "55555555-5555-5555-5555-555555555555"
	quoteID      = "66666666-6666-6666-6666-666666666666"
	creatorID    = "77777777-7777-7777-7777-777777777777"
)

type mockContext = context.Context

// garageDeps are the mocked dependencies of the e-mails of a garage.
type garageDeps struct {
	*deps

	Settings     *mocks.MockNotificationSettings
	Suppressions *mocks.MockNotificationSuppressions
	Customers    *mocks.MockNotificationCustomerReader
	Garages      *mocks.MockNotificationGarageReader
	Staff        *mocks.MockNotificationStaffReader
	Receipts     *mocks.MockNotificationReceipts
	Tx           *mocks.MockNotificationTransactor
}

func newGarageUseCase(t *testing.T) (*usecase.NotificationUseCase, *garageDeps) {
	t.Helper()

	d := &garageDeps{
		deps: &deps{
			Mailer: mocks.NewMockNotificationMailer(t), Users: mocks.NewMockNotificationUserReader(t), Log: mocks.NewMockNotificationRepository(t),
		},
		Settings: mocks.NewMockNotificationSettings(t), Suppressions: mocks.NewMockNotificationSuppressions(t),
		Customers: mocks.NewMockNotificationCustomerReader(t), Garages: mocks.NewMockNotificationGarageReader(t),
		Staff: mocks.NewMockNotificationStaffReader(t), Receipts: mocks.NewMockNotificationReceipts(t), Tx: mocks.NewMockNotificationTransactor(t),
	}

	// Nothing has been sent yet unless a test says so.
	d.Log.EXPECT().AlreadySent(mock.Anything, mock.Anything).Return(false, nil).Maybe()

	uc := usecase.NewNotificationUseCase(usecase.Deps{
		Mailer: d.Mailer, Users: d.Users, Log: d.Log, Settings: d.Settings, Suppressions: d.Suppressions, Customers: d.Customers,
		Garages: d.Garages, Staff: d.Staff, Receipts: d.Receipts, Tx: d.Tx, OptOutKey: []byte("test-key"), BaseURL: "https://app.example.com/",
	})

	return uc, d
}

// garageReads expects the settings and the garage that every e-mail reads.
func (d *garageDeps) garageReads(settings domain.Settings) {
	d.Settings.EXPECT().Get(mock.Anything, garageID).Return(settings, nil).Once()
	d.Garages.EXPECT().Get(mock.Anything, garageID).Return(usecase.GarageInfo{
		Name: "Gara Anh Minh", Email: "gara@example.com", Phone: "0901234567", Address: "1 Le Loi",
	}, nil).Once()
}

func customerAn() usecase.CustomerInfo {
	return usecase.CustomerInfo{ID: customerID, Name: "An Nguyen", Email: "customer.an@example.com", Locale: "vi"}
}

func (d *garageDeps) notSuppressed(email string) {
	d.Suppressions.EXPECT().Suppressed(mock.Anything, email).Return(false, nil).Once()
}

// sentMessage captures the message handed to the mailer.
func (d *garageDeps) sentMessage() *mail.Message {
	var msg mail.Message

	d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).
		Run(func(_ mockContext, m mail.Message) { msg = m }).Return(nil).Once()

	return &msg
}

// loggedOnce captures the log rows written.
func (d *garageDeps) logged() *[]domain.Log {
	var rows []domain.Log

	record := func(_ mockContext, l *domain.Log) { rows = append(rows, *l) }

	d.Log.EXPECT().Create(mock.Anything, mock.Anything).Run(record).Return(nil).Maybe()
	d.Log.EXPECT().Sent(mock.Anything, mock.Anything).Run(record).Return(nil).Maybe()

	return &rows
}

func event(garage, eventType, data string) outbox.Event {
	return outbox.Event{ID: "event-1", Type: eventType, GarageID: garage, Data: []byte(data)}
}

// rebuild makes the use case again over d's mocks, for a test that replaced
// one of them.
func rebuild(d *garageDeps) *usecase.NotificationUseCase {
	return usecase.NewNotificationUseCase(usecase.Deps{
		Mailer: d.Mailer, Users: d.Users, Log: d.Log, Settings: d.Settings, Suppressions: d.Suppressions, Customers: d.Customers,
		Garages: d.Garages, Staff: d.Staff, Receipts: d.Receipts, Tx: d.Tx, OptOutKey: []byte("test-key"), BaseURL: "https://app.example.com/",
	})
}
