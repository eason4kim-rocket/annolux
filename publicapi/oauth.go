package publicapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/eason4kim-rocket/annolux/accounts"
	"golang.org/x/oauth2"
)

const (
	googleIssuer       = "https://accounts.google.com"
	googleAuthURL      = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL     = "https://oauth2.googleapis.com/token"
	githubAuthURL      = "https://github.com/login/oauth/authorize"
	githubTokenURL     = "https://github.com/login/oauth/access_token"
	githubUserURL      = "https://api.github.com/user"
	githubUserEmailURL = "https://api.github.com/user/emails"
	maximumOAuthBody   = 1 << 20
)

type OAuthClientConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (config OAuthClientConfig) configured() bool {
	return config.ClientID != "" || config.ClientSecret != "" || config.RedirectURL != ""
}

func (config OAuthClientConfig) complete() bool {
	return config.ClientID != "" && config.ClientSecret != "" && config.RedirectURL != ""
}

type OAuthConfig struct {
	Google     OAuthClientConfig
	GitHub     OAuthClientConfig
	HTTPClient *http.Client
	SessionTTL time.Duration
}

func (config OAuthConfig) Enabled() bool {
	return config.Google.configured() || config.GitHub.configured()
}

type googleTokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

type OAuthService struct {
	accounts       *accounts.Store
	google         *oauth2.Config
	github         *oauth2.Config
	client         *http.Client
	ttl            time.Duration
	verifierMu     sync.Mutex
	googleVerifier googleTokenVerifier
}

func NewOAuthService(config OAuthConfig, store *accounts.Store) (*OAuthService, error) {
	if store == nil || !config.Google.complete() || !config.GitHub.complete() {
		return nil, errors.New("publicapi: Google and GitHub OAuth must both be configured")
	}
	if !validOAuthRedirect(config.Google.RedirectURL, "/api/v1/auth/google/callback") || !validOAuthRedirect(config.GitHub.RedirectURL, "/api/v1/auth/github/callback") {
		return nil, errors.New("publicapi: invalid OAuth redirect URL")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = accounts.DefaultSessionTTL
	}
	if config.SessionTTL <= 0 || config.SessionTTL > 90*24*time.Hour {
		return nil, errors.New("publicapi: invalid OAuth session TTL")
	}
	return &OAuthService{
		accounts: store, client: config.HTTPClient, ttl: config.SessionTTL,
		google: &oauth2.Config{ClientID: config.Google.ClientID, ClientSecret: config.Google.ClientSecret, RedirectURL: config.Google.RedirectURL, Scopes: []string{"openid", "email", "profile"}, Endpoint: oauth2.Endpoint{AuthURL: googleAuthURL, TokenURL: googleTokenURL}},
		github: &oauth2.Config{ClientID: config.GitHub.ClientID, ClientSecret: config.GitHub.ClientSecret, RedirectURL: config.GitHub.RedirectURL, Scopes: []string{"read:user", "user:email"}, Endpoint: oauth2.Endpoint{AuthURL: githubAuthURL, TokenURL: githubTokenURL}},
	}, nil
}

func validOAuthRedirect(raw, path string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != path {
		return false
	}
	if parsed.Scheme == "https" && (parsed.Host == "annolux.com" || parsed.Host == "api.annolux.com") {
		return true
	}
	return parsed.Scheme == "http" && parsed.Hostname() == "127.0.0.1"
}

func (server *Server) handleOAuthStart(provider string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if server.oauth == nil {
			writeError(writer, http.StatusServiceUnavailable, "OAUTH_UNAVAILABLE", "login is not configured")
			return
		}
		returnPath := request.URL.Query().Get("return")
		if returnPath == "" {
			returnPath = "/account"
		}
		location, challenge, err := server.beginOAuth(request.Context(), provider, returnPath, "")
		if err != nil {
			writeError(writer, http.StatusBadRequest, "OAUTH_FAILED", "login could not be started")
			return
		}
		http.SetCookie(writer, &http.Cookie{
			Name: OAuthCookieName, Value: challenge.BrowserCookie, Path: "/", Expires: challenge.ExpiresAt,
			Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(writer, request, location, http.StatusFound)
	}
}

func (server *Server) handleOAuthLinkStart(provider string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if server.oauth == nil {
			writeError(writer, http.StatusServiceUnavailable, "OAUTH_UNAVAILABLE", "login is not configured")
			return
		}
		session, ok := server.requireWriteSession(writer, request)
		if !ok {
			return
		}
		returnPath := request.URL.Query().Get("return")
		if returnPath == "" {
			returnPath = "/account"
		}
		location, challenge, err := server.beginOAuth(request.Context(), provider, returnPath, session.Session.AccountID)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "OAUTH_FAILED", "identity linking could not be started")
			return
		}
		http.SetCookie(writer, &http.Cookie{
			Name: OAuthCookieName, Value: challenge.BrowserCookie, Path: "/", Expires: challenge.ExpiresAt,
			Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		writeJSON(writer, http.StatusOK, map[string]string{"authorization_url": location})
	}
}

func (server *Server) beginOAuth(ctx context.Context, provider, returnPath, accountID string) (string, accounts.OAuthChallenge, error) {
	state, challenge, err := server.accounts.CreateOAuthChallengeForAccount(ctx, provider, returnPath, accountID, accounts.DefaultOAuthTTL)
	if err != nil {
		return "", accounts.OAuthChallenge{}, err
	}
	location, err := server.oauth.authorizationURL(provider, state, challenge)
	if err != nil {
		return "", accounts.OAuthChallenge{}, err
	}
	return location, challenge, nil
}

func (service *OAuthService) authorizationURL(provider, state string, challenge accounts.OAuthChallenge) (string, error) {
	pkce := oauth2.S256ChallengeOption(challenge.CodeVerifier)
	switch provider {
	case "google":
		return service.google.AuthCodeURL(state, pkce, oauth2.SetAuthURLParam("nonce", challenge.Nonce), oauth2.SetAuthURLParam("prompt", "select_account")), nil
	case "github":
		return service.github.AuthCodeURL(state, pkce, oauth2.SetAuthURLParam("prompt", "select_account")), nil
	default:
		return "", errors.New("unsupported OAuth provider")
	}
}

func (server *Server) handleOAuthCallback(provider string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if server.oauth == nil {
			writeError(writer, http.StatusServiceUnavailable, "OAUTH_UNAVAILABLE", "login is not configured")
			return
		}
		state := request.URL.Query().Get("state")
		cookie, err := request.Cookie(OAuthCookieName)
		if err != nil {
			server.redirectOAuthError(writer, request)
			return
		}
		challenge, err := server.accounts.ConsumeOAuthChallenge(request.Context(), state, cookie.Value)
		clearOAuthCookie(writer)
		if err != nil || challenge.Provider != provider || request.URL.Query().Get("error") != "" {
			server.redirectOAuthError(writer, request)
			return
		}
		code := request.URL.Query().Get("code")
		if len(code) < 8 || len(code) > 2048 || strings.ContainsAny(code, "\r\n") {
			server.redirectOAuthError(writer, request)
			return
		}
		identity, err := server.oauth.exchangeIdentity(request.Context(), provider, code, challenge)
		if err != nil {
			server.logger.Warn("OAuth exchange failed", "provider", provider, "error", err)
			server.redirectOAuthError(writer, request)
			return
		}
		var account accounts.Account
		if challenge.AccountID != "" {
			session, ok := server.requireSession(writer, request)
			if !ok || session.Session.AccountID != challenge.AccountID {
				return
			}
			if _, err := server.accounts.LinkIdentity(request.Context(), challenge.AccountID, identity); err != nil {
				server.redirectOAuthError(writer, request)
				return
			}
			account, err = server.accounts.GetAccount(request.Context(), challenge.AccountID)
		} else {
			account, _, err = server.accounts.LoginIdentity(request.Context(), identity)
		}
		if err != nil {
			server.redirectOAuthError(writer, request)
			return
		}
		server.ensureMonthlyFreeCredits(request.Context(), account.ID)
		session, token, _, err := server.accounts.CreateSession(request.Context(), account.ID, server.oauth.ttl)
		if err != nil {
			server.redirectOAuthError(writer, request)
			return
		}
		setSessionCookie(writer, token, session.ExpiresAt)
		http.Redirect(writer, request, server.origin+challenge.ReturnPath, http.StatusSeeOther)
	}
}

func (service *OAuthService) exchangeIdentity(ctx context.Context, provider, code string, challenge accounts.OAuthChallenge) (accounts.IdentityInput, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, service.client)
	var config *oauth2.Config
	switch provider {
	case "google":
		config = service.google
	case "github":
		config = service.github
	default:
		return accounts.IdentityInput{}, errors.New("unsupported OAuth provider")
	}
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(challenge.CodeVerifier))
	if err != nil {
		return accounts.IdentityInput{}, fmt.Errorf("token exchange: %w", err)
	}
	if provider == "google" {
		return service.googleIdentity(ctx, token, challenge.Nonce)
	}
	return service.githubIdentity(ctx, token)
}

func (service *OAuthService) googleIdentity(ctx context.Context, token *oauth2.Token, nonce string) (accounts.IdentityInput, error) {
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return accounts.IdentityInput{}, errors.New("Google ID token is missing")
	}
	verifier, err := service.getGoogleVerifier(ctx)
	if err != nil {
		return accounts.IdentityInput{}, err
	}
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return accounts.IdentityInput{}, fmt.Errorf("verify Google ID token: %w", err)
	}
	var claims struct {
		Subject       string `json:"sub"`
		Nonce         string `json:"nonce"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" || claims.Nonce != nonce || claims.Email == "" || !claims.EmailVerified {
		return accounts.IdentityInput{}, errors.New("Google identity claims are invalid")
	}
	return accounts.IdentityInput{Provider: "google", Subject: claims.Subject, Email: claims.Email, DisplayName: claims.Name, AvatarURL: claims.Picture}, nil
}

func (service *OAuthService) getGoogleVerifier(ctx context.Context) (googleTokenVerifier, error) {
	service.verifierMu.Lock()
	defer service.verifierMu.Unlock()
	if service.googleVerifier != nil {
		return service.googleVerifier, nil
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, service.client)
	provider, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, fmt.Errorf("discover Google OIDC: %w", err)
	}
	service.googleVerifier = provider.Verifier(&oidc.Config{ClientID: service.google.ClientID})
	return service.googleVerifier, nil
}

func (service *OAuthService) githubIdentity(ctx context.Context, token *oauth2.Token) (accounts.IdentityInput, error) {
	client := service.github.Client(context.WithValue(ctx, oauth2.HTTPClient, service.client), token)
	var profile struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getOAuthJSON(ctx, client, githubUserURL, &profile); err != nil || profile.ID <= 0 || profile.Login == "" {
		if err == nil {
			err = errors.New("GitHub profile is invalid")
		}
		return accounts.IdentityInput{}, err
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getOAuthJSON(ctx, client, githubUserEmailURL, &emails); err != nil {
		return accounts.IdentityInput{}, fmt.Errorf("load verified GitHub email: %w", err)
	}
	profile.Email = ""
	for _, candidate := range emails {
		if candidate.Primary && candidate.Verified && strings.TrimSpace(candidate.Email) != "" {
			profile.Email = candidate.Email
			break
		}
	}
	if profile.Email == "" {
		for _, candidate := range emails {
			if candidate.Verified && strings.TrimSpace(candidate.Email) != "" {
				profile.Email = candidate.Email
				break
			}
		}
	}
	if profile.Email == "" {
		return accounts.IdentityInput{}, errors.New("GitHub account has no verified email")
	}
	displayName := profile.Name
	if displayName == "" {
		displayName = profile.Login
	}
	return accounts.IdentityInput{Provider: "github", Subject: strconv.FormatInt(profile.ID, 10), Email: profile.Email, DisplayName: displayName, AvatarURL: profile.AvatarURL}, nil
}

func getOAuthJSON(ctx context.Context, client *http.Client, endpoint string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maximumOAuthBody))
		return fmt.Errorf("OAuth profile status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumOAuthBody+1))
	if err != nil {
		return err
	}
	if len(body) > maximumOAuthBody {
		return errors.New("OAuth profile response is too large")
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return err
	}
	return nil
}

func clearOAuthCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{Name: OAuthCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (server *Server) redirectOAuthError(writer http.ResponseWriter, request *http.Request) {
	http.Redirect(writer, request, server.origin+"/login?error=oauth_failed", http.StatusSeeOther)
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
