package main

import (
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/access"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	supportHandler "github.com/viettechno/vietgara-backend/internal/support/handler"
	supportRepository "github.com/viettechno/vietgara-backend/internal/support/repository"
	supportUsecase "github.com/viettechno/vietgara-backend/internal/support/usecase"
)

// supportModule is the read-only support access of Release 8.2.
type supportModule struct {
	uc     *supportUsecase.UseCase
	server *supportHandler.Server
}

// buildSupport wires the support module. Its use case is also the request
// pipeline's middleware.SupportSessions: it lets admin staff read a garage
// they hold an active grant for.
func buildSupport(
	pool *platformdb.Pool, tx middleware.Transactor, platform access.PlatformReader, auditLog *audit.Logger, mailer mail.Sender,
) *supportModule {
	uc := supportUsecase.New(supportUsecase.Deps{
		Repo: supportRepository.NewPostgres(pool), Platform: platform, Audit: auditLog, Mailer: mailer, Tx: tx,
		OwnerBaseURL: appBaseURL(),
	})

	return &supportModule{uc: uc, server: supportHandler.NewServer(uc)}
}
