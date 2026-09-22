package handler

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/viettechno/vietgara-backend/internal/middleware"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/tenant/domain"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
	tenantv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/tenant/v1"
)

const (
	maxGarageBodyBytes = 1 << 20
	maxTaxCodeLen      = 20
	maxTimezoneLen     = 20
	currencyLen        = 3
)

// ServerOption customizes a Server.
type ServerOption func(*Server)

// WithTxManager lets the HTTP garage endpoints open a garage-scoped
// transaction (setting app.current_garage_id for Row-Level Security) the way
// TransactionInterceptor does for gRPC requests.
func WithTxManager(txManager *platformdb.TxManager) ServerOption {
	return func(s *Server) {
		s.txManager = txManager
	}
}

// registerGarageHTTPRoutes adds the browser-facing garage endpoints. Request
// and response bodies use the protojson form of the gRPC messages.
func (s *Server) registerGarageHTTPRoutes(router *echo.Group) {
	router.GET("/garages", s.listHTTPGarages)
	router.POST("/garages", s.createHTTPGarage)
	router.GET("/garages/:id", s.getHTTPGarage)
	router.PATCH("/garages/:id", s.updateHTTPGarage)
}

func (s *Server) listHTTPGarages(c echo.Context) error {
	resp, err := s.ListGarages(c.Request().Context(), &tenantv1.ListGaragesRequest{})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return writeGarageProtoJSON(c, http.StatusOK, resp)
}

func (s *Server) createHTTPGarage(c echo.Context) error {
	var req tenantv1.CreateGarageRequest
	if err := decodeGarageBody(c, &req); err != nil {
		return writeGarageHTTPError(c, err)
	}

	if err := validateGarageFields(req.GetName(), req.Timezone, req.Currency, req.TaxCode); err != nil {
		return writeGarageHTTPError(c, err)
	}

	if strings.TrimSpace(req.GetName()) == "" {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "name is required"))
	}

	resp, err := s.CreateGarage(c.Request().Context(), &req)
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return writeGarageProtoJSON(c, http.StatusCreated, resp.GetGarage())
}

func (s *Server) getHTTPGarage(c echo.Context) error {
	garageID := c.Param("id")

	var garage *tenantv1.Garage

	err := s.inGarageTx(c.Request().Context(), garageID, func(ctx context.Context) error {
		g, err := s.authorizedGarage(ctx, garageID, false)
		if err != nil {
			return err
		}

		garage = garageToProto(g)

		return nil
	})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return writeGarageProtoJSON(c, http.StatusOK, garage)
}

func (s *Server) updateHTTPGarage(c echo.Context) error {
	garageID := c.Param("id")

	var req tenantv1.UpdateGarageRequest
	if err := decodeGarageBody(c, &req); err != nil {
		return writeGarageHTTPError(c, err)
	}

	req.GarageId = garageID

	if req.Name != nil && strings.TrimSpace(req.GetName()) == "" {
		return writeGarageHTTPError(c, status.Error(codes.InvalidArgument, "name must not be empty"))
	}

	if err := validateGarageFields(req.GetName(), req.Timezone, req.Currency, req.TaxCode); err != nil {
		return writeGarageHTTPError(c, err)
	}

	var garage *tenantv1.Garage

	err := s.inGarageTx(c.Request().Context(), garageID, func(ctx context.Context) error {
		if _, err := s.authorizedGarage(ctx, garageID, true); err != nil {
			return err
		}

		resp, err := s.UpdateGarage(ctx, &req)
		if err != nil {
			return err
		}

		garage = resp.GetGarage()

		return nil
	})
	if err != nil {
		return writeGarageHTTPError(c, err)
	}

	return writeGarageProtoJSON(c, http.StatusOK, garage)
}

// inGarageTx runs fn inside a transaction scoped to garageID so the garages
// RLS policy lets the row through. Without a transaction manager (tests) fn
// runs directly.
func (s *Server) inGarageTx(ctx context.Context, garageID string, fn func(context.Context) error) error {
	if s.txManager == nil {
		return fn(ctx)
	}

	return s.txManager.WithTransaction(ctx, &garageID, fn)
}

// authorizedGarage loads the garage and checks that the caller may see it
// (or change it, when write is true). The account that owns the garage always
// may; other staff need the garage in their token, and only its owners may
// write. Ownership is checked against the database rather than the token so a
// garage created moments ago works before the user's token is refreshed.
func (s *Server) authorizedGarage(ctx context.Context, garageID string, write bool) (*domain.Garage, error) {
	claims, ok := middleware.ClaimsFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing account context")
	}

	g, err := s.garage.Get(ctx, garageID)
	if err != nil {
		return nil, err
	}

	if g.AccountID == claims.Subject {
		return g, nil
	}

	role, hasAccess := claims.HasAccessToGarage(garageID)
	if !hasAccess || (write && role != string(domain.RoleOwner)) {
		return nil, status.Error(codes.PermissionDenied, "no access to this garage")
	}

	return g, nil
}

func validateGarageFields(name string, timezone, currency, taxCode *string) error {
	switch {
	case len(name) > 255:
		return status.Error(codes.InvalidArgument, "name is too long")
	case timezone != nil && (*timezone == "" || len(*timezone) > maxTimezoneLen):
		return status.Error(codes.InvalidArgument, "timezone is invalid")
	case currency != nil && len(*currency) != currencyLen:
		return status.Error(codes.InvalidArgument, "currency must be a 3-letter code")
	case taxCode != nil && len(*taxCode) > maxTaxCodeLen:
		return status.Error(codes.InvalidArgument, "taxCode is too long")
	}

	return nil
}

func decodeGarageBody(c echo.Context, msg proto.Message) error {
	body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxGarageBodyBytes))
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid request body")
	}

	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, msg); err != nil {
		return status.Error(codes.InvalidArgument, "invalid request body")
	}

	return nil
}

func writeGarageProtoJSON(c echo.Context, statusCode int, msg proto.Message) error {
	body, err := protojson.Marshal(msg)
	if err != nil {
		return writeGarageHTTPError(c, status.Error(codes.Internal, "internal error"))
	}

	return c.JSONBlob(statusCode, body)
}

// writeGarageHTTPError maps gRPC status errors and *apperror.AppError values
// to an HTTP status and the {"message": ...} body the web admin expects.
func writeGarageHTTPError(c echo.Context, err error) error {
	if _, isStatus := status.FromError(err); !isStatus {
		err = apperror.ToGRPCError(err)
	}

	st := status.Convert(err)
	message := st.Message()

	if st.Code() == codes.Unknown || st.Code() == codes.Internal {
		message = "internal error"
	}

	return c.JSON(converter.GRPCCodeToHTTPStatus(st.Code()), map[string]string{"message": message})
}
