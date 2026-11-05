package handler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	receivablev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/receivable/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/receivable/domain"
	"github.com/viettechno/vietgara-backend/internal/receivable/handler"
	"github.com/viettechno/vietgara-backend/internal/receivable/usecase"
)

const (
	garageID     = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	receivableID = "11111111-1111-1111-1111-111111111111"
	actorID      = "55555555-5555-5555-5555-555555555555"
)

var (
	now     = time.Date(2026, time.November, 20, 3, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	srv     *handler.Server
	repo    *mocks.MockReceivableRepository
	garages *mocks.MockReceivableGarageReader
	paidOut *mocks.MockReceivablePaidOutReader
	audit   *mocks.MockReceivableAuditLogger
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		repo: mocks.NewMockReceivableRepository(t), garages: mocks.NewMockReceivableGarageReader(t),
		paidOut: mocks.NewMockReceivablePaidOutReader(t), audit: mocks.NewMockReceivableAuditLogger(t),
	}
	e.garages.EXPECT().Timezone(mock.Anything, garageID).Return("Asia/Ho_Chi_Minh", nil).Maybe()

	uc := usecase.NewReceivableUseCase(usecase.Deps{
		Repo: e.repo, Garages: e.garages, PaidOut: e.paidOut, Audit: e.audit, Now: func() time.Time { return now },
	})
	e.srv = handler.NewServer(uc)

	return e
}

func signedIn(t *testing.T) context.Context {
	t.Helper()

	claims := &auth.Claims{}
	claims.Subject = actorID

	return middleware.WithClaims(t.Context(), claims)
}

func receivable() *domain.Receivable {
	reminded := now
	due := time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC)

	return &domain.Receivable{
		ID: receivableID, GarageID: garageID, SettlementID: "s-1", SettlementNumber: "QT-0001", CustomerID: "c-1", CustomerName: "An",
		VehicleLicensePlate: "51A-111.11", TotalAmount: 1_000_000, PaidAmount: 400_000, DueDate: due, Status: domain.StatusOpen,
		RemindersSent: 1, LastReminderAt: &reminded, CreatedAt: now, UpdatedAt: now,
		Entries: []domain.Entry{
			{ID: "e-1", Kind: domain.EntryCharge, Amount: 1_000_000, Reference: "QT-0001", OccurredAt: now},
			{ID: "e-2", Kind: domain.EntryPayment, Amount: 400_000, Reference: "PT-1", Method: "cash", OccurredAt: now},
			{ID: "e-3", Kind: domain.EntryVoid, Amount: 400_000, Reference: "PT-1", Method: "bank_transfer", OccurredAt: now},
			{ID: "e-4", Kind: domain.EntryPayment, Amount: 1, Reference: "PT-2", Method: "online", OccurredAt: now},
			{ID: "e-5", Kind: domain.EntryKind("other"), Amount: 1, Method: "weird", OccurredAt: now},
		},
		Reminders: []domain.Reminder{
			{ID: "m-1", OffsetDays: 3, Channel: "email", Outcome: domain.OutcomeSent, CreatedAt: now},
			{ID: "m-2", OffsetDays: 7, Channel: "email", Outcome: domain.OutcomeSkipped, Reason: "NO_EMAIL", CreatedAt: now},
			{ID: "m-3", OffsetDays: 9, Channel: "email", Outcome: domain.OutcomeFailed, CreatedAt: now},
			{ID: "m-4", OffsetDays: 11, Channel: "email", Outcome: domain.Outcome("?"), CreatedAt: now},
		},
	}
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	p := handler.Policies()
	assert.Len(t, p, 5)
	assert.NotNil(t, p[receivablev1.ReceivableService_UpdateReceivableSettings_FullMethodName].Garage)
}

func TestListReceivables(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	paid := receivable()
	paid.Status, paid.PaidAmount = domain.StatusPaid, paid.TotalAmount
	e.repo.EXPECT().List(mock.Anything, garageID, mock.MatchedBy(func(f usecase.ListFilter) bool {
		return f.CustomerID == "c-1" && f.OverdueOnly && f.IncludePaid && f.Query == "51A" && f.Limit == 10 && f.Offset == 10
	})).Return([]*domain.Receivable{receivable(), paid}, 12, usecase.Summary{OpenCount: 3, OutstandingAmount: 5, OverdueCount: 2, OverdueAmount: 4}, nil).Once()

	resp, err := e.srv.ListReceivables(signedIn(t), &receivablev1.ListReceivablesRequest{
		GarageId: garageID, CustomerId: "c-1", OverdueOnly: true, IncludePaid: true, Query: "51A", Page: 2, PageSize: 10,
	})
	require.NoError(t, err)
	require.Len(t, resp.GetData(), 2)
	assert.EqualValues(t, 12, resp.GetPagination().GetTotal())
	assert.EqualValues(t, 3, resp.GetSummary().GetOpenCount())
	assert.EqualValues(t, 4, resp.GetSummary().GetOverdueAmount())

	first := resp.GetData()[0]
	assert.Equal(t, "2026-11-17", first.GetDueDate())
	assert.True(t, first.GetOverdue())
	assert.EqualValues(t, 3, first.GetDaysOverdue())
	assert.EqualValues(t, 600_000, first.GetRemainingAmount())
	assert.Equal(t, receivablev1.ReceivableStatus_RECEIVABLE_STATUS_OPEN, first.GetStatus())
	assert.NotNil(t, first.GetLastReminderAt())
	assert.Equal(t, receivablev1.ReceivableStatus_RECEIVABLE_STATUS_PAID, resp.GetData()[1].GetStatus())
	assert.Empty(t, first.GetEntries(), "a list carries no ledger")

	e.repo.EXPECT().List(mock.Anything, garageID, mock.Anything).Return(nil, 0, usecase.Summary{}, errBoom).Once()
	_, err = e.srv.ListReceivables(signedIn(t), &receivablev1.ListReceivablesRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGetReceivable(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(receivable(), nil).Once()

	got, err := e.srv.GetReceivable(signedIn(t), &receivablev1.GetReceivableRequest{GarageId: garageID, ReceivableId: receivableID})
	require.NoError(t, err)
	require.Len(t, got.GetEntries(), 5)
	assert.Equal(t, receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_PAYMENT, got.GetEntries()[1].GetKind())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, got.GetEntries()[1].GetMethod())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_BANK_TRANSFER, got.GetEntries()[2].GetMethod())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_ONLINE, got.GetEntries()[3].GetMethod())
	assert.Equal(t, receivablev1.ReceivableEntryKind_RECEIVABLE_ENTRY_KIND_UNSPECIFIED, got.GetEntries()[4].GetKind())
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_UNSPECIFIED, got.GetEntries()[4].GetMethod())
	require.Len(t, got.GetReminders(), 4)
	assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_SKIPPED, got.GetReminders()[1].GetOutcome())
	assert.Equal(t, "NO_EMAIL", got.GetReminders()[1].GetReason())
	assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_FAILED, got.GetReminders()[2].GetOutcome())
	assert.Equal(t, receivablev1.ReminderOutcome_REMINDER_OUTCOME_UNSPECIFIED, got.GetReminders()[3].GetOutcome())

	e.repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(nil, domain.ErrReceivableNotFound).Once()
	_, err = e.srv.GetReceivable(signedIn(t), &receivablev1.GetReceivableRequest{GarageId: garageID, ReceivableId: receivableID})
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestReceivableStatusUnspecified(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	odd := receivable()
	odd.Status = domain.Status("odd")
	e.repo.EXPECT().FindByID(mock.Anything, garageID, receivableID).Return(odd, nil).Once()

	got, err := e.srv.GetReceivable(signedIn(t), &receivablev1.GetReceivableRequest{GarageId: garageID, ReceivableId: receivableID})
	require.NoError(t, err)
	assert.Equal(t, receivablev1.ReceivableStatus_RECEIVABLE_STATUS_UNSPECIFIED, got.GetStatus())
}

func TestReceivableSettings(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.DefaultSettings(), nil).Times(2)

	got, err := e.srv.GetReceivableSettings(signedIn(t), &receivablev1.GetReceivableSettingsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.EqualValues(t, 7, got.GetPaymentTermsDays())
	assert.True(t, got.GetRemindersEnabled())
	assert.Equal(t, []int32{3, 7}, got.GetReminderOffsetDays())

	e.repo.EXPECT().SaveSettings(mock.Anything, garageID, domain.Settings{
		PaymentTermsDays: 14, RemindersEnabled: false, ReminderOffsetDays: []int{2, 9},
	}).Return(nil).Once()
	e.audit.EXPECT().Record(mock.Anything, mock.Anything).Return(nil).Once()

	days, off := int32(14), false
	saved, err := e.srv.UpdateReceivableSettings(signedIn(t), &receivablev1.UpdateReceivableSettingsRequest{
		GarageId: garageID, Settings: &receivablev1.ReceivableSettings{PaymentTermsDays: &days, RemindersEnabled: &off, ReminderOffsetDays: []int32{9, 2}},
	})
	require.NoError(t, err)
	assert.Equal(t, []int32{2, 9}, saved.GetReminderOffsetDays())
	assert.False(t, saved.GetRemindersEnabled())

	for name, in := range map[string]*receivablev1.ReceivableSettings{
		"nothing": nil, "no terms": {RemindersEnabled: &off}, "no switch": {PaymentTermsDays: &days},
	} {
		_, err = e.srv.UpdateReceivableSettings(signedIn(t), &receivablev1.UpdateReceivableSettingsRequest{GarageId: garageID, Settings: in})
		require.Equal(t, codes.InvalidArgument, status.Code(err), name)
	}

	e.repo.EXPECT().GetSettings(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()
	_, err = e.srv.GetReceivableSettings(signedIn(t), &receivablev1.GetReceivableSettingsRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestGetReconciliationReport(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	at := time.Date(2026, 11, 3, 5, 0, 0, 0, time.UTC)

	e.repo.EXPECT().BalanceBefore(mock.Anything, garageID, mock.Anything).Return(100, nil).Once()
	e.repo.EXPECT().Entries(mock.Anything, garageID, mock.Anything, mock.Anything).Return([]usecase.LedgerLine{
		{Entry: domain.Entry{Kind: domain.EntryCharge, Amount: 1000, Reference: "QT", OccurredAt: at}, CustomerName: "An"},
		{Entry: domain.Entry{Kind: domain.EntryPayment, Amount: 400, Reference: "PT", Method: "cash", OccurredAt: at}, CustomerName: "An"},
	}, nil).Once()
	e.repo.EXPECT().LedgerBalance(mock.Anything, garageID).Return(700, nil).Once()
	e.repo.EXPECT().RemainingTotal(mock.Anything, garageID).Return(700, nil).Once()
	e.paidOut.EXPECT().PaidOut(mock.Anything, garageID, mock.Anything, mock.Anything).Return(50, nil).Once()

	rep, err := e.srv.GetReconciliationReport(signedIn(t), &receivablev1.GetReconciliationReportRequest{GarageId: garageID, From: "2026-11-01", To: "2026-12-01"})
	require.NoError(t, err)
	assert.EqualValues(t, 100, rep.GetOpeningBalance())
	assert.EqualValues(t, 700, rep.GetClosingBalance())
	assert.EqualValues(t, 50, rep.GetPaidOutAmount())
	assert.True(t, rep.GetMatches())
	require.Len(t, rep.GetCollectedByMethod(), 1)
	assert.Equal(t, billingv1.PaymentMethod_PAYMENT_METHOD_CASH, rep.GetCollectedByMethod()[0].GetMethod())
	require.Len(t, rep.GetLines(), 2)
	assert.Equal(t, "2026-11-03", rep.GetLines()[0].GetDate())

	_, err = e.srv.GetReconciliationReport(signedIn(t), &receivablev1.GetReconciliationReportRequest{GarageId: garageID, From: "bad", To: "2026-12-01"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

type bus map[string]outbox.Handler

func (b bus) Subscribe(eventType string, h outbox.Handler) { b[eventType] = h }

func TestSubscribe(t *testing.T) {
	t.Parallel()

	b := bus{}
	handler.Subscribe(b, outbox.NewProcessor(nil, nil), usecase.NewReceivableUseCase(usecase.Deps{}))

	assert.Len(t, b, 3)
	assert.Contains(t, b, "settlement.created")
	assert.Contains(t, b, "payment_voucher.issued")
	assert.Contains(t, b, "payment_voucher.voided")
	assert.Equal(t, "receivable", handler.ConsumerName)
}
