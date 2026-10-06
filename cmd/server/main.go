// Command server boots the VietGara backend as a single Modular Monolith
// binary (ADR-001, Phase 1): every module runs in one process, sharing one
// gRPC server, but each module's internal package structure already
// respects service boundaries so modules can be extracted into standalone
// services later without a rewrite.
//
// The browser-facing REST/JSON API (API Specification) is served by
// grpc-gateway from the same gRPC services, through an in-memory
// connection, so every REST call passes the same interceptors.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"

	billingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/billing/v1"
	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"
	customerportalv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customerportal/v1"
	documentv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/document/v1"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	inventoryv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/inventory/v1"
	licensev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/license/v1"
	quotev1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/quote/v1"
	repairorderv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/repairorder/v1"
	reportingv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/reporting/v1"
	technicianv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/technician/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"github.com/viettechno/vietgara-proto/openapi"

	appdb "github.com/viettechno/vietgara-backend/db"
	billingHandler "github.com/viettechno/vietgara-backend/internal/billing/handler"
	customerHandler "github.com/viettechno/vietgara-backend/internal/customer/handler"
	customerRepository "github.com/viettechno/vietgara-backend/internal/customer/repository"
	customerUsecase "github.com/viettechno/vietgara-backend/internal/customer/usecase"
	customerportalHandler "github.com/viettechno/vietgara-backend/internal/customerportal/handler"
	documentHandler "github.com/viettechno/vietgara-backend/internal/document/handler"
	identityHandler "github.com/viettechno/vietgara-backend/internal/identity/handler"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	inventoryHandler "github.com/viettechno/vietgara-backend/internal/inventory/handler"
	licenseHandler "github.com/viettechno/vietgara-backend/internal/license/handler"
	licenseUsecase "github.com/viettechno/vietgara-backend/internal/license/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	notificationHandler "github.com/viettechno/vietgara-backend/internal/notification/handler"
	"github.com/viettechno/vietgara-backend/internal/platform/apidocs"
	"github.com/viettechno/vietgara-backend/internal/platform/audit"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/googleid"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/internal/platform/kafka"
	"github.com/viettechno/vietgara-backend/internal/platform/mail"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	"github.com/viettechno/vietgara-backend/internal/platform/tracing"
	quoteHandler "github.com/viettechno/vietgara-backend/internal/quote/handler"
	repairorderHandler "github.com/viettechno/vietgara-backend/internal/repairorder/handler"
	reportingHandler "github.com/viettechno/vietgara-backend/internal/reporting/handler"
	technicianHandler "github.com/viettechno/vietgara-backend/internal/technician/handler"
	tenantHandler "github.com/viettechno/vietgara-backend/internal/tenant/handler"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
	tenantUsecase "github.com/viettechno/vietgara-backend/internal/tenant/usecase"
)

const defaultMigrationsPath = "file://db/migrations"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	if err := run(ctx); err != nil {
		stop()
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}

	stop()
}

func run(ctx context.Context) error {
	dsn := mustEnv("DATABASE_URL")

	if err := runMigrations(envOr("MIGRATION_DATABASE_URL", dsn)); err != nil {
		return err
	}

	connPool, err := platformdb.NewPgxPool(ctx, dsn)
	if err != nil {
		return err
	}

	pool := platformdb.NewPool(ctx, connPool)
	defer pool.Close()

	app, err := build(ctx, pool)
	if err != nil {
		return err
	}
	defer app.close()

	for _, worker := range app.workers {
		go worker(ctx)
	}

	return serve(ctx, app)
}

func runMigrations(dsn string) error {
	if !envBool("RUN_MIGRATIONS", true) {
		return nil
	}

	if err := appdb.RunMigrations(dsn, envOr("MIGRATIONS_PATH", defaultMigrationsPath)); err != nil {
		return err
	}

	// Local/CI convenience: create the non-superuser login the backend
	// connects as (staging/production users come from Terraform).
	if user := envOr("APP_DB_USER", ""); user != "" {
		return appdb.EnsureAppLogin(dsn, user, mustEnv("APP_DB_PASSWORD"))
	}

	return nil
}

// worker is a background loop that runs until ctx is cancelled.
type worker func(ctx context.Context)

type application struct {
	grpcServer *grpc.Server
	httpServer *http.Server
	workers    []worker
	closers    []func()
}

func (a *application) close() {
	for _, c := range a.closers {
		c()
	}
}

//nolint:funlen // the composition root wires every module in one place
func build(ctx context.Context, pool *platformdb.Pool) (*application, error) {
	app := &application{}

	// Distributed tracing (NFR-OB-01): without OTEL_EXPORTER_OTLP_ENDPOINT,
	// spans are still created (every request still carries a trace id to
	// log) but are not exported anywhere.
	shutdownTracing, err := tracing.Init(ctx, "vietgara-backend", envOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""))
	if err != nil {
		return nil, err
	}

	app.closers = append(app.closers, func() { _ = shutdownTracing(context.Background()) })

	tx := platformdb.NewTxManager(pool)
	issuer := auth.NewTokenIssuer([]byte(mustEnv("JWT_SIGNING_KEY")),
		envDuration("JWT_ACCESS_TTL", 20*time.Minute), envDuration("JWT_REFRESH_TTL", 30*24*time.Hour))
	events := outbox.NewEventPublisher(pool)
	auditLog := audit.NewLogger(pool)

	redisClient, err := newRedis(ctx)
	if err != nil {
		return nil, err
	}

	if redisClient != nil {
		app.closers = append(app.closers, func() { _ = redisClient.Close() })
	}

	mailer, err := mail.New(mail.Config{
		Provider:         envOr("MAIL_PROVIDER", "smtp"),
		From:             mail.From{Email: envOr("MAIL_FROM_ADDRESS", "support@viettechno.com"), Name: envOr("MAIL_FROM_NAME", "VietGara")},
		SMTPHost:         envOr("SMTP_HOST", "localhost"),
		SMTPPort:         envInt("SMTP_PORT", 1025),
		SMTPUsername:     envOr("SMTP_USERNAME", ""),
		SMTPPassword:     envOr("SMTP_PASSWORD", ""),
		MailjetAPIKey:    envOr("MAILJET_API_KEY", ""),
		MailjetSecretKey: envOr("MAILJET_SECRET_KEY", ""),
	})
	if err != nil {
		return nil, err
	}

	files, err := storage.New(ctx, storage.Config{
		Backend:       envOr("STORAGE_BACKEND", "local"),
		LocalDir:      envOr("STORAGE_LOCAL_DIR", "./.data/uploads"),
		PublicBaseURL: envOr("STORAGE_PUBLIC_BASE_URL", ""),
		Bucket:        envOr("STORAGE_GCS_BUCKET", ""),
	})
	if err != nil {
		return nil, err
	}

	// Identity & Access
	accounts := identityRepo.NewPostgresAccountRepository(pool)

	var sessions interface {
		identityUsecase.SessionStore
		identityUsecase.OTPStore
	} = identityRepo.NewMemoryStore()

	if redisClient != nil {
		sessions = identityRepo.NewRedisStore(redisClient)
	} else {
		slog.Warn("REDIS_ADDR is not set: sessions, OTPs and the entitlement cache live in memory (single instance only)")
	}

	identityUC := identityUsecase.NewIdentityUseCase(accounts, sessions, sessions, mailer, events, tx, issuer)
	if err := identityUC.GrantPlatformAdmins(ctx, envList("PLATFORM_ADMIN_EMAILS")); err != nil {
		return nil, err
	}

	identitySrv := identityHandler.NewServer(identityUC, int64(issuer.AccessTTL().Seconds()), identityHandler.GoogleOAuthConfig{
		ClientID:      envOr("GOOGLE_CLIENT_ID", ""),
		ClientSecret:  envOr("GOOGLE_CLIENT_SECRET", ""),
		AuthEndpoint:  envOr("GOOGLE_AUTH_ENDPOINT", "https://accounts.google.com/o/oauth2/v2/auth"),
		TokenEndpoint: envOr("GOOGLE_TOKEN_ENDPOINT", "https://oauth2.googleapis.com/token"),
		UserInfoURL:   envOr("GOOGLE_USERINFO_URL", "https://openidconnect.googleapis.com/v1/userinfo"),
		AppBaseURL:    appBaseURL(),
	}).WithGoogleIDTokenVerifier(googleid.NewVerifier(envList("GOOGLE_MOBILE_CLIENT_IDS"),
		googleid.WithCertsURL(envOr("GOOGLE_CERTS_URL", googleid.DefaultCertsURL))))

	// Tenant/Garage Management
	garages := tenantRepo.NewPostgresGarageRepository(pool)
	accessReader := tenantRepo.NewPostgresAccessReader(pool)
	license := buildLicense(pool, tx, redisClient, events, auditLog, accessReader, garages)

	tenantUC := tenantUsecase.NewTenantUseCase(tenantUsecase.Deps{
		Garages:     garages,
		Staff:       tenantRepo.NewPostgresStaffRepository(pool),
		Roles:       tenantRepo.NewPostgresRoleRepository(pool),
		Groups:      tenantRepo.NewPostgresGroupRepository(pool),
		Invitations: tenantRepo.NewPostgresInvitationRepository(pool),
		Access:      accessReader,
		Usage:       accessReader,
		Limits:      limitChecker{entitlements: license.entitlements},
		Accounts:    accountDirectory{accounts: accounts},
		Logos:       files,
		Mailer:      mailer,
		Events:      events,
		Audit:       auditLog,
		Tx:          tx,
		AppBaseURL:  appBaseURL(),
	})
	tenantSrv := tenantHandler.NewServer(tenantUC)

	// Customer & Vehicle
	customerRepo := customerRepository.NewPostgresCustomerRepository(pool)
	vehicleRepo := customerRepository.NewPostgresVehicleRepository(pool)

	// Repair Order (Release 1.3): the Kanban board, plus the
	// draft-creation-from-quote use case Quote depends on (FR-QUO-04).
	repairOrder := buildRepairOrder(pool, events, garages, license.entitlements, repairOrderVehicles{vehicles: vehicleRepo})

	// Quote & Pricing (Release 1.2)
	quote := buildQuote(pool, tx, events, mailer, garages, license.entitlements, customerRepo, vehicleRepo, repairOrder.uc)

	// Inventory (Release 1.3): items, stock receipts and the
	// repair_order.closed consumer that deducts stock (FR-INV-02).
	inventory := buildInventory(pool, events)

	partnerRepo := customerRepository.NewPostgresPartnerRepository(pool)

	vehicleUC := customerUsecase.NewVehicleUseCase(vehicleRepo, customerRepo, vehicleHistory{quotes: quote.quotes, repairOrders: repairOrder.uc})
	customerSrv := customerHandler.NewServer(
		customerUsecase.NewCustomerUseCase(customerRepo, vehicleRepo),
		vehicleUC,
		customerUsecase.NewPartnerUseCase(partnerRepo),
	)

	// Billing & Settlement (Release 1.4): settlements from a closed
	// repair order, the customer/insurer allocation, recorded payments.
	billing := buildBilling(pool, events, garages, license.entitlements, partnerRepo, repairOrder.uc)

	// Document/Print (Release 1.4): quote, repair order and settlement
	// receipt PDFs, the stock movement report as Excel.
	document := buildDocument(quote.quotes, repairOrder.uc, billing.uc, inventory.uc, garages)

	// Reporting (Release 1.5): revenue by period (FR-RPT-01), reusing
	// Billing & Settlement's own repository -- no tables of its own.
	reporting := buildReporting(billing.settlements)

	// Notification (Release 1.5): the e-mail channel only (FR-NOT-04) --
	// registration confirmation and license renewal reminders. No RPCs of
	// its own; every e-mail is triggered by an event consumer (wireEvents).
	notification := buildNotification(pool, mailer, accounts)

	// Mobile BFFs (Phase 2): the Technician App's work list (Release 2.2)
	// and the Customer App's vehicles and history (Release 2.4).
	technicianSrv := buildTechnician(tx, tenantUC, repairOrder.uc)
	customerPortalSrv := buildCustomerPortal(tx, identityUC, customerRepo, vehicleUC, garages)

	// Events: Kafka when brokers are configured, otherwise in-process.
	if err := wireEvents(app, pool, tx, tenantUC, license, inventory, notification); err != nil {
		return nil, err
	}

	app.workers = append(app.workers, func(ctx context.Context) {
		license.expiryJob.Schedule(ctx, envDuration("LICENSE_EXPIRY_JOB_INTERVAL", licenseUsecase.DefaultExpiryJobInterval))
	})

	// gRPC server with the request pipeline of internal/middleware.
	policies := middleware.Merge(
		identityHandler.Policies(), tenantHandler.Policies(), licenseHandler.Policies(), customerHandler.Policies(),
		quoteHandler.Policies(), repairorderHandler.Policies(), inventoryHandler.Policies(), billingHandler.Policies(),
		documentHandler.Policies(), reportingHandler.Policies(),
		technicianHandler.Policies(), customerportalHandler.Policies(),
	)
	app.grpcServer = grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()), // one span per request (NFR-OB-01)
		grpc.ChainUnaryInterceptor(
			middleware.Auth(issuer, policies),
			middleware.EmailVerified(),
			middleware.PlatformAdmin(),
			middleware.Garage(tx, accessReader, license.enforcement),
		),
	)

	identityv1.RegisterAuthServiceServer(app.grpcServer, identitySrv)
	identityv1.RegisterAccountServiceServer(app.grpcServer, identitySrv)
	tenantv1.RegisterGarageServiceServer(app.grpcServer, tenantSrv)
	tenantv1.RegisterStaffServiceServer(app.grpcServer, tenantSrv)
	tenantv1.RegisterInvitationServiceServer(app.grpcServer, tenantSrv)
	tenantv1.RegisterRoleServiceServer(app.grpcServer, tenantSrv)
	tenantv1.RegisterStaffGroupServiceServer(app.grpcServer, tenantSrv)
	licensev1.RegisterLicenseServiceServer(app.grpcServer, license.server)
	licensev1.RegisterLicenseAdminServiceServer(app.grpcServer, license.server)
	customerv1.RegisterCustomerServiceServer(app.grpcServer, customerSrv)
	customerv1.RegisterVehicleServiceServer(app.grpcServer, customerSrv)
	customerv1.RegisterPartnerServiceServer(app.grpcServer, customerSrv)
	quotev1.RegisterQuoteServiceServer(app.grpcServer, quote.server)
	quotev1.RegisterPublicQuoteServiceServer(app.grpcServer, quote.server)
	repairorderv1.RegisterRepairOrderServiceServer(app.grpcServer, repairOrder.server)
	inventoryv1.RegisterInventoryServiceServer(app.grpcServer, inventory.server)
	billingv1.RegisterSettlementServiceServer(app.grpcServer, billing.server)
	documentv1.RegisterDocumentServiceServer(app.grpcServer, document.server)
	reportingv1.RegisterReportingServiceServer(app.grpcServer, reporting.server)
	technicianv1.RegisterTechnicianServiceServer(app.grpcServer, technicianSrv)
	customerportalv1.RegisterCustomerPortalServiceServer(app.grpcServer, customerPortalSrv)
	reflection.Register(app.grpcServer)

	mux, err := buildGateway(ctx, app, identitySrv, files)
	if err != nil {
		return nil, err
	}

	app.httpServer = &http.Server{Handler: httpapi.NoContentForDelete(mux), ReadHeaderTimeout: 5 * time.Second}

	return app, nil
}

// wireEvents connects the outbox relay to Kafka (or the in-process
// dispatcher) and subscribes the module consumers.
func wireEvents(
	app *application, pool *platformdb.Pool, tx *platformdb.TxManager, tenantUC *tenantUsecase.TenantUseCase,
	license *licenseModule, inventory *inventoryModule, notification *notificationModule,
) error {
	processor := outbox.NewProcessor(pool, tx)
	relayOpts := []outbox.RelayOption{outbox.WithInterval(envDuration("OUTBOX_RELAY_INTERVAL", outbox.DefaultRelayInterval))}

	brokers := envList("KAFKA_BROKERS")
	if len(brokers) == 0 {
		slog.Warn("KAFKA_BROKERS is not set: events are delivered in-process")

		dispatcher := outbox.NewDispatcher()
		tenantHandler.Subscribe(dispatcher, processor, tenantUC)
		licenseHandler.Subscribe(dispatcher, processor, license.subscriptions)
		inventoryHandler.Subscribe(dispatcher, processor, inventory.uc)
		notificationHandler.Subscribe(dispatcher, processor, notification.uc)
		app.workers = append(app.workers, outbox.NewRelay(pool, tx, dispatcher, relayOpts...).Run)

		return nil
	}

	prefix := envOr("KAFKA_TOPIC_PREFIX", kafka.DefaultTopicPrefix)

	producer, err := kafka.NewProducer(brokers, prefix)
	if err != nil {
		return err
	}

	app.closers = append(app.closers, producer.Close)

	consumer := kafka.NewConsumer(brokers, envOr("KAFKA_CONSUMER_GROUP", "vietgara-backend"), prefix)
	tenantHandler.Subscribe(consumer, processor, tenantUC)
	licenseHandler.Subscribe(consumer, processor, license.subscriptions)
	inventoryHandler.Subscribe(consumer, processor, inventory.uc)
	notificationHandler.Subscribe(consumer, processor, notification.uc)

	app.workers = append(app.workers,
		outbox.NewRelay(pool, tx, producer, relayOpts...).Run,
		func(ctx context.Context) {
			if err := consumer.Run(ctx); err != nil {
				slog.Error("kafka consumer stopped", "error", err)
			}
		},
	)

	return nil
}

// buildGateway serves the REST API from the gRPC server over an in-memory
// connection, plus the plain HTTP routes (Google OAuth, local files, API
// docs).
func buildGateway(ctx context.Context, app *application, identitySrv *identityHandler.Server, files storage.Store) (*runtime.ServeMux, error) {
	listener := bufconn.Listen(1 << 20)

	go func() {
		if err := app.grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			slog.Error("in-memory gRPC listener stopped", "error", err)
		}
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("gateway connection: %w", err)
	}

	app.closers = append(app.closers, func() { _ = conn.Close() })

	mux := httpapi.NewMux()

	for _, register := range []func(context.Context, *runtime.ServeMux, *grpc.ClientConn) error{
		identityv1.RegisterAuthServiceHandler, identityv1.RegisterAccountServiceHandler,
		tenantv1.RegisterGarageServiceHandler, tenantv1.RegisterStaffServiceHandler,
		tenantv1.RegisterInvitationServiceHandler, tenantv1.RegisterRoleServiceHandler,
		tenantv1.RegisterStaffGroupServiceHandler,
		licensev1.RegisterLicenseServiceHandler, licensev1.RegisterLicenseAdminServiceHandler,
		customerv1.RegisterCustomerServiceHandler, customerv1.RegisterVehicleServiceHandler,
		customerv1.RegisterPartnerServiceHandler,
		quotev1.RegisterQuoteServiceHandler, quotev1.RegisterPublicQuoteServiceHandler,
		repairorderv1.RegisterRepairOrderServiceHandler, inventoryv1.RegisterInventoryServiceHandler,
		billingv1.RegisterSettlementServiceHandler, documentv1.RegisterDocumentServiceHandler,
		reportingv1.RegisterReportingServiceHandler,
		technicianv1.RegisterTechnicianServiceHandler, customerportalv1.RegisterCustomerPortalServiceHandler,
	} {
		if err := register(ctx, mux, conn); err != nil {
			return nil, err
		}
	}

	if err := identitySrv.RegisterHTTPRoutes(mux); err != nil {
		return nil, err
	}

	if envBool("API_DOCS_ENABLED", false) {
		if err := apidocs.Register(mux, openapi.Spec); err != nil {
			return nil, err
		}
	}

	if local, ok := files.(*storage.LocalStore); ok {
		serveFiles := local.Handler()

		if err := mux.HandlePath(http.MethodGet, storage.DefaultLocalBaseURL+"/{path=**}", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
			serveFiles.ServeHTTP(w, r)
		}); err != nil {
			return nil, err
		}
	}

	if err := mux.HandlePath(http.MethodGet, "/healthz", func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
		httpapi.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}); err != nil {
		return nil, err
	}

	return mux, nil
}

func newRedis(ctx context.Context) (redis.UniversalClient, error) {
	addr := envOr("REDIS_ADDR", "")
	if addr == "" {
		return nil, nil //nolint:nilnil // no Redis configured: in-memory stores
	}

	client := redis.NewClient(&redis.Options{Addr: addr, Password: envOr("REDIS_PASSWORD", "")})
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("connect to redis at %s: %w", addr, err)
	}

	return client, nil
}

func serve(ctx context.Context, app *application) error {
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+envOr("GRPC_PORT", "50051"))
	if err != nil {
		return err
	}

	app.httpServer.Addr = ":" + envOr("HTTP_PORT", "8080")

	go func() {
		if err := app.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server exited", "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		slog.Info("shutting down")

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		if err := app.httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("HTTP server shutdown failed", "error", err)
		}

		app.grpcServer.GracefulStop()
	}()

	slog.Info("VietGara backend listening", "grpc", lis.Addr().String(), "http", app.httpServer.Addr)

	return app.grpcServer.Serve(lis)
}
