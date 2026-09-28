package main

import (
	"github.com/redis/go-redis/v9"

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

	return &licenseModule{
		server:        licenseHandler.NewServer(subscriptions, entitlements),
		enforcement:   licenseHandler.NewEnforcement(entitlements),
		subscriptions: subscriptions,
		entitlements:  entitlements,
		expiryJob:     licenseUsecase.NewExpiryJob(subscriptionRepo, subscriptionRepo, events, cache, tx),
	}
}
