package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/nglong14/CodeDuel/internal/app"
	accountauth "github.com/nglong14/CodeDuel/internal/auth"
)

type matchSnapshotLoader interface {
	current(context.Context, uuid.UUID) (*matchSnapshot, error)
	byID(context.Context, uuid.UUID, uuid.UUID) (*matchSnapshot, error)
}

type matchResponse struct {
	Match *matchSnapshot `json:"match"`
}

type matchHTTP struct {
	matches  matchSnapshotLoader
	accounts accountService
	secret   string
	logger   interface {
		Warn(string, ...any)
	}
}

func newMatchHTTP(deps *app.Dependencies) *matchHTTP {
	return &matchHTTP{
		matches:  newMatchSnapshotService(deps.Postgres),
		accounts: accountauth.NewService(deps.Postgres),
		secret:   deps.Config.Gateway.JWTSecret,
		logger:   deps.Logger,
	}
}

func (h *matchHTTP) current(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, "")
}

func (h *matchHTTP) byID(w http.ResponseWriter, r *http.Request) {
	h.serve(w, r, r.PathValue("id"))
}

func (h *matchHTTP) serve(w http.ResponseWriter, r *http.Request, rawMatchID string) {
	w.Header().Set("Cache-Control", "no-store")
	principal, err := AuthenticateREST(r.Context(), r, h.secret, h.accounts)
	if err != nil {
		if errors.Is(err, errUnauthorized) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "authentication is required")
			return
		}
		h.logger.Warn("authenticate match request failed", "err", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}

	var snapshot *matchSnapshot
	if rawMatchID == "" {
		snapshot, err = h.matches.current(r.Context(), principal.User.ID)
	} else {
		matchID, parseErr := uuid.Parse(rawMatchID)
		if parseErr != nil || matchID == uuid.Nil {
			writeAPIError(w, http.StatusNotFound, "match_not_found", "match was not found")
			return
		}
		snapshot, err = h.matches.byID(r.Context(), principal.User.ID, matchID)
		if err == nil && snapshot == nil {
			err = errMatchNotFound
		}
	}
	if err != nil {
		if errors.Is(err, errMatchNotFound) {
			writeAPIError(w, http.StatusNotFound, "match_not_found", "match was not found")
			return
		}
		h.logger.Warn("match snapshot failed", "user_id", principal.User.ID, "err", fmt.Errorf("snapshot: %w", err))
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	writeJSON(w, http.StatusOK, matchResponse{Match: snapshot})
}
