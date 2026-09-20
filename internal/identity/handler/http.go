package handler

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/viettechno/vietgara-backend/pkg/converter"
	identityv1 "github.com/viettechno/vietgara-proto/apis-go/vietgara/identity/v1"
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

// RegisterHTTPRoutes adds the browser-facing auth endpoints to Echo.
func (s *Server) RegisterHTTPRoutes(router *echo.Group) {
	router.POST("/auth/register", echo.WrapHandler(http.HandlerFunc(s.handleHTTPRegister)))
	router.POST("/auth/login", echo.WrapHandler(http.HandlerFunc(s.handleHTTPLogin)))
	router.GET("/auth/google/login", echo.WrapHandler(http.HandlerFunc(s.googleLogin)))
	router.GET("/auth/google/callback", echo.WrapHandler(http.HandlerFunc(s.googleCallback)))
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
	writeHTTPError(w, converter.GRPCCodeToHTTPStatus(status.Code(err)), status.Convert(err).Message())
}

func writeHTTPError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}
