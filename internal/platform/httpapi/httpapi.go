// Package httpapi serves the browser-facing REST/JSON API (API
// Specification) from the gRPC services through grpc-gateway: the REST
// mapping is declared on the .proto RPCs (google.api.http), so REST and
// gRPC can never drift apart.
//
// Conventions applied here (API Specification Section 1.1):
//   - JSON field names are camelCase; zero values are emitted.
//   - Errors are {"error": {"code", "message", "details"}}; code is the
//     stable reason clients translate.
//   - Create RPCs answer 201 and DELETE RPCs 204 without a body.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/viettechno/vietgara-backend/pkg/apperror"
	"github.com/viettechno/vietgara-backend/pkg/converter"
)

// httpCodeHeader carries the HTTP status an RPC asks for (see Created).
const httpCodeHeader = "x-http-code"

// Created marks a successful RPC response as 201 Created.
func Created(ctx context.Context) {
	_ = grpc.SetHeader(ctx, metadata.Pairs(httpCodeHeader, strconv.Itoa(http.StatusCreated)))
}

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Error ErrorPayload `json:"error"`
}

// ErrorPayload describes why a request failed.
type ErrorPayload struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details"`
}

// ErrorDetail is one invalid field.
type ErrorDetail struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

// reasonStatus overrides the HTTP status of some reasons.
var reasonStatus = map[string]int{
	"SUBSCRIPTION_SUSPENDED": http.StatusPaymentRequired,
	"PLAN_LIMIT_REACHED":     http.StatusPaymentRequired,
}

// NewMux returns the gateway mux with VietGara's conventions. Handlers are
// registered on it by the caller (Register*HandlerClient / HandlePath).
//
// The marshaler is wrapped in HTTPBodyMarshaler so a google.api.HttpBody
// response (Document/Print's PDF/Excel exports) is written as its raw
// bytes with its own Content-Type, not JSON-encoded like every other
// response -- without this wrapper, ForwardResponseMessage has no special
// case for HttpBody and silently falls back to the plain JSONPb marshaler.
func NewMux() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.HTTPBodyMarshaler{
			Marshaler: &runtime.JSONPb{
				MarshalOptions:   protojson.MarshalOptions{EmitDefaultValues: true},
				UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: true},
			},
		}),
		runtime.WithErrorHandler(errorHandler),
		runtime.WithRoutingErrorHandler(routingErrorHandler),
		runtime.WithForwardResponseOption(forwardStatus),
		runtime.WithIncomingHeaderMatcher(incomingHeaders),
		runtime.WithOutgoingHeaderMatcher(func(string) (string, bool) { return "", false }),
	)
}

// incomingHeaders forwards the headers handlers need as gRPC metadata.
func incomingHeaders(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "authorization", "accept-language", "x-request-id":
		return strings.ToLower(key), true
	default:
		return "", false
	}
}

// forwardStatus applies the status requested with Created.
func forwardStatus(ctx context.Context, w http.ResponseWriter, _ proto.Message) error {
	md, ok := runtime.ServerMetadataFromContext(ctx)
	if !ok {
		return nil
	}

	if values := md.HeaderMD.Get(httpCodeHeader); len(values) > 0 {
		if code, err := strconv.Atoi(values[0]); err == nil {
			w.WriteHeader(code)
		}
	}

	return nil
}

// errorHandler writes gRPC errors as the JSON error envelope.
func errorHandler(_ context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, _ *http.Request, err error) {
	st := status.Convert(err)
	body := ErrorBody{Error: ErrorPayload{Code: reasonFor(st), Message: st.Message(), Details: []ErrorDetail{}}}

	for _, detail := range st.Details() {
		if br, ok := detail.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				body.Error.Details = append(body.Error.Details, ErrorDetail{Field: v.GetField(), Issue: v.GetDescription()})
			}
		}
	}

	httpStatus := converter.GRPCCodeToHTTPStatus(st.Code())

	switch override, ok := reasonStatus[body.Error.Code]; {
	case ok:
		httpStatus = override
	case st.Code() == codes.InvalidArgument && hasReason(st):
		// A request the handler understood but refused (OTP_INVALID, ...)
		// is unprocessable; only malformed requests stay 400.
		httpStatus = http.StatusUnprocessableEntity
	}

	if httpStatus == http.StatusInternalServerError {
		body.Error.Message = "internal error"
	}

	WriteJSON(w, httpStatus, body)
}

func routingErrorHandler(ctx context.Context, mux *runtime.ServeMux, m runtime.Marshaler, w http.ResponseWriter, r *http.Request, httpStatus int) {
	code := codes.NotFound
	if httpStatus == http.StatusMethodNotAllowed {
		code = codes.Unimplemented
	}

	errorHandler(ctx, mux, m, w, r, status.Error(code, http.StatusText(httpStatus)))
}

func hasReason(st *status.Status) bool {
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetReason() != "" {
			return true
		}
	}

	return false
}

// codeReasons are the default reasons of statuses without an ErrorInfo.
var codeReasons = map[codes.Code]string{
	codes.InvalidArgument:    "BAD_REQUEST",
	codes.NotFound:           apperror.ReasonNotFound,
	codes.Unauthenticated:    apperror.ReasonUnauthenticated,
	codes.PermissionDenied:   apperror.ReasonPermissionDenied,
	codes.AlreadyExists:      apperror.ReasonAlreadyExists,
	codes.FailedPrecondition: apperror.ReasonFailedPrecondition,
	codes.ResourceExhausted:  apperror.ReasonRateLimited,
	codes.Unimplemented:      "METHOD_NOT_ALLOWED",
}

// reasonFor is the ErrorInfo reason of st, or a code-derived default.
func reasonFor(st *status.Status) string {
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.GetReason() != "" {
			return info.GetReason()
		}
	}

	if reason, ok := codeReasons[st.Code()]; ok {
		return reason
	}

	return apperror.ReasonInternal
}

// WriteJSON writes v as a JSON response.
func WriteJSON(w http.ResponseWriter, httpStatus int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes err (an AppError or gRPC status) as the JSON error
// envelope, for the few plain HTTP handlers outside grpc-gateway.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	errorHandler(r.Context(), nil, nil, w, r, apperror.ToGRPCError(err))
}

// NoContentForDelete turns the empty 200 answer of a DELETE RPC into 204
// No Content, as REST clients expect.
func NoContentForDelete(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)

			return
		}

		next.ServeHTTP(&deleteWriter{ResponseWriter: w}, r)
	})
}

type deleteWriter struct {
	http.ResponseWriter

	status  int
	decided bool
	noBody  bool
}

func (d *deleteWriter) WriteHeader(code int) {
	if d.decided || d.status != 0 {
		return
	}

	d.status = code
}

func (d *deleteWriter) Write(b []byte) (int, error) {
	if !d.decided {
		d.decided = true

		code := d.status
		if code == 0 {
			code = http.StatusOK
		}

		if code == http.StatusOK && strings.TrimSpace(string(b)) == "{}" {
			d.noBody = true
			d.ResponseWriter.Header().Del("Content-Type")
			d.ResponseWriter.WriteHeader(http.StatusNoContent)

			return len(b), nil
		}

		d.ResponseWriter.WriteHeader(code)
	}

	if d.noBody {
		return len(b), nil
	}

	return d.ResponseWriter.Write(b)
}
