package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/viettechno/vietgara-backend/internal/identity/domain"
	"github.com/viettechno/vietgara-backend/internal/identity/usecase"
)

var googleHTTPClient = &http.Client{Timeout: 10 * time.Second}

type googleTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type googleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Locale        string `json:"locale"`
}

// exchangeGoogleCode trades the authorization code for the user's profile.
func exchangeGoogleCode(ctx context.Context, config GoogleOAuthConfig, code string) (usecase.GoogleProfile, error) {
	token, err := fetchGoogleToken(ctx, config, code)
	if err != nil {
		return usecase.GoogleProfile{}, err
	}

	info, err := fetchGoogleUserInfo(ctx, config, token.AccessToken)
	if err != nil {
		return usecase.GoogleProfile{}, err
	}

	if info.Sub == "" || info.Email == "" {
		return usecase.GoogleProfile{}, errors.New("google profile is missing required fields")
	}

	locale := domain.LocaleVI
	if strings.HasPrefix(strings.ToLower(info.Locale), "en") {
		locale = domain.LocaleEN
	}

	return usecase.GoogleProfile{
		Subject: info.Sub, Email: info.Email, EmailVerified: info.EmailVerified, FullName: info.Name, Locale: locale,
	}, nil
}

func fetchGoogleToken(ctx context.Context, config GoogleOAuthConfig, code string) (googleTokenResponse, error) {
	form := url.Values{
		"code": {code}, "client_id": {config.ClientID}, "client_secret": {config.ClientSecret},
		"redirect_uri": {config.redirectURI()}, "grant_type": {"authorization_code"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokenResponse{}, err
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := googleHTTPClient.Do(req)
	if err != nil {
		return googleTokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return googleTokenResponse{}, fmt.Errorf("token endpoint returned %s", resp.Status)
	}

	var token googleTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil || token.AccessToken == "" {
		return googleTokenResponse{}, errors.New("invalid token response")
	}

	return token, nil
}

func fetchGoogleUserInfo(ctx context.Context, config GoogleOAuthConfig, accessToken string) (googleUserInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.UserInfoURL, nil)
	if err != nil {
		return googleUserInfo{}, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := googleHTTPClient.Do(req)
	if err != nil {
		return googleUserInfo{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return googleUserInfo{}, fmt.Errorf("userinfo endpoint returned %s", resp.Status)
	}

	var info googleUserInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return googleUserInfo{}, err
	}

	return info, nil
}
