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
