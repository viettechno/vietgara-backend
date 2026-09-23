// Package handler wires the Customer & Vehicle use cases to the gRPC
// services generated from vietgara-proto (vietgara/customer/v1) and to the
// browser-facing HTTP routes, which reuse the gRPC methods.
package handler

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/internal/customer/domain"
	"github.com/viettechno/vietgara-backend/internal/customer/usecase"
	"github.com/viettechno/vietgara-backend/internal/middleware"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Staff roles (see tenant domain.Role) that matter to this module.
const (
	roleOwner          = "owner"
	roleServiceAdvisor = "service_advisor"
)

// permission is an action class checked against the caller's garage role
// (Security Architecture Section 3.1).
type permission int

const (
	// permRead lets any active staff member of the garage read.
	permRead permission = iota
	// permManageCustomers covers customer and vehicle changes (FR-CUS-01/02).
	permManageCustomers
	// permManagePartners covers the partner directory (FR-CUS-04).
	permManagePartners
)

var rolesAllowed = map[permission][]string{
	permManageCustomers: {roleOwner, roleServiceAdvisor},
	permManagePartners:  {roleOwner},
}

// MembershipReader is a narrow read-only view of the Tenant module's staff
// memberships, implemented by an adapter in cmd/server/wiring.go so the two
// modules stay decoupled.
type MembershipReader interface {
	// RoleInGarage returns the account's role in an active membership of
	// the garage, or domain.ErrNoAccessToGarage.
	RoleInGarage(ctx context.Context, garageID, accountID string) (string, error)
}

// ServerOption customizes a Server.
type ServerOption func(*Server)

// WithTxManager lets the HTTP endpoints open a garage-scoped transaction
// (setting app.current_garage_id for Row-Level Security) the way
// middleware.TransactionInterceptor does for gRPC requests.
func WithTxManager(txManager *platformdb.TxManager) ServerOption {
	return func(s *Server) {
		s.txManager = txManager
	}
}

// Server implements customerv1.CustomerServiceServer,
// customerv1.VehicleServiceServer and customerv1.PartnerServiceServer.
type Server struct {
	customerv1.UnimplementedCustomerServiceServer
	customerv1.UnimplementedVehicleServiceServer
	customerv1.UnimplementedPartnerServiceServer

	customers *usecase.CustomerUseCase
	vehicles  *usecase.VehicleUseCase
	partners  *usecase.PartnerUseCase
	members   MembershipReader

	txManager *platformdb.TxManager
}

// NewServer wires the handler to its use cases and the membership reader
// used for authorization.
func NewServer(
	customers *usecase.CustomerUseCase,
	vehicles *usecase.VehicleUseCase,
	partners *usecase.PartnerUseCase,
	members MembershipReader,
	opts ...ServerOption,
) *Server {
	s := &Server{customers: customers, vehicles: vehicles, partners: partners, members: members}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// authorize checks the caller is an active staff member of garageID whose
// role grants perm, and returns the caller's account id. Membership is read
// from the database rather than the token so a deactivated member loses
// access at once and a garage created moments ago works before the token
// is refreshed.
func (s *Server) authorize(ctx context.Context, garageID string, perm permission) (string, error) {
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing account context")
	}

	if uuid.Validate(garageID) != nil {
		return "", status.Error(codes.InvalidArgument, "garage_id is invalid")
	}

	role, err := s.members.RoleInGarage(ctx, garageID, claims.Subject)
	if err != nil {
		if errors.Is(err, domain.ErrNoAccessToGarage) {
			return "", status.Error(codes.PermissionDenied, domain.ErrNoAccessToGarage.Error())
		}

		return "", status.Error(codes.Internal, "internal error")
	}

	if perm != permRead && !slices.Contains(rolesAllowed[perm], role) {
		return "", status.Error(codes.PermissionDenied, domain.ErrPermissionDenied.Error())
	}

	return claims.Subject, nil
}

// run authorizes perm on garageID, then calls fn with the caller's account
// id and converts use case errors to gRPC status errors.
func run[T any](ctx context.Context, s *Server, garageID string, perm permission, fn func(actorID string) (T, error)) (T, error) {
	var zero T

	actorID, err := s.authorize(ctx, garageID, perm)
	if err != nil {
		return zero, err
	}

	resp, err := fn(actorID)
	if err != nil {
		return zero, apperror.ToGRPCError(err)
	}

	return resp, nil
}
