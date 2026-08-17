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
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc/reflection"

	tenantv1 "github.com/viettechno/vietgara-proto/gen/go/vietgara/tenant/v1"
	"google.golang.org/grpc"

	appdb "github.com/viettechno/vietgara-backend/db"
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

	connPool, err := platformdb.NewPgxPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer connPool.Close()

	pool := platformdb.NewPool(ctx, connPool)
	defer pool.Close()

	if envBool("RUN_MIGRATIONS", true) {
		migrationsPath := envOr("MIGRATIONS_PATH", defaultMigrationsPath)
		if err := appdb.RunMigrations(dsn, migrationsPath); err != nil {
			return err
		}
	}

	txManager := platformdb.NewTxManager(pool)
	tokenIssuer := auth.NewTokenIssuer(signingKey, 20*time.Minute, 30*24*time.Hour)

	// --- Repositories ---
	garageRepo := tenantRepo.NewPostgresGarageRepository(pool)
	staffRepo := tenantRepo.NewPostgresGarageStaffRepository(pool)

	// --- Usecases ---
	eventPublisher := outbox.NewEventPublisher(pool)
	garageUC := tenantUsecase.NewGarageUseCase(
		garageRepo,
		staffRepo,
		eventPublisher,
		tenantUsecase.WithTransactionManager(txManager), // CreateGarage runs inside one tenant-scoped transaction (RLS)
	)
	staffUC := tenantUsecase.NewStaffUseCase(staffRepo)

	// --- Handler ---
	tenantSrv := tenantHandler.NewServer(garageUC, staffUC)

	// --- gRPC server with the shared interceptor chain ---
	// NOTE: AuthInterceptor must be skipped for Register/Login/SocialLogin
	// themselves (no token exists yet) -- in the real .proto wiring this is
	// done via a per-method allowlist passed into a small wrapper around
	// AuthInterceptor; the Identity module is not wired yet.
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor(tokenIssuer),
			middleware.TenantContextInterceptor(),
			middleware.TransactionInterceptor(txManager),
		),
	)
	tenantv1.RegisterGarageServiceServer(grpcServer, tenantSrv)
	tenantv1.RegisterStaffServiceServer(grpcServer, tenantSrv)
	reflection.Register(grpcServer)

	lis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", ":"+grpcPort)
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()

		slog.Info("shutting down gRPC server")
		grpcServer.GracefulStop()
	}()

	slog.Info("VietGara backend (Modular Monolith, Phase 1) listening", "addr", lis.Addr().String())

	if err := grpcServer.Serve(lis); err != nil {
		return err
	}

	return nil
}
