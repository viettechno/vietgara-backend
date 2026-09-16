// Command server boots the VietGara backend as a single Modular Monolith
// binary (ADR-001, Phase 1): every module below runs in one process today,
// sharing one gRPC server, but each module's internal package structure
// already respects service boundaries so individual modules can be
// extracted into standalone services later without a rewrite.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc/reflection"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
	"google.golang.org/grpc"

	appdb "github.com/viettechno/vietgara-backend/db"
	identityHandler "github.com/viettechno/vietgara-backend/internal/identity/handler"
	identityRepo "github.com/viettechno/vietgara-backend/internal/identity/repository"
	identityUsecase "github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/outbox"
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
	signingKey := []byte(mustEnv("JWT_SIGNING_KEY"))
	grpcPort := envOr("GRPC_PORT", "50051")
	httpPort := envOr("HTTP_PORT", "8080")

	pool, err := initPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := runMigrations(dsn); err != nil {
		return err
	}

	grpcServer, httpServer, err := buildServices(pool, signingKey)
	if err != nil {
		return err
	}

	return serve(ctx, grpcPort, httpPort, grpcServer, httpServer)
}

func initPool(ctx context.Context, dsn string) (*platformdb.Pool, error) {
	connPool, err := platformdb.NewPgxPool(ctx, dsn)
	if err != nil {
		return nil, err
	}

	return platformdb.NewPool(ctx, connPool), nil
}

func runMigrations(dsn string) error {
	if !envBool("RUN_MIGRATIONS", true) {
		return nil
	}

	migrationsPath := envOr("MIGRATIONS_PATH", defaultMigrationsPath)

	return appdb.RunMigrations(dsn, migrationsPath)
}

func buildServices(pool *platformdb.Pool, signingKey []byte) (*grpc.Server, *http.Server, error) {
	txManager := platformdb.NewTxManager(pool)
	accessTTL := envDuration("JWT_ACCESS_TTL", 20*time.Minute)
	refreshTTL := envDuration("JWT_REFRESH_TTL", 30*24*time.Hour)
	tokenIssuer := auth.NewTokenIssuer(signingKey, accessTTL, refreshTTL)

	garageRepo := tenantRepo.NewPostgresGarageRepository(pool)
	staffRepo := tenantRepo.NewPostgresGarageStaffRepository(pool)
	accountRepo := identityRepo.NewPostgresAccountRepository(pool)

	eventPublisher := outbox.NewEventPublisher(pool)
	garageUC := tenantUsecase.NewGarageUseCase(
		garageRepo,
		staffRepo,
		eventPublisher,
		tenantUsecase.WithTransactionManager(txManager),
	)
	staffUC := tenantUsecase.NewStaffUseCase(staffRepo)

	refreshStore := newInMemoryRefreshTokenStore()
	garageReader := &tenantToIdentityGarageReaderAdapter{staffRepo: staffRepo}
	identityUC := identityUsecase.NewIdentityUseCase(
		accountRepo,
		garageReader,
		refreshStore,
		&stubOTPVerifier{},
		tokenIssuer,
		refreshTTL,
	)

	tenantSrv := tenantHandler.NewServer(garageUC, staffUC)
	identitySrv := identityHandler.NewServer(identityUC, &stubSocialIdentityVerifier{})
	identitySrv.ConfigureGoogleOAuth(identityHandler.GoogleOAuthConfig{
		ClientID:       envOr("GOOGLE_CLIENT_ID", ""),
		ClientSecret:   envOr("GOOGLE_CLIENT_SECRET", ""),
		AuthEndpoint:   envOr("GOOGLE_AUTH_ENDPOINT", "https://accounts.google.com/o/oauth2/v2/auth"),
		TokenEndpoint:  envOr("GOOGLE_TOKEN_ENDPOINT", "https://oauth2.googleapis.com/token"),
		UserInfoURL:    envOr("GOOGLE_USERINFO_URL", "https://openidconnect.googleapis.com/v1/userinfo"),
		RedirectURI:    envOr("GOOGLE_REDIRECT_URI", "http://localhost:8000/api/auth/google/callback"),
		FrontendOrigin: envOr("FRONTEND_ORIGIN", "http://localhost:8000"),
	})

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor(tokenIssuer,
				middleware.WithSkipMethods(
					identityv1.AuthService_Register_FullMethodName,
					identityv1.AuthService_Login_FullMethodName,
					identityv1.AuthService_RefreshToken_FullMethodName,
					identityv1.AuthService_Logout_FullMethodName,
				),
			),
			middleware.TenantContextInterceptor(),
			middleware.TransactionInterceptor(txManager),
		),
	)
	identityv1.RegisterAuthServiceServer(grpcServer, identitySrv)
	tenantv1.RegisterGarageServiceServer(grpcServer, tenantSrv)
	tenantv1.RegisterStaffServiceServer(grpcServer, tenantSrv)
	reflection.Register(grpcServer)

	httpMux := http.NewServeMux()
	httpMux.Handle("/auth/", identitySrv)
	httpMux.Handle("/staff", middleware.HTTPAuth(tokenIssuer, tenantSrv))

	return grpcServer, &http.Server{
		Addr:              ":" + envOr("HTTP_PORT", "8080"),
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
	}, nil
}

func serve(ctx context.Context, grpcPort, httpPort string, services *grpc.Server, httpServer *http.Server) error {
	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+grpcPort)
	if err != nil {
		return err
	}

	if httpServer != nil {
		httpServer.Addr = ":" + httpPort
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("HTTP server exited", "error", err)
			}
		}()
	}

	go func() {
		<-ctx.Done()

		slog.Info("shutting down gRPC server")
		services.GracefulStop()

		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		if httpServer != nil {
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				slog.Error("HTTP server shutdown failed", "error", err)
			}
		}
	}()

	slog.Info("VietGara backend (Modular Monolith, Phase 1) listening", "addr", lis.Addr().String())

	if err := services.Serve(lis); err != nil {
		return err
	}

	return nil
}
