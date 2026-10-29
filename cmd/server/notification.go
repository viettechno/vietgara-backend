package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	customerDomain "github.com/viettechno/vietgara-backend/internal/customer/domain"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	documentUsecase "github.com/viettechno/vietgara-backend/internal/document/usecase"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	notificationHandler "github.com/viettechno/vietgara-backend/internal/notification/handler"
	notificationRepository "github.com/viettechno/vietgara-backend/internal/notification/repository"
	notificationUsecase "github.com/viettechno/vietgara-backend/internal/notification/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	quoteUsecase "github.com/viettechno/vietgara-backend/internal/quote/usecase"
	tenantDomain "github.com/viettechno/vietgara-backend/internal/tenant/domain"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// notificationModule is the wired Notification module (FRD 3.14): the
// account and licence e-mails (FR-NOT-04) and, from Release 10.1, the e-mails
// of a garage at each hand-off (FR-NOT-05) and the low-stock alert (FR-INV-04).
// Every e-mail is triggered by an event another module publishes, except the
// quote's approval link, which exists only when the quote is sent.
type notificationModule struct {
	uc     *notificationUsecase.NotificationUseCase
	server *notificationHandler.Server
	// webhookSecret is the credential in the path of the provider's webhook;
	// empty switches the webhook off.
	webhookSecret string
}

func buildNotification(
	pool *platformdb.Pool,
	tx *platformdb.TxManager,
	mailer mail.Sender,
	users *identityRepo.PostgresUserRepository,
	customers *customerRepository.PostgresCustomerRepository,
	vehicles *customerRepository.PostgresVehicleRepository,
	customerUC *customerUsecase.CustomerUseCase,
	garages *tenantRepo.PostgresGarageRepository,
	documents *documentUsecase.DocumentUseCase,
	signingKey []byte,
) *notificationModule {
	transactor := garageTransactor{tx: tx}
	log := notificationRepository.NewPostgresRepository(pool).WithFailureTx(transactor)

	uc := notificationUsecase.NewNotificationUseCase(notificationUsecase.Deps{
		Mailer: mailer, Users: notificationUsers{users: users}, Log: log,
		Settings: notificationRepository.NewPostgresSettings(pool), Suppressions: notificationRepository.NewPostgresSuppressions(pool),
		Customers: notificationCustomers{
			customers: customers, vehicles: vehicles, links: customerRepository.NewPostgresLinkRepository(pool), users: users, uc: customerUC,
		},
		Garages: notificationGarages{garages: garages}, Staff: notificationRepository.NewPostgresStaff(pool),
		Receipts: notificationReceipts{documents: documents}, Tx: transactor,
		OptOutKey: deriveKey(signingKey, "vietgara/email-opt-out/v1"), BaseURL: appBaseURL(),
	})

	return &notificationModule{
		uc: uc, server: notificationHandler.NewServer(uc), webhookSecret: envOr("MAILJET_WEBHOOK_SECRET", ""),
	}
}

// deriveKey gives each purpose its own key from the signing key.
func deriveKey(master []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(purpose))

	return mac.Sum(nil)
}

// garageTransactor runs a function in a transaction scoped to a garage.
type garageTransactor struct {
	tx *platformdb.TxManager
}

func (g garageTransactor) InGarage(ctx context.Context, garageID string, fn func(ctx context.Context) error) error {
	return g.tx.WithTransaction(ctx, platformdb.GarageScope(garageID), fn)
}

// notificationCustomers lets Notification address a customer: the e-mail, the
// language (the linked user's, else the customer's own) and the opt-out.
type notificationCustomers struct {
	customers *customerRepository.PostgresCustomerRepository
	vehicles  *customerRepository.PostgresVehicleRepository
	links     *customerRepository.PostgresLinkRepository
	users     *identityRepo.PostgresUserRepository
	uc        *customerUsecase.CustomerUseCase
}

func (a notificationCustomers) Get(ctx context.Context, garageID, customerID string) (notificationUsecase.CustomerInfo, error) {
	customer, err := a.customers.FindByID(ctx, garageID, customerID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrCustomerNotFound) {
			return notificationUsecase.CustomerInfo{}, notificationUsecase.ErrCustomerNotFound
		}

		return notificationUsecase.CustomerInfo{}, err
	}

	info := notificationUsecase.CustomerInfo{
		ID: customer.ID, Name: customer.FullName, Locale: customer.Locale, OptedOut: customer.EmailOptOutAt != nil,
	}
	if customer.Email != nil {
		info.Email = *customer.Email
	}

	link, err := a.links.FindCurrent(ctx, garageID, customerID)
	if err != nil && !errors.Is(err, customerDomain.ErrLinkNotFound) {
		return notificationUsecase.CustomerInfo{}, err
	}

	if err == nil && link.Status == customerDomain.LinkLinked {
		if user, err := a.users.FindByID(ctx, link.UserID); err == nil {
			info.Locale = string(user.Locale.OrDefault())
		}
	}

	return info, nil
}

func (a notificationCustomers) ByVehicle(ctx context.Context, garageID, vehicleID string) (notificationUsecase.CustomerInfo, error) {
	vehicle, err := a.vehicles.FindByID(ctx, garageID, vehicleID)
	if err != nil {
		if errors.Is(err, customerDomain.ErrVehicleNotFound) {
			return notificationUsecase.CustomerInfo{}, notificationUsecase.ErrCustomerNotFound
		}

		return notificationUsecase.CustomerInfo{}, err
	}

	return a.Get(ctx, garageID, vehicle.CustomerID)
}

func (a notificationCustomers) OptOut(ctx context.Context, garageID, customerID string) error {
	err := a.uc.OptOutEmail(ctx, garageID, customerID)
	if appErr, ok := apperror.As(err); ok && appErr.Code == apperror.CodeNotFound {
		return notificationUsecase.ErrCustomerNotFound
	}

	return err
}

// notificationGarages gives an e-mail the garage's name and contact details.
type notificationGarages struct {
	garages *tenantRepo.PostgresGarageRepository
}

func (a notificationGarages) Get(ctx context.Context, garageID string) (notificationUsecase.GarageInfo, error) {
	garage, err := a.garages.FindByID(ctx, garageID)
	if err != nil {
		if errors.Is(err, tenantDomain.ErrGarageNotFound) {
			return notificationUsecase.GarageInfo{}, notificationUsecase.ErrGarageNotFound
		}

		return notificationUsecase.GarageInfo{}, err
	}

	info := notificationUsecase.GarageInfo{Name: garage.Name}
	if garage.Email != nil {
		info.Email = *garage.Email
	}

	if garage.Phone != nil {
		info.Phone = *garage.Phone
	}

	if garage.Address != nil {
		info.Address = *garage.Address
	}

	return info, nil
}

// notificationReceipts renders the receipt PDF of a settlement as an export
// (no print-log row), in the garage's paper size.
type notificationReceipts struct {
	documents *documentUsecase.DocumentUseCase
}

func (a notificationReceipts) SettlementReceipt(ctx context.Context, garageID, settlementID string) ([]byte, error) {
	return a.documents.SettlementReceiptPDF(ctx, garageID, settlementID, false)
}

// quoteNotifier gives Quote the call that e-mails an approval link. It is set
// once Notification is built, since Quote is built first.
type quoteNotifier struct {
	uc *notificationUsecase.NotificationUseCase
}

func (n *quoteNotifier) QuoteReady(ctx context.Context, quote quoteUsecase.QuoteReady) bool {
	return n.uc.QuoteReady(ctx, notificationUsecase.QuoteReadyRequest{
		GarageID: quote.GarageID, QuoteID: quote.QuoteID, CustomerID: quote.CustomerID, Number: quote.Number,
		VehiclePlate: quote.VehiclePlate, Total: quote.Total, ApproveURL: quote.ApproveURL,
	})
}
