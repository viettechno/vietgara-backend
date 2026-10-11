package main

import (
	"context"

	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// buildCustomerLinks wires FR-CUS-06: a garage links a customer profile with
// the user that owns its verified e-mail, confirmed by a one-time code from
// the Identity module's OTP infrastructure or by the user in the app.
func buildCustomerLinks(
	pool *platformdb.Pool,
	customers *customerRepository.PostgresCustomerRepository,
	identity *identityUsecase.IdentityUseCase,
	garages *tenantRepo.PostgresGarageRepository,
	auditLog *audit.Logger,
) *customerUsecase.LinkUseCase {
	return customerUsecase.NewLinkUseCase(customerUsecase.LinkDeps{
		Links:     customerRepository.NewPostgresLinkRepository(pool),
		Customers: customers,
		Users:     linkUsers{identity: identity},
		Codes:     linkCodes{identity: identity},
		Garages:   linkGarages{garages: garages},
		Audit:     auditLog,
	})
}

// linkUsers answers the platform-wide "who owns this verified e-mail".
type linkUsers struct {
	identity *identityUsecase.IdentityUseCase
}

func (u linkUsers) FindVerifiedByEmail(ctx context.Context, email string) (string, bool, error) {
	return u.identity.FindVerifiedUserIDByEmail(ctx, email)
}

// linkCodes sends and checks the code of a link request.
type linkCodes struct {
	identity *identityUsecase.IdentityUseCase
}

func (c linkCodes) Send(ctx context.Context, userID, linkID, garageName string) error {
	return c.identity.SendCustomerLinkCode(ctx, userID, linkID, garageName)
}

func (c linkCodes) Check(ctx context.Context, linkID, code string) error {
	return c.identity.CheckCustomerLinkCode(ctx, linkID, code)
}

// linkGarages names the garage a request comes from.
type linkGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (g linkGarages) Name(ctx context.Context, garageID string) (string, error) {
	garage, err := g.garages.FindByID(ctx, garageID)
	if err != nil {
		return "", err
	}

	return garage.Name, nil
}
