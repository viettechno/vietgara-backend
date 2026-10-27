package middleware

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/viettechno/vietgara-backend/internal/platform/access"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/observability"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Reasons reported by the Garage interceptor.
const (
	ReasonGarageAccessDenied    = "GARAGE_ACCESS_DENIED"
	ReasonOwnerOnly             = "OWNER_ONLY"
	ReasonGarageInactive        = "GARAGE_INACTIVE"
	ReasonSubscriptionSuspended = "SUBSCRIPTION_SUSPENDED"
	ReasonSubscriptionReadOnly  = "SUBSCRIPTION_READ_ONLY"
)

// GarageScoped is implemented by any request message carrying a
// garage_id field -- every garage-scoped RPC of the .proto contract.
type GarageScoped interface {
	GetGarageId() string
}

// Transactor runs fn in a scoped transaction (see db.TxManager).
type Transactor interface {
	WithTransaction(ctx context.Context, scope platformdb.Scope, fn func(ctx context.Context) error) error
}

// ReasonPlatformDenied is reported when the caller lacks a platform permission.
const ReasonPlatformDenied = "PLATFORM_PERMISSION_REQUIRED"

// Garage runs every authenticated RPC in one Postgres transaction scoped to
// the caller (app.current_user_id) and, for garage-scoped RPCs, to the
// request's garage (app.current_garage_id) so Row-Level Security applies.
// Platform RPCs are authorized in the same transaction against the caller's
// platform permissions (Security Architecture 3.4).
// Inside it, garage-scoped RPCs are authorized against the policy's rule
// (owner or active member holding the permission, Security Architecture
// 3.2) and the garage owner's subscription is enforced (LLD 3.3): a
// locked subscription blocks everything, a past-due one blocks writes.
func Garage(
	tx Transactor, reader access.Reader, platform access.PlatformReader, license LicenseChecker, support SupportSessions,
) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		policy := policyFromContext(ctx)
		if policy.Public {
			return handler(ctx, req)
		}

		scope := platformdb.UserScope(UserID(ctx))

		if policy.Garage != nil {
			scoped, ok := req.(GarageScoped)
			if !ok || uuid.Validate(scoped.GetGarageId()) != nil {
				return nil, apperror.ToGRPCError(apperror.Validation("garageId", apperror.IssueInvalid, "garage_id is invalid"))
			}

			scope.GarageID = scoped.GetGarageId()
			observability.SetGarage(ctx, scope.GarageID)
		}

		var resp any

		err := tx.WithTransaction(ctx, scope, func(txCtx context.Context) error {
			if policy.Platform != "" {
				var err error

				txCtx, err = authorizePlatform(txCtx, platform, scope.UserID, policy.Platform)
				if err != nil {
					return err
				}
			}

			if scope.GarageID != "" {
				var err error

				txCtx, err = authorizeGarage(txCtx, reader, license, support, scope, policy, info.FullMethod, req)
				if err != nil {
					return err
				}
			}

			var err error

			resp, err = handler(txCtx, req)

			return err
		})
		if err != nil {
			return nil, apperror.ToGRPCError(err)
		}

		return resp, nil
	}
}

func authorizePlatform(
	ctx context.Context, reader access.PlatformReader, userID string, required access.PlatformPermission,
) (context.Context, error) {
	user, err := reader.PlatformAccess(ctx, userID)
	if err != nil {
		return nil, apperror.Internal("load platform access", err)
	}

	if err := user.Check(required); err != nil {
		return nil, apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err).WithReason(ReasonPlatformDenied)
	}

	return access.WithPlatformAccess(ctx, user), nil
}

func authorizeGarage(
	ctx context.Context, reader access.Reader, license LicenseChecker, support SupportSessions,
	scope platformdb.Scope, policy Policy, method string, req any,
) (context.Context, error) {
	user, err := reader.GarageAccess(ctx, scope.GarageID, scope.UserID)
	if err != nil {
		if errors.Is(err, access.ErrNoMembership) && support != nil {
			return authorizeSupport(ctx, support, *policy.Garage, scope.UserID, scope.GarageID, method, req)
		}

		if errors.Is(err, access.ErrNoMembership) {
			return nil, apperror.Wrap(apperror.CodePermissionDenied, access.ErrNoMembership.Error(), err).WithReason(ReasonGarageAccessDenied)
		}

		return nil, apperror.Internal("load garage access", err)
	}

	if err := checkRule(user, *policy.Garage); err != nil {
		return nil, err
	}

	ctx = access.WithAccess(ctx, user)

	if policy.SkipLicense || license == nil {
		return ctx, nil
	}

	switch licenseMode(ctx, license, scope.GarageID) {
	case LicenseLocked:
		return nil, apperror.New(apperror.CodeFailedPrecondition, msgLicenseLocked).WithReason(ReasonSubscriptionSuspended)
	case LicenseReadOnly:
		if !isReadMethod(method) {
			return nil, apperror.New(apperror.CodePermissionDenied, msgLicenseReadOnly).WithReason(ReasonSubscriptionReadOnly)
		}
	case LicenseFull:
	}

	return ctx, nil
}

// checkRule reports why user is not allowed rule, as an API error.
func checkRule(user *access.Access, rule access.Rule) error {
	err := user.Check(rule)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, access.ErrGarageClosed):
		return apperror.Wrap(apperror.CodeFailedPrecondition, err.Error(), err).WithReason(ReasonGarageInactive)
	case errors.Is(err, access.ErrOwnerOnly):
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err).WithReason(ReasonOwnerOnly)
	default:
		return apperror.Wrap(apperror.CodePermissionDenied, err.Error(), err)
	}
}

func licenseMode(ctx context.Context, checker LicenseChecker, garageID string) LicenseMode {
	mode, err := checker.LicenseMode(ctx, garageID)
	if err != nil {
		// Licensing must not take the API down: let the request through.
		slog.Warn("license check failed, allowing the request", "garage_id", garageID, "error", err)

		return LicenseFull
	}

	return mode
}
