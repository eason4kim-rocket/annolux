package publicapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
)

type sessionContext struct {
	Session accounts.Session
	Token   string
	CSRF    string
}

func (server *Server) requireSession(writer http.ResponseWriter, request *http.Request) (sessionContext, bool) {
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		writeError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "login is required")
		return sessionContext{}, false
	}
	session, csrf, err := server.accounts.AuthenticateSession(request.Context(), cookie.Value)
	if errors.Is(err, accounts.ErrSuspended) {
		writeError(writer, http.StatusForbidden, "ACCOUNT_SUSPENDED", "account access is suspended")
		return sessionContext{}, false
	}
	if err != nil {
		clearSessionCookie(writer)
		writeError(writer, http.StatusUnauthorized, "UNAUTHORIZED", "login is required")
		return sessionContext{}, false
	}
	return sessionContext{Session: session, Token: cookie.Value, CSRF: csrf}, true
}

func (server *Server) requireWriteSession(writer http.ResponseWriter, request *http.Request) (sessionContext, bool) {
	if request.Header.Get("Origin") != server.origin {
		writeError(writer, http.StatusForbidden, "ORIGIN_DENIED", "origin is not allowed")
		return sessionContext{}, false
	}
	session, ok := server.requireSession(writer, request)
	if !ok {
		return sessionContext{}, false
	}
	if !server.accounts.ValidateCSRF(session.Token, request.Header.Get("X-CSRF-Token")) {
		writeError(writer, http.StatusForbidden, "CSRF_FAILED", "CSRF validation failed")
		return sessionContext{}, false
	}
	return session, true
}

func (server *Server) handleAccountMe(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(writer, request)
	if !ok {
		return
	}
	account, err := server.accounts.GetAccount(request.Context(), session.Session.AccountID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "account is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"id": account.ID, "status": account.Status, "created_at": account.CreatedAt,
		"terms_version": account.TermsVersion, "refund_version": account.RefundVersion,
		"csrf_token": session.CSRF,
	})
}

func (server *Server) handleAccountUsage(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(writer, request)
	if !ok {
		return
	}
	server.ensureMonthlyFreeCredits(request.Context(), session.Session.AccountID)
	summary, err := server.accounts.CreditSummary(request.Context(), session.Session.AccountID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "usage is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, summary)
}

func (server *Server) handleAPIKeys(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet {
		session, ok := server.requireSession(writer, request)
		if !ok {
			return
		}
		keys, err := server.accounts.ListAPIKeys(request.Context(), session.Session.AccountID)
		if err != nil {
			writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "API keys are unavailable")
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"api_keys": keys})
		return
	}
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		Name string `json:"name"`
	}
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "INVALID_INPUT", "invalid API key request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(writer, http.StatusBadRequest, "INVALID_INPUT", "invalid API key request")
		return
	}
	key, plaintext, err := server.accounts.CreateAPIKey(request.Context(), session.Session.AccountID, input.Name)
	if errors.Is(err, accounts.ErrMaximumAPIKeys) {
		writeError(writer, http.StatusConflict, "API_KEY_LIMIT", "an account may have at most three active API keys")
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadRequest, "INVALID_INPUT", "API key could not be created")
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"api_key": key, "secret": plaintext, "shown_once": true})
}

func (server *Server) handleAPIKeyDelete(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	keyID := strings.TrimSpace(request.PathValue("keyID"))
	if err := server.accounts.RevokeAPIKey(request.Context(), session.Session.AccountID, keyID); errors.Is(err, accounts.ErrNotFound) {
		writeError(writer, http.StatusNotFound, "NOT_FOUND", "API key was not found")
		return
	} else if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "API key could not be revoked")
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (server *Server) handleIdentities(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireSession(writer, request)
	if !ok {
		return
	}
	identities, err := server.accounts.ListIdentities(request.Context(), session.Session.AccountID)
	if err != nil {
		writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "identities are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"identities": identities})
}

func (server *Server) handleLogout(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	if err := server.accounts.RevokeSession(request.Context(), session.Token); err != nil && !errors.Is(err, accounts.ErrNotFound) {
		writeError(writer, http.StatusServiceUnavailable, "ACCOUNT_UNAVAILABLE", "logout failed")
		return
	}
	clearSessionCookie(writer)
	writer.WriteHeader(http.StatusNoContent)
}

func setSessionCookie(writer http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(writer, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/", Expires: expiresAt,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(writer http.ResponseWriter) {
	http.SetCookie(writer, &http.Cookie{
		Name: SessionCookieName, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}
