package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"github.com/viettechno/vietgara-backend/internal/platform/auth"
	"github.com/viettechno/vietgara-backend/pkg/apperror"
)

const (
	googleStateCookie = "vietgara_google_oauth_state"
	popupErrorKey     = "error"
	// linkStatePrefix marks a popup that links a Google account to the
	// signed-in user instead of signing in (FR-IAM-06). It is part of the
	// state, which the cookie and Google both carry, so it cannot be changed
	// on the way.
	linkStatePrefix = "link."
)

// Popup error codes posted to the web admin.
const (
	googleErrorNotConfigured = "GOOGLE_NOT_CONFIGURED"
	googleErrorState         = "GOOGLE_STATE_INVALID"
	googleErrorDenied        = "GOOGLE_DENIED"
	googleErrorExchange      = "GOOGLE_EXCHANGE_FAILED"
)

// GoogleOAuthConfig contains the provider endpoints and the public base URL.
// Client credentials must come from environment or secret management.
type GoogleOAuthConfig struct {
	ClientID      string
	ClientSecret  string
	AuthEndpoint  string
	TokenEndpoint string
	UserInfoURL   string
	// AppBaseURL is the public URL of the deployment (local, staging or
	// production). The web admin and the API share its origin behind Kong.
	AppBaseURL string
}

func (c GoogleOAuthConfig) configured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.origin() != ""
}

// origin is the scheme and host of AppBaseURL, or "" when it is not an
// absolute URL.
func (c GoogleOAuthConfig) origin() string {
	u, err := url.Parse(c.AppBaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}

	return u.Scheme + "://" + u.Host
}

// redirectURI is the callback registered with Google, on the public origin.
func (c GoogleOAuthConfig) redirectURI() string {
	return c.origin() + GoogleCallbackPath
}

// googleLogin starts the authorization-code flow in the sign-in popup.
func (s *Server) googleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.googleOAuth.configured() {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: googleErrorNotConfigured})

		return
	}

	state := auth.NewOpaqueToken()
	if r.URL.Query().Get("link") == "1" {
		state = linkStatePrefix + state
	}

	http.SetCookie(w, &http.Cookie{
		Name: googleStateCookie, Value: state, Path: "/", MaxAge: 600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: true,
	})

	query := url.Values{
		"client_id": {s.googleOAuth.ClientID}, "redirect_uri": {s.googleOAuth.redirectURI()},
		"response_type": {"code"}, "scope": {"openid email profile"},
		"state": {state}, "prompt": {"select_account"},
	}
	http.Redirect(w, r, s.googleOAuth.AuthEndpoint+"?"+query.Encode(), http.StatusFound)
}

// googleCallback finishes the flow and posts the session (or an error
// code) to the web admin window that opened the popup.
func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.googleOAuth.configured() {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: googleErrorNotConfigured})

		return
	}

	stateCookie, err := r.Cookie(googleStateCookie)
	http.SetCookie(w, &http.Cookie{
		Name: googleStateCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: true,
	})

	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: googleErrorState})

		return
	}

	code := r.URL.Query().Get("code")
	if r.URL.Query().Get("error") != "" || code == "" {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: googleErrorDenied})

		return
	}

	profile, err := exchangeGoogleCode(r.Context(), s.googleOAuth, code)
	if err != nil {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: googleErrorExchange})

		return
	}

	if strings.HasPrefix(stateCookie.Value, linkStatePrefix) {
		s.googleLinkProof(w, r, profile)

		return
	}

	session, err := s.uc.GoogleLogin(r.Context(), profile)
	if err != nil {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: apperror.ReasonOf(err)})

		return
	}

	sessionJSON, err := protojson.MarshalOptions{EmitDefaultValues: true}.Marshal(s.sessionToProto(session))
	if err != nil {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: apperror.ReasonInternal})

		return
	}

	s.writeGooglePopup(w, map[string]any{"session": json.RawMessage(sessionJSON)})
}

// googleLinkProof ends a linking popup: it posts a single-use proof of the
// Google account to the console window that opened the popup, and to no
// other origin, so a page that is not the console cannot receive it. The
// console then claims it with CompleteGoogleLink, as the signed-in user.
func (s *Server) googleLinkProof(w http.ResponseWriter, r *http.Request, profile usecase.GoogleProfile) {
	proof, err := s.uc.CreateGoogleLinkProof(r.Context(), profile)
	if err != nil {
		s.writeGooglePopup(w, map[string]any{popupErrorKey: apperror.ReasonOf(err)})

		return
	}

	s.writeGooglePopup(w, map[string]any{"linkProof": proof})
}

// writeGooglePopup posts {type: "vietgara-google-auth", ...payload} to the
// opener (restricted to the web admin origin) and closes the popup.
func (s *Server) writeGooglePopup(w http.ResponseWriter, payload map[string]any) {
	// Without a configured origin, "/" restricts the message to the popup's
	// own origin, which is the web admin's behind Kong.
	origin := s.googleOAuth.origin()
	if origin == "" {
		origin = "/"
	}

	payload["type"] = "vietgara-google-auth"
	message, _ := json.Marshal(payload)

	page := `<!doctype html><meta charset="utf-8"><title>Google</title><script>` +
		`if(window.opener){window.opener.postMessage(` + string(message) + `,` + strconv.Quote(origin) + `);}window.close();</script>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, page)
}
