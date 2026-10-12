package main

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"

	fileDomain "github.com/viettechno/vietgara-backend/internal/file/domain"
	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	licenseDomain "github.com/viettechno/vietgara-backend/internal/license/domain"
	licenseHandler "github.com/viettechno/vietgara-backend/internal/license/handler"
	licenseRepo "github.com/viettechno/vietgara-backend/internal/license/repository"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// licenseModule is the wired License Management module.
type licenseModule struct {
	server        *licenseHandler.Server
	enforcement   *licenseHandler.Enforcement
	subscriptions *licenseUsecase.SubscriptionUseCase
	entitlements  *licenseUsecase.EntitlementUseCase
	payments      *licenseUsecase.PaymentUseCase
	expiryJob     *licenseUsecase.ExpiryJob
}

func buildLicense(
	pool *platformdb.Pool,
	tx *platformdb.TxManager,
	redisClient redis.UniversalClient,
	events *outbox.EventPublisher,
	auditLog *audit.Logger,
	accessReader *tenantRepo.PostgresAccessReader,
	garages *tenantRepo.PostgresGarageRepository,
	files *fileUsecase.UseCase,
) *licenseModule {
	ttl := envDuration("ENTITLEMENT_CACHE_TTL", licenseRepo.DefaultCacheTTL)

	var (
		cache   licenseUsecase.EntitlementCache
		counter licenseUsecase.TransactionCounter
	)

	if redisClient != nil {
		cache, counter = licenseRepo.NewRedisEntitlementCache(redisClient, ttl), licenseRepo.NewRedisTransactionCounter(redisClient)
	} else {
		cache, counter = licenseRepo.NewMemoryEntitlementCache(ttl), licenseRepo.NewMemoryTransactionCounter()
	}

	plans := licenseRepo.NewPostgresPlanRepository(pool)
	subscriptionRepo := licenseRepo.NewPostgresRepository(pool)

	subscriptions := licenseUsecase.NewSubscriptionUseCase(licenseUsecase.SubscriptionDeps{
		Plans: plans, Subscriptions: subscriptionRepo, Entitlements: subscriptionRepo, Usage: accessReader,
		Counter: counter, Events: events, Cache: cache, Audit: auditLog,
	})
	entitlements := licenseUsecase.NewEntitlementUseCase(subscriptionRepo, cache, garageOwners{garages: garages}, counter, subscriptions)
	payments := licenseUsecase.NewPaymentUseCase(licenseUsecase.PaymentDeps{
		Plans: plans, Subscriptions: subscriptionRepo, Entitlements: subscriptionRepo, Payments: subscriptionRepo,
		Files: paymentFiles{files: files}, Usage: accessReader, Events: events, Cache: cache, Audit: auditLog,
	})

	return &licenseModule{
		server:        licenseHandler.NewServer(subscriptions, entitlements, payments),
		enforcement:   licenseHandler.NewEnforcement(entitlements),
		subscriptions: subscriptions,
		entitlements:  entitlements,
		payments:      payments,
		expiryJob:     licenseUsecase.NewExpiryJob(subscriptionRepo, subscriptionRepo, events, cache, subscriptionRepo, tx),
	}
}

// paymentFiles keeps the private files of subscription payments in the file
// registry without the License module importing the File module: the
// evidence of a transfer belongs to the owner, the platform's QR code to
// the admin who uploaded it.
type paymentFiles struct {
	files *fileUsecase.UseCase
}

func (p paymentFiles) StoreEvidence(ctx context.Context, userID string, content []byte, contentType string) (string, error) {
	f, err := p.files.StoreUserFile(ctx, userID, fileDomain.KindPaymentEvidence, content, contentType)
	if errors.Is(err, fileDomain.ErrInvalidFile) {
		return "", licenseDomain.ErrInvalidEvidence
	}

	if err != nil {
		return "", err
	}

	return f.ID, nil
}

func (p paymentFiles) StoreQR(ctx context.Context, adminID string, content []byte, contentType string) (string, error) {
	f, err := p.files.StoreUserFile(ctx, adminID, fileDomain.KindPlatformQR, content, contentType)
	if errors.Is(err, fileDomain.ErrInvalidFile) {
		return "", licenseDomain.ErrInvalidQR
	}

	if err != nil {
		return "", err
	}

	return f.ID, nil
}

func (p paymentFiles) SignedURL(ctx context.Context, fileID string) (string, error) {
	return p.files.SignedURLByID(ctx, fileID)
}
