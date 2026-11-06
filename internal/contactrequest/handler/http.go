// Package handler serves the website's contact request endpoint, a plain HTTP
// route beside the gRPC gateway (API Specification, public routes).
package handler

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/viettechno/vietgara-backend/internal/contactrequest/domain"
	"github.com/viettechno/vietgara-backend/internal/contactrequest/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/httpapi"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

// Path is where the website's server-side function posts a request.
const Path = "/api/v1/public/contact-requests"

// KeyHeader carries the key the website's function shares with the backend.
const KeyHeader = "X-Contact-Key"

const maxBodyBytes = 16 << 10

// Server serves the endpoint.
type Server struct {
	uc  *usecase.UseCase
	key string
}

// NewServer returns a server that accepts requests carrying key.
func NewServer(uc *usecase.UseCase, key string) *Server {
	return &Server{uc: uc, key: key}
}

// RegisterHTTPRoutes adds the endpoint to mux. Without a key the endpoint is
// off: anyone could post, and the anti-spam check lives in the website's function.
func (s *Server) RegisterHTTPRoutes(mux *runtime.ServeMux) error {
	if s.key == "" {
		return nil
	}

	return mux.HandlePath(http.MethodPost, Path, func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		s.create(w, r)
	})
}

type requestBody struct {
	Name       string `json:"name"`
	GarageName string `json:"garageName"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	Message    string `json:"message"`
	Consent    bool   `json:"consent"`
	Locale     string `json:"locale"`
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(KeyHeader)), []byte(s.key)) != 1 {
		httpapi.WriteError(w, r, apperror.New(apperror.CodeUnauthenticated, "missing or wrong key"))

		return
	}

	var body requestBody

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpapi.WriteError(w, r, apperror.Validation("body", "valid JSON of up to 16 KB", "invalid contact request"))

		return
	}

	id, err := s.uc.Create(r.Context(), domain.Input(body), usecase.Source{IP: sourceIP(r), UserAgent: r.UserAgent()})
	if err != nil {
		httpapi.WriteError(w, r, err)

		return
	}

	httpapi.WriteJSON(w, http.StatusAccepted, map[string]string{"id": id})
}

// sourceIP is the address the gateway resolved (it overwrites X-Real-IP), else
// the peer of the connection.
func sourceIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}
