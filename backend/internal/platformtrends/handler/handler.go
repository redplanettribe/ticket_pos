// Package handler exposes Platform Trends over HTTP (#808).
//
// Its routes sit on the operator namespace behind the operator gate, a Staff
// Session whose email is on the Platform Operator allowlist and nothing else.
// No handler here takes an Active Member: operator authority is orthogonal to
// Membership (ADR 0015).
package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

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

	query, fields := parseSpanQuery(r.URL.Query())
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	span, ok := h.svc.ResolveSpan(query)
	if !ok {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeStartAfterEnd,
			Message: "must be before today: a span ends at the close of yesterday",
		}})
		return
	}

	trends, err := h.svc.Trends(r.Context(), span)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, trends)
}

// parseSpanQuery checks the span's shape: a known preset (30d when absent)
// and, for a custom span, two well-formed days with the first not after the
// last, covering at most service.MaxCustomSpanDays. Every field error is
// returned at once, and an inverted or overlong range only when both days
// parsed. Where the span falls against today is the service's to resolve.
func parseSpanQuery(values url.Values) (service.SpanQuery, []platform.FieldError) {
	preset := service.SpanPreset(strings.TrimSpace(values.Get("span")))
	if preset == "" {
		return service.SpanQuery{}, nil
	}
	if !slices.Contains(service.Presets, preset) {
		names := make([]string, 0, len(service.Presets))
		for _, p := range service.Presets {
			names = append(names, string(p))
		}
		return service.SpanQuery{}, []platform.FieldError{{
			Field:   "span",
			Code:    platform.CodeInvalidEnum,
			Message: "must be one of " + strings.Join(names, ", "),
		}}
	}
	if preset != service.PresetCustom {
		return service.SpanQuery{Preset: preset}, nil
	}

	var fields []platform.FieldError
	from, fromOK := parseDay("from", values.Get("from"), &fields)
	to, toOK := parseDay("to", values.Get("to"), &fields)
	if !fromOK || !toOK {
		return service.SpanQuery{}, fields
	}
	if from.After(to) {
		return service.SpanQuery{}, []platform.FieldError{{Field: "from", Code: platform.CodeStartAfterEnd, Message: "must be on or before to"}}
	}
	if to.Sub(from) >= service.MaxCustomSpanDays*24*time.Hour {
		return service.SpanQuery{}, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeTooManyItems,
			Message: fmt.Sprintf("the range must cover at most %d days", service.MaxCustomSpanDays),
		}}
	}
	return service.SpanQuery{Preset: preset, From: from, To: to}, nil
}

// parseDay reads one end of a custom range, appending its refusal to fields.
func parseDay(field, raw string, fields *[]platform.FieldError) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeRequired, Message: "is required for a custom span"})
		return time.Time{}, false
	}
	day, err := time.Parse(service.DayFormat, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	return day, true
}
