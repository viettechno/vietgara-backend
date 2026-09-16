package handler

import (
	"encoding/json"
	"net/http"

	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type emailPasswordRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type registerRequest struct {
	emailPasswordRequest
	FullName string `json:"fullName"`
}

// ServeHTTP exposes the browser-facing auth endpoints. The gRPC service stays
// the source of truth; these endpoints only translate JSON to the same RPC
// request types used by native clients.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		switch r.URL.Path {
		case "/auth/google/login":
			s.googleLogin(w, r)
		case "/auth/google/callback":
			s.googleCallback(w, r)
		default:
			writeHTTPError(w, http.StatusNotFound, "not found")
		}

		return
	}

	if r.Method != http.MethodPost {
		writeHTTPError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch r.URL.Path {
	case "/auth/register":
		s.handleHTTPRegister(w, r)
	case "/auth/login":
		s.handleHTTPLogin(w, r)
	default:
		writeHTTPError(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleHTTPRegister(w http.ResponseWriter, r *http.Request) {
	var payload registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	response, err := s.Register(r.Context(), &identityv1.RegisterRequest{
		Method: &identityv1.RegisterRequest_EmailPassword{
			EmailPassword: &identityv1.EmailPasswordCredential{
				Email:    payload.Email,
				Password: payload.Password,
			},
		},
		FullName: payload.FullName,
	})
	if err != nil {
		writeGRPCHTTPError(w, err)
		return
	}

	writeProtoJSON(w, response.GetAuth())
}

func (s *Server) handleHTTPLogin(w http.ResponseWriter, r *http.Request) {
	var payload emailPasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeHTTPError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	response, err := s.Login(r.Context(), &identityv1.LoginRequest{
		Method: &identityv1.LoginRequest_EmailPassword{
			EmailPassword: &identityv1.EmailPasswordCredential{
				Email:    payload.Email,
				Password: payload.Password,
			},
		},
	})
	if err != nil {
		writeGRPCHTTPError(w, err)
		return
	}

	writeProtoJSON(w, response.GetAuth())
}

func writeProtoJSON(w http.ResponseWriter, message protoreflect.ProtoMessage) {
	body, err := protojson.Marshal(message)
	if err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeGRPCHTTPError(w http.ResponseWriter, err error) {
	writeHTTPError(w, grpcCodeToHTTPStatus(status.Code(err)), status.Convert(err).Message())
}

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

func grpcCodeToHTTPStatus(code codes.Code) int {
	if httpCode, ok := grpcCodeHTTPStatus[code]; ok {
		return httpCode
	}

	return http.StatusInternalServerError
}

func writeHTTPError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
