package main

import (
	"log/slog"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	contactHandler "github.com/viettechno/vietgara-backend/internal/contactrequest/handler"
	contactRepository "github.com/viettechno/vietgara-backend/internal/contactrequest/repository"
	contactUsecase "github.com/viettechno/vietgara-backend/internal/contactrequest/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
)

// buildContactRequests wires the website's contact request endpoint
// (FR-WEB-04). CONTACT_REQUEST_KEY is the key the website's function sends;
// without it the endpoint is off. Support's address is CONTACT_SUPPORT_EMAIL.
func buildContactRequests(pool *platformdb.Pool, mailer mail.Sender, signingKey []byte) *contactHandler.Server {
	uc := contactUsecase.New(
		contactRepository.NewPostgresRepository(pool), mailer,
		envOr("CONTACT_SUPPORT_EMAIL", "support@vietgara.vn"), deriveKey(signingKey, "vietgara/contact-request-source/v1"),
	)

	key := envOr("CONTACT_REQUEST_KEY", "")
	if key != "" && len(key) < minSigningKeyLen {
		slog.Warn("CONTACT_REQUEST_KEY is too short and is ignored: the contact endpoint stays off", "min_length", minSigningKeyLen)

		key = ""
	}

	return contactHandler.NewServer(uc, key)
}

// registerPublicRoutes adds the plain HTTP routes that need no sign-in: the
// e-mail opt-out page, the provider's webhook and the website's contact requests.
func registerPublicRoutes(mux *runtime.ServeMux, notification *notificationModule, contact *contactHandler.Server) error {
	if err := notification.server.RegisterHTTPRoutes(mux, notification.webhookSecret); err != nil {
		return err
	}

	return contact.RegisterHTTPRoutes(mux)
}
