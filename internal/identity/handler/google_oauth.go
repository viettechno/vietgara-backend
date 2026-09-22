package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
	"google.golang.org/protobuf/encoding/protojson"
)

const googleStateCookie = "vietgara_google_oauth_state"

// GoogleOAuthConfig contains the provider endpoints and the public callback.
// Client credentials must come from environment or secret management.
type GoogleOAuthConfig struct {
	ClientID       string
	ClientSecret   string
	AuthEndpoint   string
	TokenEndpoint  string
	UserInfoURL    string
	RedirectURI    string
	FrontendOrigin string
}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type googleUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (s *Server) googleLogin(w http.ResponseWriter, r *http.Request) {
	config, ok := s.googleConfig(w)
	if !ok {
		return
	}

	state, err := randomState()
	if err != nil {
		http.Error(w, "unable to start Google login", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})

	query := url.Values{
		"client_id": {config.ClientID}, "redirect_uri": {config.RedirectURI},
		"response_type": {"code"}, "scope": {"openid email profile"},
		"state": {state}, "access_type": {"offline"}, "prompt": {"select_account"},
	}
	http.Redirect(w, r, config.AuthEndpoint+"?"+query.Encode(), http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	config, ok := s.googleConfig(w)
	if !ok {
		return
	}

	stateCookie, err := r.Cookie(googleStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     googleStateCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	})

	if providerError := r.URL.Query().Get("error"); providerError != "" {
		http.Error(w, "Google authorization failed: "+providerError, http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	profile, err := exchangeGoogleCode(r.Context(), config, code)
	if err != nil {
		http.Error(w, "Google token exchange failed", http.StatusBadGateway)
		return
	}

	result, err := s.uc.SocialLogin(r.Context(), usecase.VerifiedSocialProfile{
		IdentityPlatformUID: profile.Sub,
		Provider:            domain.AuthProviderGoogle,
		Email:               optionalString(profile.Email),
		FullName:            profile.Name,
	})
	if err != nil {
		writeGRPCHTTPError(w, err)
		return
	}

	authJSON, err := protojson.Marshal(s.authToProto(result))
	if err != nil {
		http.Error(w, "unable to create session", http.StatusInternalServerError)
		return
	}

	writeGooglePopupResponse(w, config.FrontendOrigin, authJSON)
}

func (s *Server) googleConfig(w http.ResponseWriter) (GoogleOAuthConfig, bool) {
	if s.googleOAuth == nil || s.googleOAuth.ClientID == "" || s.googleOAuth.ClientSecret == "" || s.googleOAuth.RedirectURI == "" {
		http.Error(w, "Google login is not configured", http.StatusNotImplemented)
		return GoogleOAuthConfig{}, false
	}

	return *s.googleOAuth, true
}

func exchangeGoogleCode(ctx context.Context, config GoogleOAuthConfig, code string) (googleUserInfo, error) {
	token, err := fetchGoogleToken(ctx, config, code)
	if err != nil {
		return googleUserInfo{}, err
	}

	profile, err := fetchGoogleUserInfo(ctx, config, token.AccessToken)
	if err != nil {
		return googleUserInfo{}, err
	}

	if profile.Sub == "" || profile.Name == "" {
		return googleUserInfo{}, errors.New("google profile is missing required fields")
	}

	return profile, nil
}

func fetchGoogleToken(ctx context.Context, config GoogleOAuthConfig, code string) (googleTokenResponse, error) {
	form := url.Values{
		"code": {code}, "client_id": {config.ClientID}, "client_secret": {config.ClientSecret},
		"redirect_uri": {config.RedirectURI}, "grant_type": {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokenResponse{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return googleTokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return googleTokenResponse{}, fmt.Errorf("token endpoint returned %s", resp.Status)
	}

	var token googleTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil || token.AccessToken == "" {
		return googleTokenResponse{}, errors.New("invalid token response")
	}

	return token, nil
}

func fetchGoogleUserInfo(ctx context.Context, config GoogleOAuthConfig, accessToken string) (googleUserInfo, error) {
	userinfoReq, err := http.NewRequestWithContext(ctx, http.MethodGet, config.UserInfoURL, nil)
	if err != nil {
		return googleUserInfo{}, err
	}

	userinfoReq.Header.Set("Authorization", "Bearer "+accessToken)

	userinfoResp, err := http.DefaultClient.Do(userinfoReq)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer func() { _ = userinfoResp.Body.Close() }()

	if userinfoResp.StatusCode < http.StatusOK || userinfoResp.StatusCode >= http.StatusMultipleChoices {
		return googleUserInfo{}, fmt.Errorf("userinfo endpoint returned %s", userinfoResp.Status)
	}

	var profile googleUserInfo
	if err := json.NewDecoder(io.LimitReader(userinfoResp.Body, 1<<20)).Decode(&profile); err != nil {
		return googleUserInfo{}, err
	}

	return profile, nil
}

func randomState() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(value), nil
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func writeGooglePopupResponse(w http.ResponseWriter, origin string, authJSON []byte) {
	if origin == "" {
		origin = "http://localhost:8000"
	}

	page := `<!doctype html><meta charset="utf-8"><title>Google login</title><script>window.opener.postMessage({type:"vietgara-google-auth",auth:JSON.parse(` + strconv.Quote(string(authJSON)) + `)},` + strconv.Quote(origin) + `);window.close();</script>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}
