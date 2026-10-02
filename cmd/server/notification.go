package main

import (
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	notificationRepository "github.com/viettechno/vietgara-backend/internal/notification/repository"
	notificationUsecase "github.com/viettechno/vietgara-backend/internal/notification/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// notificationModule is the wired Notification module (FRD 3.14, Release
// 1.5): the e-mail channel only (FR-NOT-04) -- a registration-confirmation
// e-mail and license renewal reminders. It has no RPCs of its own; every
// e-mail is triggered by consuming an event another module already
// publishes (see Subscribe, wired into wireEvents).
type notificationModule struct {
	uc *notificationUsecase.NotificationUseCase
}

func buildNotification(pool *platformdb.Pool, mailer mail.Sender, accounts *identityRepo.PostgresAccountRepository) *notificationModule {
	uc := notificationUsecase.NewNotificationUseCase(notificationUsecase.Deps{
		Mailer: mailer, Accounts: notificationAccounts{accounts: accounts}, Log: notificationRepository.NewPostgresRepository(pool),
	})

	return &notificationModule{uc: uc}
}
