// Package handler exposes Home over HTTP (#858, ADR 0081).
//
// Its routes sit on the staff namespace and are open to any Member of the
// Active Member's Organization, Event Staff included; what a role may not see
// is withheld by the service from the response, never hidden by the page.
package handler

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/organizationtrends/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/trendspan"
)

// Handler exposes Home's endpoints.
type Handler struct {
	svc *service.Service
}

// New returns a Home HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// readerFromRequest is the Active Member reading Home.
func readerFromRequest(r *http.Request) service.Reader {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.Reader{OrganizationID: member.OrganizationID, Role: member.Role}
}

// GetHomeNow returns what stands now across the Organization.
//
// @Summary      Get what stands now on the Organization's Home
// @Description  Returns the top of the Active Member's Organization's Home, the whole Organization whichever Member reads it. active_events counts the Active Events, those neither cancelled nor over (the end instant passed, or the start where there is no end), drafts included; has_events is false for an Organization with no Event in any status. next_event is the published, non-cancelled Event with the earliest start that is not over, so an Ongoing Event (started, end not reached) is chosen over a later one and ongoing says so; a draft is never next. It carries the Event's Tickets Sold so far on every Sales Channel, reversed sales left out, and is null when there is no such Event. today_so_far is today's sales on the platform clock (America/Guayaquil), by the instant each sale was made, so a Sale Import backdated to today counts and one backdated to another day does not; reversed Ticket Sales are left out, as Sales Trends leaves them out. It is always partial. takings_cents is the Takings of those sales, as Sales Trends computes it, in the Organization's currency: null for Event Staff, never zero in its place. payable_balance_cents is the Payable Balance exactly as the Payouts page states it, null for anyone but an Org Admin. Any Member.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeHomeNow
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/home/now [get]
func (h *Handler) GetHomeNow(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	now, err := h.svc.Now(r.Context(), readerFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, now)
}

// GetOrganizationTrends returns Organization Trends for a span.
//
// @Summary      Get Organization Trends for a span
// @Description  Returns Organization Trends for the Active Member's Organization, the whole Organization whichever Member reads it, over a span of whole days on the platform clock (America/Guayaquil), never an Event's timezone. span is 7d, 30d, 90d, 12m, all or custom, defaulting to 30d; a custom span takes from and to (YYYY-MM-DD, both included, at most 3660 days), which the other spans ignore. Every span ends at the close of yesterday: a custom range reaching today or later is cut at yesterday, and one with no day before today is refused. Buckets are days for a span of up to 90 days, and calendar months beyond it and for all time, as granularity states; a month the span cuts is flagged partial, on span.buckets and on every series bucket. 12m runs from the first of the month eleven months before the current one through yesterday, so its last month is cut at yesterday. all starts on the first of the month holding the earlier of the Organization's creation and its earliest Ticket Sale's sale day, and carries no comparison: comparison is null, every comparison_total and change is null and every comparison_buckets is empty, whatever compare says. compare is previous (the default), the span just before (as many days, or for months as many months, keeping the cuts), or year, the same span a year earlier (52 weeks back for days, so a day sits beside the same weekday, and 12 calendar months back for months). comparison_buckets has one entry per bucket at the same index. Today is returned on each series as today, a separate bucket flagged partial, whenever the span ends yesterday, and is in no total and no comparison. A sale counts on the platform day of the instant it was made, not the day it was recorded, so a backdated Sale Import lands in its own past day; reversed Ticket Sales are left out, live, as Sales Trends leaves them out. tickets_sold is the quantity over active Ticket Sales, one series per Sales Channel (online, in_person, import), each always present; ticket_sales is how many active Ticket Sales were made, one series keyed all. takings is the Takings of the active Ticket Sales in the Organization's minor units, computed exactly as Sales Trends computes it (every Sales Channel, no channel filter), one series per Sales Channel like tickets_sold; it is null for Event Staff, never zero in its place. A bucket nothing sold in is zero, never absent. Each figure's total is its series' summed. change is a signed ratio (0.5 for half as much again), null when the comparison total is zero or there is none. currency is the Organization's. Any Member.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        span     query  string  false  "Span"  Enums(7d, 30d, 90d, 12m, all, custom)  default(30d)
// @Param        from     query  string  false  "First day of a custom span (YYYY-MM-DD)"
// @Param        to       query  string  false  "Last day of a custom span (YYYY-MM-DD)"
// @Param        compare  query  string  false  "Comparison span, ignored for all"  Enums(previous, year)  default(previous)
// @Success      200  {object}  openapi.EnvelopeOrganizationTrends
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/trends [get]
func (h *Handler) GetOrganizationTrends(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	query, fields := parseSpanQuery(r.URL.Query())
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	trends, ok, err := h.svc.Trends(r.Context(), readerFromRequest(r), query)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	if !ok {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeStartAfterEnd,
			Message: "must be before today: a span ends at the close of yesterday",
		}})
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, trends)
}

// presets is every span a caller may name, in the order the page offers them.
var presets = []trendspan.Preset{
	trendspan.Preset7Days, trendspan.Preset30Days, trendspan.Preset90Days,
	trendspan.Preset12Months, trendspan.PresetAll, trendspan.PresetCustom,
}

// comparisons is every comparison a caller may name, the default first.
var comparisons = []service.OrganizationTrendComparisonKind{service.ComparePrevious, service.CompareYear}

// parseSpanQuery checks the span's shape as Platform Trends' does: a known
// comparison (previous when absent), a known preset (30d when absent) and,
// for a custom span, the range trendspan.ParseCustomRange accepts. Where the
// span falls against today is the service's to resolve.
func parseSpanQuery(values url.Values) (service.TrendSpanQuery, []platform.FieldError) {
	compare := service.OrganizationTrendComparisonKind(strings.TrimSpace(values.Get("compare")))
	if compare != "" && !slices.Contains(comparisons, compare) {
		return service.TrendSpanQuery{}, []platform.FieldError{trendspan.InvalidEnum("compare", comparisons)}
	}
	preset := trendspan.Preset(strings.TrimSpace(values.Get("span")))
	if preset == "" {
		return service.TrendSpanQuery{Compare: compare}, nil
	}
	if !slices.Contains(presets, preset) {
		return service.TrendSpanQuery{}, []platform.FieldError{trendspan.InvalidEnum("span", presets)}
	}
	if preset != trendspan.PresetCustom {
		return service.TrendSpanQuery{Preset: preset, Compare: compare}, nil
	}

	from, to, fields := trendspan.ParseCustomRange(values.Get("from"), values.Get("to"))
	if len(fields) > 0 {
		return service.TrendSpanQuery{}, fields
	}
	return service.TrendSpanQuery{Preset: preset, From: from, To: to, Compare: compare}, nil
}
