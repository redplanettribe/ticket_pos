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
// @Description  Returns Organization Trends for the Active Member's Organization, the whole Organization whichever Member reads it, over a span of whole days on the platform clock (America/Guayaquil), never an Event's timezone. span is 7d, 30d, 90d, 12m, all or custom, defaulting to 30d; a custom span takes from and to (YYYY-MM-DD, both included, at most 3660 days), which the other spans ignore. Every span ends at the close of yesterday: a custom range reaching today or later is cut at yesterday, and one with no day before today is refused. Buckets are days for a span of up to 90 days, and calendar months beyond it and for all time, as granularity states; a month the span cuts is flagged partial, on span.buckets and on every series bucket. 12m runs from the first of the month eleven months before the current one through yesterday, so its last month is cut at yesterday. all starts on the first of the month holding the earlier of the Organization's creation and its earliest Ticket Sale's sale day, and carries no comparison: comparison is null, every comparison_total and change is null and every comparison_buckets is empty, whatever compare says. compare is previous (the default), the span just before (as many days, or for months as many months, keeping the cuts), or year, the same span a year earlier (52 weeks back for days, so a day sits beside the same weekday, and 12 calendar months back for months). comparison_buckets has one entry per bucket at the same index. Today is returned on each series as today, a separate bucket flagged partial, whenever the span ends yesterday, and is in no total and no comparison. A sale counts on the platform day of the instant it was made, not the day it was recorded, so a backdated Sale Import lands in its own past day; reversed Ticket Sales are left out, live, as Sales Trends leaves them out. tickets_sold is the quantity over active Ticket Sales, one series per Sales Channel (online, in_person, import), each always present; ticket_sales is how many active Ticket Sales were made, one series keyed all. takings is the Takings of the active Ticket Sales in the Organization's minor units, computed exactly as Sales Trends computes it (every Sales Channel, no channel filter), one series per Sales Channel like tickets_sold; it is null for Event Staff, never zero in its place. page_views is how many times the Organization's Event pages were loaded, every Event's summed, read from the hourly Page View buckets with each hour on the platform day it falls in; a load through an Affiliate Link counts once. It counts loads, not people. events_held is how many published Events started, each on the platform day of its start instant; a draft or a cancelled Event is never held, an External Registration Event is. Both are one series keyed all. A bucket nothing sold, loaded or started in is zero, never absent. Each figure's total is its series' summed. change is a signed ratio (0.5 for half as much again), null when the comparison total is zero or there is none. currency is the Organization's. Any Member.
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

// GetOrganizationTrendBreakdown returns the Events one bucket of one figure
// of Organization Trends was made of.
//
// @Summary      Get the Events a bucket of an Organization Trends figure was made of
// @Description  Returns the breakdown of one figure of Organization Trends for one bucket, for the Active Member's Organization: the Events that contributed, ranked largest first, then by name, each with the id its page is linked on, and the bucket's total, which they add up to and which is what the Organization Trends reading states for the bucket, every Sales Channel summed. figure is the figure's name in the reading: takings, tickets_sold, ticket_sales, page_views or events_held. A bucket is named by its days on the platform clock (America/Guayaquil), as the reading states them: first_day alone (or with an equal last_day) names a day, which may be today, answered with partial true, and never a day after today; first_day and last_day name a month bucket, the whole calendar month or the part of it a span cuts, both in the same month and ending before today, as every month bucket of a reading does. Every figure is read by the day rules of the reading: a sale on the platform day of the instant it was made, a reversed Ticket Sale in no figure, Takings computed exactly as Sales Trends computes it in the Organization's minor units, Page Views from the whole-page hourly buckets with each hour on its platform day, and Events held as the published Events whose start falls in the bucket, each counting one and listed earliest start first. An Event with nothing for the figure is left out, so a bucket nothing happened in has a zero total and no Events. takings is refused to Event Staff with 403: they are handed no money through this route, as the reading hands them none. Any Member.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        figure     query  string  true   "Figure"  Enums(takings, tickets_sold, ticket_sales, page_views, events_held)
// @Param        first_day  query  string  true   "First day of the bucket (YYYY-MM-DD)"
// @Param        last_day   query  string  false  "Last day of a month bucket (YYYY-MM-DD), in the month of first_day"
// @Success      200  {object}  openapi.EnvelopeOrganizationTrendBreakdown
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/trends/breakdown [get]
func (h *Handler) GetOrganizationTrendBreakdown(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	figure, query, fields := parseBreakdownQuery(r.URL.Query())
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	bucket, ok := h.svc.ResolveBucket(query)
	if !ok {
		// The refusal names the day that put the bucket past today.
		refusal := platform.FieldError{Field: "first_day", Code: platform.CodeStartAfterEnd, Message: "must not be after today"}
		if query.LastDay != nil {
			refusal = platform.FieldError{Field: "last_day", Code: platform.CodeStartAfterEnd, Message: "a month bucket must end before today: a span ends at the close of yesterday"}
		}
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{refusal})
		return
	}
	breakdown, err := h.svc.Breakdown(r.Context(), readerFromRequest(r), figure, bucket)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, breakdown)
}

// parseBreakdownQuery checks the shape of a breakdown request: a known
// figure, a well-formed first day and, when given, a last day no earlier than
// it and in the same calendar month. Every field error is returned at once.
// Where the bucket falls against today is the service's to resolve.
func parseBreakdownQuery(values url.Values) (service.OrganizationTrendFigureName, service.BucketQuery, []platform.FieldError) {
	var fields []platform.FieldError

	figure := service.OrganizationTrendFigureName(strings.TrimSpace(values.Get("figure")))
	switch {
	case figure == "":
		fields = append(fields, platform.FieldError{Field: "figure", Code: platform.CodeRequired, Message: "is required"})
	case !slices.Contains(service.OrganizationTrendFigureNames, figure):
		fields = append(fields, trendspan.InvalidEnum("figure", service.OrganizationTrendFigureNames))
	}

	firstDay, firstOK := trendspan.ParseDay("first_day", values.Get("first_day"), "is required", &fields)
	query := service.BucketQuery{FirstDay: firstDay}
	if strings.TrimSpace(values.Get("last_day")) != "" {
		lastDay, lastOK := trendspan.ParseDay("last_day", values.Get("last_day"), "is required", &fields)
		switch {
		case !firstOK || !lastOK:
		case lastDay.Before(firstDay):
			fields = append(fields, platform.FieldError{Field: "last_day", Code: platform.CodeEndBeforeStart, Message: "must be on or after first_day"})
		case lastDay.Year() != firstDay.Year() || lastDay.Month() != firstDay.Month():
			fields = append(fields, platform.FieldError{Field: "last_day", Code: platform.CodeTooManyItems, Message: "a bucket covers at most one calendar month"})
		default:
			query.LastDay = &lastDay
		}
	}
	return figure, query, fields
}
