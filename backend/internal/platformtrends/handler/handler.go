// Package handler exposes Platform Trends over HTTP (#808).
//
// Its routes sit on the operator namespace behind the operator gate, a Staff
// Session whose email is on the Platform Operator allowlist and nothing else.
// No handler here takes an Active Member: operator authority is orthogonal to
// Membership (ADR 0015).
package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platformtrends/service"
)

// Handler exposes the Platform Trends endpoints.
type Handler struct {
	svc *service.Service
}

// New returns a Platform Trends HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// GetPlatformTrends returns every figure of Platform Trends for a span.
//
// @Summary      Get Platform Trends for a span
// @Description  Returns the platform-wide figures of Platform Trends, House Organizations included, over a span of whole days on the platform clock (America/Guayaquil), never an Event's timezone. span is 7d, 30d, 90d, 12m (52 whole weeks) or custom, defaulting to 30d; a custom span takes from and to (YYYY-MM-DD, both included), which the other spans ignore. Every span ends at the close of yesterday: a custom range reaching today or later is cut at yesterday, and one with no day before today is refused. Buckets are days for a span of up to 90 days and weeks beyond, as granularity states; a weekly bucket is seven days counted back from the span's last day, so the oldest may be a shorter stub, and every bucket states its first and last day. Days with nothing in them are zero, never absent. Today is returned on each series as a separate bucket flagged partial, in no total, whenever the span ends yesterday. Each figure states whether it is recorded at all and the day exact recording began (null when always exact). Tickets Sold is the quantity over active Ticket Sales by the day sold, one series per Sales Channel. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        span  query  string  false  "Span"  Enums(7d, 30d, 90d, 12m, custom)  default(30d)
// @Param        from  query  string  false  "First day of a custom span (YYYY-MM-DD)"
// @Param        to    query  string  false  "Last day of a custom span (YYYY-MM-DD)"
// @Success      200  {object}  openapi.EnvelopePlatformTrends
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/platform-trends [get]
func (h *Handler) GetPlatformTrends(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	query := r.URL.Query()
	span, fields := h.svc.ResolveSpan(service.SpanQuery{
		Preset: service.SpanPreset(strings.TrimSpace(query.Get("span"))),
		From:   strings.TrimSpace(query.Get("from")),
		To:     strings.TrimSpace(query.Get("to")),
	})
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	trends, err := h.svc.Trends(r.Context(), span)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, trends)
}
