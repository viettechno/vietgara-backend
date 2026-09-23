package handler

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	customerv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/customer/v1"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

const maxBodyBytes = 1 << 20

// RegisterHTTPRoutes adds the browser-facing endpoints of API Specification
// Section 5 to Echo. Bodies use the protojson form of the gRPC messages;
// path and query parameters fill the request fields with the same JSON name
// (garageId, customerId, page, pageSize, query, type, ...).
func (s *Server) RegisterHTTPRoutes(router *echo.Group) {
	g := router.Group("/garages/:garageId")

	g.GET("/customers", endpoint(s, http.StatusOK, s.ListCustomers, whole))
	g.POST("/customers", endpoint(s, http.StatusCreated, s.CreateCustomer, (*customerv1.CreateCustomerResponse).GetCustomer))
	g.GET("/customers/:customerId", endpoint(s, http.StatusOK, s.GetCustomer, (*customerv1.GetCustomerResponse).GetCustomer))
	g.PATCH("/customers/:customerId", endpoint(s, http.StatusOK, s.UpdateCustomer, (*customerv1.UpdateCustomerResponse).GetCustomer))
	g.DELETE("/customers/:customerId", endpoint(s, http.StatusNoContent, s.DeleteCustomer, none))

	g.GET("/customers/:customerId/vehicles", endpoint(s, http.StatusOK, s.ListVehicles, whole))
	g.POST("/customers/:customerId/vehicles", endpoint(s, http.StatusCreated, s.CreateVehicle, (*customerv1.CreateVehicleResponse).GetVehicle))
	g.GET("/vehicles", endpoint(s, http.StatusOK, s.ListVehicles, whole))
	g.POST("/vehicles", endpoint(s, http.StatusCreated, s.CreateVehicle, (*customerv1.CreateVehicleResponse).GetVehicle))
	g.GET("/vehicles/:vehicleId", endpoint(s, http.StatusOK, s.GetVehicle, (*customerv1.GetVehicleResponse).GetVehicle))
	g.PATCH("/vehicles/:vehicleId", endpoint(s, http.StatusOK, s.UpdateVehicle, (*customerv1.UpdateVehicleResponse).GetVehicle))
	g.DELETE("/vehicles/:vehicleId", endpoint(s, http.StatusNoContent, s.DeleteVehicle, none))

	g.GET("/partners", endpoint(s, http.StatusOK, s.ListPartners, whole))
	g.POST("/partners", endpoint(s, http.StatusCreated, s.CreatePartner, (*customerv1.CreatePartnerResponse).GetPartner))
	g.GET("/partners/:partnerId", endpoint(s, http.StatusOK, s.GetPartner, (*customerv1.GetPartnerResponse).GetPartner))
	g.PATCH("/partners/:partnerId", endpoint(s, http.StatusOK, s.UpdatePartner, (*customerv1.UpdatePartnerResponse).GetPartner))
	g.DELETE("/partners/:partnerId", endpoint(s, http.StatusNoContent, s.DeletePartner, none))
}

// garageScoped is a request message carrying a garage_id.
type garageScoped interface {
	proto.Message
	GetGarageId() string
}

// endpoint adapts one gRPC method to Echo: it decodes the JSON body of a
// POST/PATCH into a new request message, copies path (and, for GET, query)
// parameters over it -- path parameters always win, so a body cannot point
// at another garage --, runs call inside a transaction scoped to the
// request's garage, and writes out(resp) as protojson with okStatus, or an
// empty 204 when out returns nothing.
func endpoint[Req garageScoped, Resp, Out proto.Message](
	s *Server, okStatus int, call func(context.Context, Req) (Resp, error), out func(Resp) Out,
) echo.HandlerFunc {
	return func(c echo.Context) error {
		req, err := decodeRequest[Req](c)
		if err != nil {
			return writeHTTPError(c, err)
		}

		var body proto.Message

		err = s.inGarageTx(c.Request().Context(), req.GetGarageId(), func(ctx context.Context) error {
			resp, err := call(ctx, req)
			if err != nil {
				return err
			}

			body = out(resp)

			return nil
		})
		if err != nil {
			return writeHTTPError(c, err)
		}

		if body == nil || !body.ProtoReflect().IsValid() {
			return c.NoContent(http.StatusNoContent)
		}

		payload, err := protojson.Marshal(body)
		if err != nil {
			return writeHTTPError(c, status.Error(codes.Internal, "internal error"))
		}

		return c.JSONBlob(okStatus, payload)
	}
}

// whole returns the full response message, e.g. a list with its pagination.
func whole[Resp proto.Message](resp Resp) proto.Message { return resp }

// none drops the response message (DELETE answers 204 No Content).
func none[Resp proto.Message](Resp) proto.Message { return nil }

// decodeRequest builds a Req from the body, path and query of c.
func decodeRequest[Req garageScoped](c echo.Context) (Req, error) {
	var zero Req

	req, ok := zero.ProtoReflect().New().Interface().(Req)
	if !ok {
		return zero, status.Error(codes.Internal, "internal error")
	}

	method := c.Request().Method
	if method == http.MethodPost || method == http.MethodPatch {
		raw, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, maxBodyBytes))
		if err != nil {
			return zero, status.Error(codes.InvalidArgument, "invalid request body")
		}

		if len(raw) > 0 {
			if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, req); err != nil {
				return zero, status.Error(codes.InvalidArgument, "invalid request body")
			}
		}
	}

	if method == http.MethodGet {
		for name, values := range c.QueryParams() {
			if err := setField(req, name, values[0]); err != nil {
				return zero, err
			}
		}
	}

	for i, name := range c.ParamNames() {
		if err := setField(req, name, c.ParamValues()[i]); err != nil {
			return zero, err
		}
	}

	return req, nil
}

// setField sets the scalar field of msg whose JSON name is name. Unknown
// names are ignored so extra query parameters are harmless.
func setField(msg proto.Message, name, value string) error {
	m := msg.ProtoReflect()

	fd := m.Descriptor().Fields().ByJSONName(name)
	if fd == nil || fd.IsList() || fd.IsMap() {
		return nil
	}

	var v protoreflect.Value

	switch fd.Kind() { //nolint:exhaustive // path and query parameters only map to these kinds
	case protoreflect.StringKind:
		v = protoreflect.ValueOfString(value)
	case protoreflect.Int32Kind:
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "%s must be a number", name)
		}

		v = protoreflect.ValueOfInt32(int32(n))
	case protoreflect.EnumKind:
		n, ok := enumParam(fd.Enum(), value)
		if !ok {
			return status.Errorf(codes.InvalidArgument, "%s is invalid", name)
		}

		v = protoreflect.ValueOfEnum(n)
	default:
		return nil
	}

	m.Set(fd, v)

	return nil
}

// enumParam accepts an enum value by its full name (PARTNER_TYPE_SUPPLIER)
// or its short lower-case suffix (supplier); "" is the zero value.
func enumParam(enum protoreflect.EnumDescriptor, value string) (protoreflect.EnumNumber, bool) {
	if value == "" {
		return 0, true
	}

	values := enum.Values()
	if ev := values.ByName(protoreflect.Name(value)); ev != nil {
		return ev.Number(), true
	}

	suffix := "_" + strings.ToUpper(value)
	for i := range values.Len() {
		if ev := values.Get(i); ev.Number() != 0 && strings.HasSuffix(string(ev.Name()), suffix) {
			return ev.Number(), true
		}
	}

	return 0, false
}

// inGarageTx runs fn inside a transaction scoped to garageID so the RLS
// policies let the garage's rows through. Without a transaction manager
// (tests) fn runs directly. A malformed garage id never reaches Postgres:
// authorize rejects it inside fn, so it is not used as the tenant id.
func (s *Server) inGarageTx(ctx context.Context, garageID string, fn func(context.Context) error) error {
	if s.txManager == nil {
		return fn(ctx)
	}

	var tenant *string
	if uuid.Validate(garageID) == nil {
		tenant = &garageID
	}

	return s.txManager.WithTransaction(ctx, tenant, fn)
}

// writeHTTPError maps gRPC status errors and *apperror.AppError values to
// an HTTP status and the {"message": ...} body the web admin expects.
func writeHTTPError(c echo.Context, err error) error {
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
