package usecase_test

import (
	"errors"
	"testing"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
)

const userID = "11111111-1111-1111-1111-111111111111"

var errBoom = errors.New("boom")

// deps are the mocked dependencies of one NotificationUseCase under test.
type deps struct {
	Mailer *mocks.MockNotificationMailer
	Users  *mocks.MockNotificationUserReader
	Log    *mocks.MockNotificationRepository
}

func newUseCase(t *testing.T) (*usecase.NotificationUseCase, *deps) {
	t.Helper()

	d := &deps{
		Mailer: mocks.NewMockNotificationMailer(t), Users: mocks.NewMockNotificationUserReader(t),
		Log: mocks.NewMockNotificationRepository(t),
	}

	uc := usecase.NewNotificationUseCase(usecase.Deps{Mailer: d.Mailer, Users: d.Users, Log: d.Log})

	return uc, d
}
