package publicapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/eason4kim-rocket/annolux/accounts"
)

func (server *Server) handlePlaygroundSearch(writer http.ResponseWriter, request *http.Request) {
	session, ok := server.requireWriteSession(writer, request)
	if !ok {
		return
	}
	publicRequest, code, message, ok := decodePublicSearchRequest(writer, request)
	if !ok {
		writeSearchError(writer, http.StatusBadRequest, code, message)
		return
	}
	if !server.limiter.Allow(session.Session.AccountID) {
		writeSearchError(writer, http.StatusTooManyRequests, "RATE_LIMITED", "rate limit exceeded")
		return
	}
	select {
	case server.gate <- struct{}{}:
		defer func() { <-server.gate }()
	default:
		writeSearchError(writer, http.StatusTooManyRequests, "CAPACITY_LIMITED", "search capacity is temporarily full")
		return
	}
	requestID := writer.Header().Get(RequestIDHeader)
	server.ensureMonthlyFreeCredits(request.Context(), session.Session.AccountID)
	reservation, _, err := server.accounts.ReserveSessionCredits(request.Context(), session.Session.AccountID, requestID, 1)
	if errors.Is(err, accounts.ErrInsufficientCredits) {
		writeSearchError(writer, http.StatusPaymentRequired, "INSUFFICIENT_CREDITS", "credit balance is empty")
		return
	}
	if err != nil {
		writeSearchError(writer, http.StatusServiceUnavailable, "BILLING_UNAVAILABLE", "credit ledger is unavailable")
		return
	}
	committed := false
	defer func() {
		if !committed {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = server.accounts.ReleaseReservation(ctx, reservation.ID)
		}
	}()
	committed = server.finishSearch(writer, request, "playground", session.Session.AccountID, requestID, publicRequest,
		func(settleContext context.Context) (int64, error) {
			return server.accounts.CommitReservation(settleContext, reservation.ID)
		})
}
