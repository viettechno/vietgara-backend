package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	notificationv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/notification/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/handler"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
)

const (
	garageID   = "22222222-2222-2222-2222-222222222222"
	customerID = "33333333-3333-3333-3333-333333333333"
)

var errBoom = errors.New("boom")

type env struct {
	srv          *handler.Server
	uc           *usecase.NotificationUseCase
	settings     *mocks.MockNotificationSettings
	suppressions *mocks.MockNotificationSuppressions
	customers    *mocks.MockNotificationCustomerReader
	log          *mocks.MockNotificationRepository
	tx           *mocks.MockNotificationTransactor
}

func newEnv(t *testing.T) *env {
	t.Helper()

	e := &env{
		settings: mocks.NewMockNotificationSettings(t), suppressions: mocks.NewMockNotificationSuppressions(t),
		customers: mocks.NewMockNotificationCustomerReader(t), log: mocks.NewMockNotificationRepository(t),
		tx: mocks.NewMockNotificationTransactor(t),
	}
	e.uc = usecase.NewNotificationUseCase(usecase.Deps{
		Log: e.log, Settings: e.settings, Suppressions: e.suppressions, Customers: e.customers, Tx: e.tx, OptOutKey: []byte("key"),
	})
	e.srv = handler.NewServer(e.uc)

	return e
}

func (e *env) mux(t *testing.T, webhookSecret string) *runtime.ServeMux {
	t.Helper()

	mux := runtime.NewServeMux()
	require.NoError(t, e.srv.RegisterHTTPRoutes(mux, webhookSecret))

	return mux
}

func call(mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	return rec
}

func TestPolicies(t *testing.T) {
	t.Parallel()

	assert.Len(t, handler.Policies(), 3)
}

func TestGetNotificationSettings(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{
		SenderName: "Gara", Off: map[domain.Type]bool{domain.TypeVehicleReady: true},
	}, nil).Once()
	e.settings.EXPECT().Get(mock.Anything, garageID).Return(domain.Settings{}, errBoom).Once()

	resp, err := e.srv.GetNotificationSettings(t.Context(), &notificationv1.GetNotificationSettingsRequest{GarageId: garageID})
	require.NoError(t, err)
	assert.Equal(t, "Gara", resp.GetSenderName())
	require.Len(t, resp.GetTypes(), 7)

	for _, setting := range resp.GetTypes() {
		switch setting.GetType() {
		case notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY:
			assert.False(t, setting.GetEnabled())
			assert.Equal(t, notificationv1.NotificationAudience_NOTIFICATION_AUDIENCE_CUSTOMER, setting.GetAudience())
			assert.False(t, setting.GetEssential())
		case notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_READY:
			assert.True(t, setting.GetEnabled())
			assert.True(t, setting.GetEssential())
		case notificationv1.NotificationType_NOTIFICATION_TYPE_LOW_STOCK:
			assert.Equal(t, notificationv1.NotificationAudience_NOTIFICATION_AUDIENCE_STAFF, setting.GetAudience())
		default:
		}
	}

	_, err = e.srv.GetNotificationSettings(t.Context(), &notificationv1.GetNotificationSettingsRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestUpdateNotificationSettings(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	e.settings.EXPECT().Save(mock.Anything, garageID, mock.MatchedBy(func(s domain.Settings) bool {
		return s.SenderName == "Gara" && s.Off[domain.TypeLowStock] && len(s.Off) == 1
	})).Return(nil).Once()

	resp, err := e.srv.UpdateNotificationSettings(t.Context(), &notificationv1.UpdateNotificationSettingsRequest{
		GarageId: garageID, SenderName: "Gara",
		Types: []*notificationv1.NotificationTypeSetting{
			{Type: notificationv1.NotificationType_NOTIFICATION_TYPE_LOW_STOCK, Enabled: false},
			{Type: notificationv1.NotificationType_NOTIFICATION_TYPE_QUOTE_DECIDED, Enabled: true},
		},
	})
	require.NoError(t, err)

	for _, setting := range resp.GetTypes() {
		assert.Equal(t, setting.GetType() != notificationv1.NotificationType_NOTIFICATION_TYPE_LOW_STOCK, setting.GetEnabled())
	}

	for name, req := range map[string]*notificationv1.UpdateNotificationSettingsRequest{
		"unspecified type": {GarageId: garageID, Types: []*notificationv1.NotificationTypeSetting{{}}},
		"long name":        {GarageId: garageID, SenderName: strings.Repeat("a", 101)},
	} {
		_, err := e.srv.UpdateNotificationSettings(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err), name)
	}

	e.settings.EXPECT().Save(mock.Anything, garageID, mock.Anything).Return(errBoom).Once()

	_, err = e.srv.UpdateNotificationSettings(t.Context(), &notificationv1.UpdateNotificationSettingsRequest{GarageId: garageID})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestListNotificationLog(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	e := newEnv(t)
	e.log.EXPECT().List(mock.Anything, garageID, usecase.LogFilter{Type: domain.TypeVehicleReady, Status: domain.StatusSkipped}, 2, 50).
		Return([]domain.Log{{
			ID: "log-1", Type: domain.TypeVehicleReady, Recipient: "an@example.com", CustomerID: customerID, EntityType: "repair_order",
			EntityID: "ro-1", Status: domain.StatusSkipped, SkipReason: domain.SkipOptedOut, CreatedAt: at,
		}}, 51, nil).Once()
	e.log.EXPECT().List(mock.Anything, garageID, usecase.LogFilter{}, 1, 100).Return(nil, 0, errBoom).Once()

	resp, err := e.srv.ListNotificationLog(t.Context(), &notificationv1.ListNotificationLogRequest{
		GarageId: garageID, Page: 2, PageSize: 50,
		Type:   notificationv1.NotificationType_NOTIFICATION_TYPE_VEHICLE_READY,
		Status: notificationv1.NotificationStatus_NOTIFICATION_STATUS_SKIPPED,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(51), resp.GetPagination().GetTotal())
	assert.Equal(t, int32(2), resp.GetPagination().GetPage())

	entry := resp.GetData()[0]
	assert.Equal(t, notificationv1.NotificationSkipReason_NOTIFICATION_SKIP_REASON_OPTED_OUT, entry.GetReason())
	assert.Equal(t, notificationv1.NotificationStatus_NOTIFICATION_STATUS_SKIPPED, entry.GetStatus())
	assert.Equal(t, "an@example.com", entry.GetRecipient())
	assert.Equal(t, at, entry.GetCreatedAt().AsTime())

	_, err = e.srv.ListNotificationLog(t.Context(), &notificationv1.ListNotificationLogRequest{GarageId: garageID, PageSize: 1000})
	require.Equal(t, codes.Internal, status.Code(err))
}

func TestOptOutPage(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	token := e.uc.OptOutTokenForTest(garageID, customerID)
	mux := e.mux(t, "")

	// The page changes nothing: no mock is called.
	rec := call(mux, http.MethodGet, "/api/v1/public/email-opt-out/"+token, "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, rec.Body.String(), `<form method="post" action="/api/v1/public/email-opt-out/`+token+`">`)

	rec = call(mux, http.MethodGet, "/api/v1/public/email-opt-out/"+token+"x", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOptOut(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	token := e.uc.OptOutTokenForTest(garageID, customerID)
	mux := e.mux(t, "")

	e.tx.EXPECT().InGarage(mock.Anything, garageID, mock.Anything).
		RunAndReturn(func(ctx context.Context, _ string, fn func(context.Context) error) error { return fn(ctx) }).Twice()
	e.customers.EXPECT().OptOut(mock.Anything, garageID, customerID).Return(nil).Once()
	e.customers.EXPECT().OptOut(mock.Anything, garageID, customerID).Return(errBoom).Once()

	rec := call(mux, http.MethodPost, "/api/v1/public/email-opt-out/"+token, "List-Unsubscribe=One-Click")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Unsubscribed")

	rec = call(mux, http.MethodPost, "/api/v1/public/email-opt-out/"+token, "")
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	rec = call(mux, http.MethodPost, "/api/v1/public/email-opt-out/not-a-token", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMailjetWebhook(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	mux := e.mux(t, "s3cret")

	e.suppressions.EXPECT().Suppress(mock.Anything, "bounced@example.com", "bounce").Return(nil).Once()
	e.suppressions.EXPECT().Suppress(mock.Anything, "blocked@example.com", "blocked").Return(nil).Once()
	e.suppressions.EXPECT().Suppress(mock.Anything, "spam@example.com", "spam").Return(nil).Once()
	e.suppressions.EXPECT().Suppress(mock.Anything, "single@example.com", "bounce").Return(nil).Once()

	events := `[
		{"event":"bounce","email":"bounced@example.com","hard_bounce":true},
		{"event":"bounce","email":"soft@example.com","hard_bounce":false},
		{"event":"blocked","email":"blocked@example.com"},
		{"event":"spam","email":"spam@example.com"},
		{"event":"sent","email":"fine@example.com"}
	]`
	assert.Equal(t, http.StatusOK, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/s3cret", events).Code)
	assert.Equal(t, http.StatusOK, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/s3cret",
		`{"event":"bounce","email":"single@example.com","hard_bounce":true}`).Code)
	assert.Equal(t, http.StatusOK, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/s3cret", `garbage`).Code, "nothing to do")

	assert.Equal(t, http.StatusNotFound, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/wrong", events).Code)

	e.suppressions.EXPECT().Suppress(mock.Anything, "bounced@example.com", "bounce").Return(errBoom).Once()
	assert.Equal(t, http.StatusInternalServerError, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/s3cret", events).Code)
}

func TestWebhookIsOffWithoutASecret(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	mux := e.mux(t, "")

	assert.Equal(t, http.StatusNotFound, call(mux, http.MethodPost, "/api/v1/webhooks/mailjet/anything", `[]`).Code)
}
