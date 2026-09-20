package converter

import (
	"net/http"

	"google.golang.org/grpc/codes"
)

var grpcCodeHTTPStatus = map[codes.Code]int{
	codes.OK:                 http.StatusOK,
	codes.Canceled:           http.StatusRequestTimeout,
	codes.Unknown:            http.StatusInternalServerError,
	codes.InvalidArgument:    http.StatusBadRequest,
	codes.DeadlineExceeded:   http.StatusGatewayTimeout,
	codes.NotFound:           http.StatusNotFound,
	codes.PermissionDenied:   http.StatusForbidden,
	codes.ResourceExhausted:  http.StatusTooManyRequests,
	codes.FailedPrecondition: http.StatusPreconditionFailed,
	codes.Aborted:            http.StatusConflict,
	codes.OutOfRange:         http.StatusBadRequest,
	codes.Unimplemented:      http.StatusNotImplemented,
	codes.Internal:           http.StatusInternalServerError,
	codes.Unavailable:        http.StatusServiceUnavailable,
	codes.DataLoss:           http.StatusInternalServerError,
	codes.Unauthenticated:    http.StatusUnauthorized,
	codes.AlreadyExists:      http.StatusConflict,
}

// GRPCCodeToHTTPStatus converts a gRPC status code to an HTTP status code. If the gRPC code is not recognized, it returns http.StatusInternalServerError.
func GRPCCodeToHTTPStatus(code codes.Code) int {
	if httpCode, ok := grpcCodeHTTPStatus[code]; ok {
		return httpCode
	}

	return http.StatusInternalServerError
}
