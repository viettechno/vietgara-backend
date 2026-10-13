package usecase_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/viettechno/vietgara-backend/internal/notification/domain"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
)

func TestNotificationUseCase_HandleUserEmailVerified(t *testing.T) {
	t.Parallel()

	t.Run("sends a registration confirmation and logs it", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
			return msg.ToEmail == "an@example.com" && msg.Tag == mail.TemplateRegistrationConfirmation && msg.Subject != ""
		})).Return(nil).Once()
		d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
			return log.UserID == userID && log.Channel == domain.ChannelEmail && log.Status == domain.StatusSent &&
				log.Template == mail.TemplateRegistrationConfirmation && log.Recipient == "an@example.com"
		})).Return(nil).Once()

		payload := []byte(`{"user_id":"` + userID + `","email":"an@example.com","full_name":"An Nguyen","locale":"vi"}`)

		err := uc.HandleUserEmailVerified(t.Context(), outbox.Event{UserID: userID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("drops a malformed event", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		err := uc.HandleUserEmailVerified(t.Context(), outbox.Event{Data: []byte(`{`)})
		require.NoError(t, err)
	})

	t.Run("drops an event with no e-mail", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		err := uc.HandleUserEmailVerified(t.Context(), outbox.Event{Data: []byte(`{"user_id":"` + userID + `"}`)})
		require.NoError(t, err)
	})

	t.Run("propagates a send failure so the event is retried", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Mailer.EXPECT().Send(mock.Anything, mock.Anything).Return(errBoom).Once()

		payload := []byte(`{"user_id":"` + userID + `","email":"an@example.com","full_name":"An","locale":"vi"}`)

		err := uc.HandleUserEmailVerified(t.Context(), outbox.Event{Data: payload})
		require.ErrorIs(t, err, errBoom)
	})
}

func TestNotificationUseCase_HandleSubscriptionExpiringSoon(t *testing.T) {
	t.Parallel()

	t.Run("reads the user and sends a renewal reminder", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).
			Return(usecase.UserInfo{Email: "owner@example.com", FullName: "Owner", Locale: "en"}, nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
			// The raw RFC3339 instant is reformatted to a plain date in the
			// user's language (English here), never shown to the
			// customer as-is.
			return msg.ToEmail == "owner@example.com" && msg.Tag == mail.TemplateSubscriptionExpiringSoon &&
				strings.Contains(msg.Text, "Nov 1, 2026") && !strings.Contains(msg.Text, "2026-11-01T00:00:00Z")
		})).Return(nil).Once()
		d.Log.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		payload := []byte(`{"user_id":"` + userID + `","plan_code":"pro","current_period_end":"2026-11-01T00:00:00Z"}`)

		err := uc.HandleSubscriptionExpiringSoon(t.Context(), outbox.Event{UserID: userID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("drops the reminder when the user no longer exists", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).Return(usecase.UserInfo{}, usecase.ErrUserNotFound).Once()

		payload := []byte(`{"user_id":"` + userID + `","plan_code":"pro","current_period_end":"2026-11-01T00:00:00Z"}`)

		err := uc.HandleSubscriptionExpiringSoon(t.Context(), outbox.Event{UserID: userID, Data: payload})
		require.NoError(t, err)
	})

	t.Run("propagates an unexpected user-read failure", func(t *testing.T) {
		t.Parallel()

		uc, d := newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).Return(usecase.UserInfo{}, errBoom).Once()

		payload := []byte(`{"user_id":"` + userID + `"}`)

		err := uc.HandleSubscriptionExpiringSoon(t.Context(), outbox.Event{UserID: userID, Data: payload})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("drops a malformed event", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		err := uc.HandleSubscriptionExpiringSoon(t.Context(), outbox.Event{Data: []byte(`{`)})
		require.NoError(t, err)
	})
}

func TestNotificationUseCase_HandleSubscriptionStatusChanged(t *testing.T) {
	t.Parallel()

	send := func(t *testing.T, to, from, tag, contains string) {
		t.Helper()

		uc, d := newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).
			Return(usecase.UserInfo{Email: "owner@example.com", FullName: "Owner", Locale: "en"}, nil).Once()
		d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
			return msg.ToEmail == "owner@example.com" && msg.Tag == tag && strings.Contains(msg.Text, contains)
		})).Return(nil).Once()
		d.Log.EXPECT().Create(mock.Anything, mock.Anything).Return(nil).Once()

		payload := []byte(`{"user_id":"` + userID + `","from":"` + from + `","to":"` + to + `","plan_code":"pro",` +
			`"grace_period_end":"2026-11-08T00:00:00Z","amount_due":599000,"bank_name":"VCB","account_number":"0123","account_holder":"VietGara"}`)

		require.NoError(t, uc.HandleSubscriptionStatusChanged(t.Context(), outbox.Event{UserID: userID, Data: payload}))
	}

	t.Run("a lapsed plan tells the owner it is read-only until the end of the grace period", func(t *testing.T) {
		t.Parallel()
		send(t, "past_due", "active", mail.TemplateSubscriptionPastDue, "Nov 8, 2026")
	})

	t.Run("a lapsed trial says so", func(t *testing.T) {
		t.Parallel()
		send(t, "past_due", "trial", mail.TemplateSubscriptionPastDue, "trial has ended")
	})

	t.Run("a suspension is e-mailed with the payment details", func(t *testing.T) {
		t.Parallel()
		send(t, "suspended", "past_due", mail.TemplateSubscriptionSuspended, "599000 VND")
	})

	t.Run("other transitions send nothing", func(t *testing.T) {
		t.Parallel()

		uc, _ := newUseCase(t)

		payload := []byte(`{"user_id":"` + userID + `","from":"past_due","to":"active"}`)
		require.NoError(t, uc.HandleSubscriptionStatusChanged(t.Context(), outbox.Event{Data: payload}))
	})

	t.Run("drops a missing user and a malformed event, propagates a read failure", func(t *testing.T) {
		t.Parallel()

		payload := []byte(`{"user_id":"` + userID + `","to":"past_due"}`)

		uc, d := newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).Return(usecase.UserInfo{}, usecase.ErrUserNotFound).Once()
		require.NoError(t, uc.HandleSubscriptionStatusChanged(t.Context(), outbox.Event{Data: payload}))

		uc, _ = newUseCase(t)
		require.NoError(t, uc.HandleSubscriptionStatusChanged(t.Context(), outbox.Event{Data: []byte(`{`)}))

		uc, d = newUseCase(t)
		d.Users.EXPECT().Get(mock.Anything, userID).Return(usecase.UserInfo{}, errBoom).Once()
		require.ErrorIs(t, uc.HandleSubscriptionStatusChanged(t.Context(), outbox.Event{Data: payload}), errBoom)
	})
}

func TestNotificationUseCase_HandlePaymentSubmitted(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	d.Users.EXPECT().PlatformAdmins(mock.Anything).
		Return([]usecase.UserInfo{
			{ID: "admin-1", Email: "admin1@example.com", FullName: "Admin One", Locale: "en"},
			{ID: "admin-2", Email: "admin2@example.com", FullName: "Admin Two", Locale: "vi"},
		}, nil).Once()

	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "admin1@example.com" && msg.Tag == mail.TemplatePaymentSubmitted
	})).Return(nil).Once()
	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "admin2@example.com" && msg.Tag == mail.TemplatePaymentSubmitted
	})).Return(nil).Once()

	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == "admin-1" && log.Channel == domain.ChannelEmail && log.Template == mail.TemplatePaymentSubmitted
	})).Return(nil).Once()
	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == "admin-2" && log.Channel == domain.ChannelEmail && log.Template == mail.TemplatePaymentSubmitted
	})).Return(nil).Once()

	payload := []byte(`{"user_id":"user-123","plan_code":"pro","reference":"REF-123","amount_due":100000,"transferred_amount":100000,"amount_mismatch":false,"reason":"","waiting":1,"oldest_submitted_at":"2026-10-01T00:00:00Z"}`)

	err := uc.HandlePaymentSubmitted(t.Context(), outbox.Event{UserID: "user-123", Data: payload})
	require.NoError(t, err)
}

func TestNotificationUseCase_HandlePaymentReviewReminder(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	d.Users.EXPECT().PlatformAdmins(mock.Anything).
		Return([]usecase.UserInfo{
			{ID: "admin-1", Email: "admin1@example.com", FullName: "Admin One", Locale: "en"},
		}, nil).Once()

	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "admin1@example.com" && msg.Tag == mail.TemplatePaymentReviewReminder
	})).Return(nil).Once()

	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == "admin-1" && log.Channel == domain.ChannelEmail && log.Template == mail.TemplatePaymentReviewReminder
	})).Return(nil).Once()

	payload := []byte(`{"waiting":5,"oldest_submitted_at":"2026-10-01T00:00:00Z"}`)

	err := uc.HandlePaymentReviewReminder(t.Context(), outbox.Event{Data: payload})
	require.NoError(t, err)
}

func TestNotificationUseCase_HandlePaymentApproved(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	d.Users.EXPECT().Get(mock.Anything, userID).
		Return(usecase.UserInfo{ID: userID, Email: "owner@example.com", FullName: "Owner", Locale: "en"}, nil).Once()

	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "owner@example.com" && msg.Tag == mail.TemplatePaymentApproved
	})).Return(nil).Once()

	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == userID && log.Channel == domain.ChannelEmail && log.Template == mail.TemplatePaymentApproved
	})).Return(nil).Once()

	payload := []byte(`{"user_id":"` + userID + `","plan_code":"pro","reference":"REF-123","amount_due":100000,"transferred_amount":100000,"amount_mismatch":false,"reason":"","waiting":0,"oldest_submitted_at":""}`)

	err := uc.HandlePaymentApproved(t.Context(), outbox.Event{UserID: userID, Data: payload})
	require.NoError(t, err)
}

func TestNotificationUseCase_HandlePaymentRejected(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	d.Users.EXPECT().Get(mock.Anything, userID).
		Return(usecase.UserInfo{ID: userID, Email: "owner@example.com", FullName: "Owner", Locale: "en"}, nil).Once()

	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "owner@example.com" && msg.Tag == mail.TemplatePaymentRejected
	})).Return(nil).Once()

	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == userID && log.Channel == domain.ChannelEmail && log.Template == mail.TemplatePaymentRejected
	})).Return(nil).Once()

	payload := []byte(`{"user_id":"` + userID + `","plan_code":"pro","reference":"REF-123","amount_due":100000,"transferred_amount":50000,"amount_mismatch":true,"reason":"amount mismatch","waiting":0,"oldest_submitted_at":""}`)

	err := uc.HandlePaymentRejected(t.Context(), outbox.Event{UserID: userID, Data: payload})
	require.NoError(t, err)
}

func TestNotificationUseCase_FormatPeriodEnd(t *testing.T) {
	t.Parallel()

	uc := usecase.NewNotificationUseCase(usecase.Deps{})
	_ = uc

	tests := []struct {
		name     string
		input    string
		locale   string
		expected string
	}{
		{"Vietnamese locale", "2026-10-25T00:00:00Z", "vi", "25/10/2026"},
		{"English locale", "2026-10-25T00:00:00Z", "en", "Oct 25, 2026"},
		{"Invalid RFC3339 falls back", "not-a-date", "vi", "not-a-date"},
		{"Empty string", "", "vi", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := usecase.FormatPeriodEndForTest(tt.input, tt.locale)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNotificationUseCase_Send(t *testing.T) {
	t.Parallel()

	uc, d := newUseCase(t)

	d.Mailer.EXPECT().Send(mock.Anything, mock.MatchedBy(func(msg mail.Message) bool {
		return msg.ToEmail == "test@example.com" && msg.Tag == mail.TemplateRegistrationConfirmation
	})).Return(nil).Once()

	d.Log.EXPECT().Create(mock.Anything, mock.MatchedBy(func(log *domain.Log) bool {
		return log.UserID == userID && log.Channel == domain.ChannelEmail && log.Template == mail.TemplateRegistrationConfirmation && log.Recipient == "test@example.com"
	})).Return(nil).Once()

	err := uc.SendForTest(t.Context(), userID, mail.TemplateRegistrationConfirmation, "en", "test@example.com", "Test User", map[string]any{"Name": "Test User"})
	require.NoError(t, err)
}
