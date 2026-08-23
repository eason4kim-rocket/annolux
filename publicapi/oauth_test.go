package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func oauthJSONResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func attachTestOAuth(t *testing.T, fixture *serverFixture, client *http.Client) {
	t.Helper()
	service, err := NewOAuthService(OAuthConfig{
		Google:     OAuthClientConfig{ClientID: "google-client", ClientSecret: "google-secret", RedirectURL: "http://127.0.0.1:8080/api/v1/auth/google/callback"},
		GitHub:     OAuthClientConfig{ClientID: "github-client", ClientSecret: "github-secret", RedirectURL: "http://127.0.0.1:8080/api/v1/auth/github/callback"},
		HTTPClient: client,
	}, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.oauth = service
}

func TestOAuthStartUsesStatePKCEAndMinimalScopes(t *testing.T) {
	fixture := newServerFixture(t)
	attachTestOAuth(t, &fixture, &http.Client{})
	for _, test := range []struct {
		provider string
		path     string
		scopes   []string
	}{
		{provider: "google", path: "/api/v1/auth/google/start?return=/account", scopes: []string{"openid", "email", "profile"}},
		{provider: "github", path: "/api/v1/auth/github/start?return=/account", scopes: []string{"read:user", "user:email"}},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusFound {
			t.Fatalf("%s start = %d %s", test.provider, recorder.Code, recorder.Body.String())
		}
		location, err := url.Parse(recorder.Header().Get("Location"))
		if err != nil || location.Query().Get("state") == "" || location.Query().Get("code_challenge_method") != "S256" || len(location.Query().Get("code_challenge")) != 43 {
			t.Fatalf("%s location = %s, %v", test.provider, location, err)
		}
		for _, scope := range test.scopes {
			if !strings.Contains(" "+location.Query().Get("scope")+" ", " "+scope+" ") {
				t.Fatalf("%s missing scope %q: %s", test.provider, scope, location.Query().Get("scope"))
			}
		}
		cookies := recorder.Result().Cookies()
		if len(cookies) == 0 || cookies[0].Name != OAuthCookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" {
			t.Fatalf("%s OAuth cookie = %#v", test.provider, cookies)
		}
	}
}

func TestOAuthStartRejectsUnapprovedReturnPaths(t *testing.T) {
	fixture := newServerFixture(t)
	attachTestOAuth(t, &fixture, &http.Client{})
	for _, returnPath := range []string{
		"https://evil.example/", "//evil.example/", "/pricing", "/account?next=https://evil.example", "/billing/checkout?plan=scale", "/%2f%2fevil.example",
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start?return="+url.QueryEscape(returnPath), nil)
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("return %q = %d %s", returnPath, recorder.Code, recorder.Body.String())
		}
	}
	for _, returnPath := range []string{"/account", "/zh/account", "/billing/checkout", "/zh/billing/checkout", "/playground", "/zh/playground"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/google/start?return="+url.QueryEscape(returnPath), nil)
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusFound {
			t.Fatalf("return %q = %d %s", returnPath, recorder.Code, recorder.Body.String())
		}
	}
}

func TestOAuthIdentityLinkStartRequiresOriginAndCSRF(t *testing.T) {
	fixture := newServerFixture(t)
	attachTestOAuth(t, &fixture, &http.Client{})
	makeRequest := func(origin, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/account/identities/github/start?return=/account", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.sessionToken})
		recorder := httptest.NewRecorder()
		fixture.server.Handler().ServeHTTP(recorder, request)
		return recorder
	}
	if response := makeRequest("https://evil.example", fixture.csrf); response.Code != http.StatusForbidden {
		t.Fatalf("evil origin = %d %s", response.Code, response.Body.String())
	}
	if response := makeRequest("http://127.0.0.1:8080", "wrong"); response.Code != http.StatusForbidden {
		t.Fatalf("wrong CSRF = %d %s", response.Code, response.Body.String())
	}
	response := makeRequest("http://127.0.0.1:8080", fixture.csrf)
	if response.Code != http.StatusOK {
		t.Fatalf("valid link start = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	location, err := url.Parse(body.AuthorizationURL)
	if err != nil || location.Host != "github.com" || location.Query().Get("state") == "" {
		t.Fatalf("authorization URL = %q, %v", body.AuthorizationURL, err)
	}
	var browserCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == OAuthCookieName {
			browserCookie = cookie
		}
	}
	if browserCookie == nil {
		t.Fatal("link start omitted browser-bound OAuth cookie")
	}
	challenge, err := fixture.store.ConsumeOAuthChallenge(context.Background(), location.Query().Get("state"), browserCookie.Value)
	if err != nil || challenge.AccountID != fixture.account.ID {
		t.Fatalf("link challenge = %#v, %v", challenge, err)
	}
	legacyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/account/identities/github/start", nil)
	legacyRecorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(legacyRecorder, legacyRequest)
	if legacyRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unsafe GET link start = %d", legacyRecorder.Code)
	}
}

func TestGitHubOAuthCallbackCreatesSessionAndCannotReplay(t *testing.T) {
	fixture := newServerFixture(t)
	var mu sync.Mutex
	tokenVerifierSeen := false
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case githubTokenURL:
			_ = request.ParseForm()
			mu.Lock()
			tokenVerifierSeen = len(request.Form.Get("code_verifier")) >= 43 && request.Form.Get("code") == "temporary-code"
			mu.Unlock()
			return oauthJSONResponse(http.StatusOK, `{"access_token":"github-access","token_type":"bearer","scope":"read:user,user:email"}`), nil
		case githubUserURL:
			if request.Header.Get("Authorization") != "Bearer github-access" {
				t.Errorf("authorization = %q", request.Header.Get("Authorization"))
			}
			return oauthJSONResponse(http.StatusOK, `{"id":4242,"login":"annolux-user","name":"Annolux User","email":"same@example.com","avatar_url":"https://avatars.githubusercontent.com/u/4242"}`), nil
		case githubUserEmailURL:
			return oauthJSONResponse(http.StatusOK, `[{"email":"verified@example.com","primary":true,"verified":true}]`), nil
		default:
			t.Fatalf("unexpected OAuth request: %s", request.URL)
			return nil, nil
		}
	})}
	attachTestOAuth(t, &fixture, client)
	startRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/github/start?return=/account", nil)
	startRecorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(startRecorder, startRequest)
	location, _ := url.Parse(startRecorder.Header().Get("Location"))
	state := location.Query().Get("state")
	var browserCookie *http.Cookie
	for _, cookie := range startRecorder.Result().Cookies() {
		if cookie.Name == OAuthCookieName {
			browserCookie = cookie
		}
	}
	if state == "" || browserCookie == nil {
		t.Fatal("OAuth start did not return state and browser cookie")
	}
	callbackPath := "/api/v1/auth/github/callback?state=" + url.QueryEscape(state) + "&code=temporary-code"
	callbackRequest := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	callbackRequest.AddCookie(browserCookie)
	callbackRecorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(callbackRecorder, callbackRequest)
	if callbackRecorder.Code != http.StatusSeeOther || callbackRecorder.Header().Get("Location") != "http://127.0.0.1:8080/account" {
		t.Fatalf("callback = %d location=%q body=%s", callbackRecorder.Code, callbackRecorder.Header().Get("Location"), callbackRecorder.Body.String())
	}
	mu.Lock()
	seen := tokenVerifierSeen
	mu.Unlock()
	if !seen {
		t.Fatal("GitHub token exchange omitted PKCE verifier")
	}
	var sessionToken string
	for _, cookie := range callbackRecorder.Result().Cookies() {
		if cookie.Name == SessionCookieName && cookie.Value != "" {
			sessionToken = cookie.Value
			if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
				t.Fatalf("session cookie = %#v", cookie)
			}
		}
	}
	if sessionToken == "" {
		t.Fatal("callback did not set a session")
	}
	session, _, err := fixture.store.AuthenticateSession(callbackRequest.Context(), sessionToken)
	if err != nil || session.AccountID == fixture.account.ID {
		t.Fatalf("new session = %#v, %v", session, err)
	}
	summary, err := fixture.store.CreditSummary(callbackRequest.Context(), session.AccountID)
	if err != nil || summary.Available != 1_000 || summary.Granted != 1_000 {
		t.Fatalf("signup summary = %#v, %v", summary, err)
	}
	replayRequest := httptest.NewRequest(http.MethodGet, callbackPath, nil)
	replayRequest.AddCookie(browserCookie)
	replayRecorder := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(replayRecorder, replayRequest)
	if replayRecorder.Code != http.StatusSeeOther || replayRecorder.Header().Get("Location") != "http://127.0.0.1:8080/login?error=oauth_failed" {
		t.Fatalf("replay = %d %q", replayRecorder.Code, replayRecorder.Header().Get("Location"))
	}
}

func TestGitHubIdentityRequiresVerifiedEmailAndBoundedJSON(t *testing.T) {
	fixture := newServerFixture(t)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.String() {
		case githubUserURL:
			return oauthJSONResponse(http.StatusOK, `{"id":7,"login":"no-email"}`), nil
		case githubUserEmailURL:
			return oauthJSONResponse(http.StatusOK, `[{"email":"unverified@example.com","primary":true,"verified":false}]`), nil
		default:
			return nil, errors.New("unexpected URL")
		}
	})}
	attachTestOAuth(t, &fixture, client)
	if _, err := fixture.server.oauth.githubIdentity(context.Background(), &oauth2.Token{AccessToken: "token", TokenType: "Bearer"}); err == nil {
		t.Fatal("GitHub identity without a verified email was accepted")
	}

	oversized := strings.Repeat(" ", maximumOAuthBody) + `{"id":1}`
	oversizedClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return oauthJSONResponse(http.StatusOK, oversized), nil
	})}
	var destination map[string]any
	if err := getOAuthJSON(context.Background(), oversizedClient, githubUserURL, &destination); err == nil {
		t.Fatal("oversized OAuth response was accepted")
	}
}
