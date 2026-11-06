package main

import (
	"context"
	"encoding/json"

	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	dataRequestHandler "github.com/viettechno/vietgara-backend/internal/datarequest/handler"
	dataRequestRepository "github.com/viettechno/vietgara-backend/internal/datarequest/repository"
	dataRequestUsecase "github.com/viettechno/vietgara-backend/internal/datarequest/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// dataRequestModule is the wired personal-data request module (Release 13.1,
// FR-CUS-05, PD-06): requests recorded by staff and the public page.
type dataRequestModule struct {
	staff      *dataRequestHandler.Server
	public     *dataRequestHandler.PublicServer
	publicCase *dataRequestUsecase.PublicUseCase
}

func buildDataRequest(
	pool *platformdb.Pool, transactor garageTransactor, mailer mail.Sender, customers *customerUsecase.CustomerUseCase,
	customerRepo *customerRepository.PostgresCustomerRepository, users *identityRepo.PostgresUserRepository,
	garages *tenantRepo.PostgresGarageRepository, signingKey []byte,
) *dataRequestModule {
	repo := dataRequestRepository.NewPostgresRepository(pool)
	adapter := dataRequestCustomers{uc: customers, repo: customerRepo}
	names := dataRequestNames{users: users, garages: garages}

	staff := dataRequestUsecase.NewStaffUseCase(repo, adapter, repo, names)
	public := dataRequestUsecase.NewPublicUseCase(dataRequestUsecase.PublicDeps{
		Confirmations: repo, Customers: adapter, Documents: repo, Repo: repo, Names: names, Tx: transactor, Mailer: mailer,
		BaseURL: appBaseURL(), SourceKey: dataRequestKey(signingKey),
	})

	return &dataRequestModule{
		staff: dataRequestHandler.NewServer(staff), public: dataRequestHandler.NewPublicServer(public), publicCase: public,
	}
}

// dataRequestKey derives the key that hashes the source of a public request.
func dataRequestKey(signingKey []byte) []byte {
	return append([]byte("data-request:"), signingKey...)
}

// dataRequestCustomers lets the data request module use the Customer module
// without importing it.
type dataRequestCustomers struct {
	uc   *customerUsecase.CustomerUseCase
	repo *customerRepository.PostgresCustomerRepository
}

func (c dataRequestCustomers) Check(ctx context.Context, garageID, customerID string) error {
	_, err := c.uc.Get(ctx, garageID, customerID)

	return err
}

func (c dataRequestCustomers) Export(ctx context.Context, garageID, customerID string) (json.RawMessage, error) {
	data, err := c.uc.Export(ctx, garageID, customerID)
	if err != nil {
		return nil, err
	}

	return json.Marshal(data)
}

func (c dataRequestCustomers) Anonymize(ctx context.Context, garageID, customerID, actorID string) (dataRequestUsecase.Counts, error) {
	counts, err := c.uc.Anonymize(ctx, garageID, customerID, actorID)

	return dataRequestUsecase.Counts(counts), err
}

func (c dataRequestCustomers) ByEmail(ctx context.Context, address string) ([]dataRequestUsecase.CustomerRef, error) {
	refs, err := c.repo.FindByEmailAcrossGarages(ctx, address)
	if err != nil {
		return nil, err
	}

	out := make([]dataRequestUsecase.CustomerRef, len(refs))
	for i, r := range refs {
		out[i] = dataRequestUsecase.CustomerRef(r)
	}

	return out, nil
}

// dataRequestNames names staff members and garages.
type dataRequestNames struct {
	users   *identityRepo.PostgresUserRepository
	garages *tenantRepo.PostgresGarageRepository
}

func (n dataRequestNames) UserNames(ctx context.Context, ids []string) (map[string]string, error) {
	users, err := n.users.FindByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	names := make(map[string]string, len(users))
	for _, u := range users {
		names[u.ID] = u.FullName
	}

	return names, nil
}

func (n dataRequestNames) GarageName(ctx context.Context, garageID string) (string, error) {
	garage, err := n.garages.FindByID(ctx, garageID)
	if err != nil {
		return "", err
	}

	return garage.Name, nil
}
