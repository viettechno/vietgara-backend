package usecase_test

import (
	"errors"
	"testing"

	"github.com/viettechno/vietgara-backend/internal/mocks"
	"github.com/viettechno/vietgara-backend/internal/notification/usecase"
)

const accountID = "11111111-1111-1111-1111-111111111111"

var errBoom = errors.New("boom")

// deps are the mocked dependencies of one NotificationUseCase under test.
type deps struct {
	Mailer   *mocks.MockNotificationMailer
	Accounts *mocks.MockNotificationAccountReader
	Log      *mocks.MockNotificationRepository
}

func newUseCase(t *testing.T) (*usecase.NotificationUseCase, *deps) {
	t.Helper()

	d := &deps{
		Mailer: mocks.NewMockNotificationMailer(t), Accounts: mocks.NewMockNotificationAccountReader(t),
		Log: mocks.NewMockNotificationRepository(t),
	}

	uc := usecase.NewNotificationUseCase(usecase.Deps{Mailer: d.Mailer, Accounts: d.Accounts, Log: d.Log})

	return uc, d
}
