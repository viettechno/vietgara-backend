package handler

import (
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
)

// Google OAuth routes: plain HTTP redirects, outside the gRPC contract.
const (
	GoogleLoginPath    = "/api/v1/auth/google/login"
	GoogleCallbackPath = "/api/v1/auth/google/callback"
)

// RegisterHTTPRoutes adds the Google OAuth redirect routes to the REST
// gateway mux.
func (s *Server) RegisterHTTPRoutes(mux *runtime.ServeMux) error {
	if err := mux.HandlePath(http.MethodGet, GoogleLoginPath, func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		s.googleLogin(w, r)
	}); err != nil {
		return err
	}

	return mux.HandlePath(http.MethodGet, GoogleCallbackPath, func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		s.googleCallback(w, r)
	})
}
