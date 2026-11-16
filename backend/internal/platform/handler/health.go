package handler

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

type healthResponse struct {
	Status string `json:"status" example:"ok"`
}

// Health returns service health status.
//
// It is the shallow check Cloud Run's startup and liveness probes ask: it
// answers while the process serves and asks nothing else, so a database
// outage never makes Cloud Run restart the API. DeepHealth is the one that
// asks the database.
//
// @Summary      Health check
// @Description  Returns service health status.
// @Tags         public
// @Produce      json
// @Success      200  {object}  healthResponse
// @Router       /health [get]
func Health(w http.ResponseWriter, _ *http.Request) {
	if err := platform.WriteJSON(w, http.StatusOK, healthResponse{Status: "ok"}); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// deepHealthEnvelope documents DeepHealth's 200.
type deepHealthEnvelope struct {
	Data      healthResponse     `json:"data"`
	Error     *platform.APIError `json:"error"`
	RequestID string             `json:"request_id"`
}

// errDatabaseUnreachable is DeepHealth's answer when the database did not
// answer within platform.DeepHealthTimeout.
var errDatabaseUnreachable = apperror.New("DATABASE_UNREACHABLE", "The database did not answer", nil)

// DeepHealth answers whether the API can reach its database (#775, ADR 0079):
// the Storefront's deep health route asks it, and that route is the uptime
// probe's target.
//
// The check is one ping on the pool, bounded by platform.DeepHealthTimeout:
// the connection it borrows is handed back, or abandoned, when the ping
// returns, so a hung database holds nothing past the bound. A database that
// does not answer is a 503 DATABASE_UNREACHABLE with a Retry-After, which the
// request pipeline logs at WARN and never reports: an outage raises the uptime
// alert, not one issue per probe. Never traced (server.neverTraced) and never
// cached.
//
// @Summary      Deep health check
// @Description  Answers 200 when the API can reach its database, and 503 DATABASE_UNREACHABLE when the database did not answer within a short bound. The Storefront's deep health route asks it for the uptime probe; it carries no data.
// @Tags         public
// @Produce      json
// @Success      200  {object}  deepHealthEnvelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/public/health [get]
func DeepHealth(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := platform.RequestID(r.Context())
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), platform.DeepHealthTimeout)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			_ = platform.WriteDomainError(w, requestID, errDatabaseUnreachable)
			return
		}
		_ = platform.WriteSuccess(w, requestID, http.StatusOK, healthResponse{Status: "ok"})
	}
}
