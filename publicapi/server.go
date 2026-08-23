// Package publicapi exposes the intentionally small Annolux self-serve API.
// Legacy scraping and enrichment transports are not registered here.
package publicapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
	"github.com/eason4kim-rocket/annolux/models"
	"github.com/eason4kim-rocket/annolux/querylog"
)

const (
	SessionCookieName = "__Host-annolux_session"
	OAuthCookieName   = "__Host-annolux_oauth"
	RequestIDHeader   = "X-Request-ID"
	CreditsUsedHeader = "X-Annolux-Credits-Used"
	CreditsLeftHeader = "X-Annolux-Credits-Remaining"
)

type SearchService interface {
	Search(context.Context, *models.SearchRequest) (*models.SearchResponse, error)
}

type Config struct {
	Origin        string
	Accounts      *accounts.Store
	Search        SearchService
	Ready         func(context.Context) error
	Logger        *slog.Logger
	MaxConcurrent int
	// DemoDailyBudget caps global demo searches per UTC day. Zero or a
	// negative value selects the built-in default; launch-day traffic is
	// tuned through ANNOLUX_DEMO_DAILY_BUDGET without a rebuild.
	DemoDailyBudget int
	Version         string
	OAuth           OAuthConfig
	Billing         BillingConfig
	// QueryLog receives one record per observed Search request when set.
	// A nil logger disables query logging entirely; it never gates startup.
	QueryLog *querylog.Logger
}

type Server struct {
	origin   string
	accounts *accounts.Store
	search   SearchService
	ready    func(context.Context) error
	logger   *slog.Logger
	version  string
	limiter  *keyLimiters
	gate     chan struct{}
	// demoLimiter and demoQuota bound the unauthenticated homepage demo:
	// per-IP pacing plus a global daily ceiling, independent of account limits.
	demoLimiter *keyLimiters
	demoQuota   *dailyQuota
	oauth       *OAuthService
	billing     *BillingService
	querylog    *querylog.Logger
	handler     http.Handler
}

func New(config Config) (*Server, error) {
	origin := strings.TrimRight(strings.TrimSpace(config.Origin), "/")
	if origin != "https://annolux.com" && origin != "http://127.0.0.1" && !strings.HasPrefix(origin, "http://127.0.0.1:") {
		return nil, errors.New("publicapi: invalid origin")
	}
	if config.Accounts == nil || config.Search == nil {
		return nil, errors.New("publicapi: missing required dependency")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 12
	}
	demoBudget := config.DemoDailyBudget
	if demoBudget <= 0 {
		demoBudget = demoDailyBudget
	}
	server := &Server{
		origin: origin, accounts: config.Accounts, search: config.Search, ready: config.Ready,
		logger: config.Logger, version: config.Version, limiter: newKeyLimiters(5, 10),
		gate: make(chan struct{}, config.MaxConcurrent), querylog: config.QueryLog,
		demoLimiter: newKeyLimiters(5.0/60.0, 5), demoQuota: newDailyQuota(demoBudget),
	}
	if config.OAuth.Enabled() {
		oauth, err := NewOAuthService(config.OAuth, config.Accounts)
		if err != nil {
			return nil, err
		}
		server.oauth = oauth
	}
	if config.Billing.Enabled() {
		billing, err := NewBillingService(config.Billing, config.Accounts)
		if err != nil {
			return nil, err
		}
		server.billing = billing
	}
	server.handler = server.routes()
	return server, nil
}

func (server *Server) Handler() http.Handler { return server.handler }

type routeDefinition struct {
	method  string
	path    string
	handler http.HandlerFunc
}

func (server *Server) routeDefinitions() []routeDefinition {
	return []routeDefinition{
		{http.MethodGet, "/api/v1/health", server.handleHealth},
		{http.MethodGet, "/api/v1/ready", server.handleReady},
		{http.MethodPost, "/api/v1/search", server.handleSearch},
		{http.MethodPost, "/api/v1/demo/search", server.handleDemoSearch},
		{http.MethodGet, "/api/v1/account/me", server.handleAccountMe},
		{http.MethodGet, "/api/v1/account/usage", server.handleAccountUsage},
		{http.MethodGet, "/api/v1/account/api-keys", server.handleAPIKeys},
		{http.MethodPost, "/api/v1/account/api-keys", server.handleAPIKeys},
		{http.MethodDelete, "/api/v1/account/api-keys/{keyID}", server.handleAPIKeyDelete},
		{http.MethodGet, "/api/v1/account/identities", server.handleIdentities},
		{http.MethodPost, "/api/v1/auth/logout", server.handleLogout},
		{http.MethodGet, "/api/v1/auth/google/start", server.handleOAuthStart("google")},
		{http.MethodGet, "/api/v1/auth/google/callback", server.handleOAuthCallback("google")},
		{http.MethodGet, "/api/v1/auth/github/start", server.handleOAuthStart("github")},
		{http.MethodGet, "/api/v1/auth/github/callback", server.handleOAuthCallback("github")},
		{http.MethodPost, "/api/v1/account/identities/google/start", server.handleOAuthLinkStart("google")},
		{http.MethodPost, "/api/v1/account/identities/github/start", server.handleOAuthLinkStart("github")},
		{http.MethodPost, "/api/v1/playground/search", server.handlePlaygroundSearch},
		{http.MethodPost, "/api/v1/billing/checkout-intents", server.handleCheckoutIntent},
		{http.MethodPost, "/api/v1/billing/portal-sessions", server.handlePortalSession},
		{http.MethodPost, "/api/v1/webhooks/paddle", server.handlePaddleWebhook},
	}
}

func (server *Server) routes() http.Handler {
	mux := http.NewServeMux()
	for _, route := range server.routeDefinitions() {
		mux.HandleFunc(route.method+" "+route.path, route.handler)
	}
	return server.middleware(mux)
}

func (server *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// Request IDs are server-owned. Accepting a caller-supplied value here
		// would let a client reuse a committed credit reservation.
		requestID := newRequestID()
		writer.Header().Set(RequestIDHeader, requestID)
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if request.Header.Get("Origin") == server.origin {
			writer.Header().Set("Access-Control-Allow-Origin", server.origin)
			writer.Header().Set("Vary", "Origin")
			writer.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		if request.Method == http.MethodOptions {
			server.handlePreflight(writer, request)
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				server.logger.Error("request panic", "request_id", requestID)
				writeError(writer, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) handlePreflight(writer http.ResponseWriter, request *http.Request) {
	if request.Header.Get("Origin") != server.origin {
		writeError(writer, http.StatusForbidden, "ORIGIN_DENIED", "origin is not allowed")
		return
	}
	method := request.Header.Get("Access-Control-Request-Method")
	if method != http.MethodGet && method != http.MethodPost && method != http.MethodDelete {
		writeError(writer, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not allowed")
		return
	}
	writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
	writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-CSRF-Token, X-Request-ID")
	writer.Header().Set("Access-Control-Max-Age", "600")
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"status": "healthy", "version": server.version})
}

func (server *Server) handleReady(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := server.accounts.Ping(ctx); err != nil {
		writeError(writer, http.StatusServiceUnavailable, "NOT_READY", "account store is unavailable")
		return
	}
	if server.ready != nil {
		if err := server.ready(ctx); err != nil {
			writeError(writer, http.StatusServiceUnavailable, "NOT_READY", "search index is unavailable")
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"status": "ready", "version": server.version})
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))[:32]
	}
	return "req_" + hex.EncodeToString(value[:])
}
