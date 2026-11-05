package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	garageID     = "11111111-1111-1111-1111-111111111111"
	settlementID = "33333333-3333-3333-3333-333333333333"
	receivableID = "44444444-4444-4444-4444-444444444444"
	customerID   = "55555555-5555-5555-5555-555555555555"
	actorID      = "66666666-6666-6666-6666-666666666666"
)

var (
	errBoom = errors.New("boom")
	// now is 10:00 on 2026-11-20 in Ho Chi Minh City.
	now = time.Date(2026, time.November, 20, 3, 0, 0, 0, time.UTC)
)

type deps struct {
	Repo        *mocks.MockReceivableRepository
	Settlements *mocks.MockReceivableSettlementReader
	Garages     *mocks.MockReceivableGarageReader
	Sender      *mocks.MockReceivableReminderSender
	PaidOut     *mocks.MockReceivablePaidOutReader
	Tx          *mocks.MockReceivableTransactor
	Audit       *mocks.MockReceivableAuditLogger
}

func newUseCase(t *testing.T) (*usecase.ReceivableUseCase, *deps) {
	t.Helper()

	d := &deps{
		Repo: mocks.NewMockReceivableRepository(t), Settlements: mocks.NewMockReceivableSettlementReader(t),
		Garages: mocks.NewMockReceivableGarageReader(t), Sender: mocks.NewMockReceivableReminderSender(t),
		PaidOut: mocks.NewMockReceivablePaidOutReader(t), Tx: mocks.NewMockReceivableTransactor(t), Audit: mocks.NewMockReceivableAuditLogger(t),
	}

	uc := usecase.NewReceivableUseCase(usecase.Deps{
		Repo: d.Repo, Settlements: d.Settlements, Garages: d.Garages, Sender: d.Sender, PaidOut: d.PaidOut, Tx: d.Tx, Audit: d.Audit,
		Now: func() time.Time { return now },
	})

	return uc, d
}

// inGarage makes the transactor run its function, any number of times.
func (d *deps) inGarage() {
	d.Tx.EXPECT().InGarage(mock.Anything, garageID, mock.Anything).RunAndReturn(
		func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }).Maybe()
}

func (d *deps) timezone() {
	d.Garages.EXPECT().Timezone(mock.Anything, garageID).Return("Asia/Ho_Chi_Minh", nil).Maybe()
}

func expectAudit(d *deps) *audit.Entry {
	var got audit.Entry

	d.Audit.EXPECT().Record(mock.Anything, mock.Anything).
		Run(func(_ context.Context, e audit.Entry) { got = e }).Return(nil).Once()

	return &got
}

func assertAppError(t *testing.T, err error, code apperror.Code) {
	t.Helper()

	require.Error(t, err)

	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected an *apperror.AppError, got %T: %v", err, err)
	require.Equal(t, code, appErr.Code)
}

func openReceivable(dueDate time.Time) *domain.Receivable {
	return &domain.Receivable{
		ID: receivableID, GarageID: garageID, SettlementID: settlementID, SettlementNumber: "QT-0001", CustomerID: customerID,
		CustomerName: "An", VehicleLicensePlate: "51A-111.11", TotalAmount: 1_000_000, PaidAmount: 400_000, DueDate: dueDate,
		Status: domain.StatusOpen,
	}
}

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
