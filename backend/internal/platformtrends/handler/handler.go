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
// @Description  Returns the platform-wide figures of Platform Trends, House Organizations included, over a span of whole days on the platform clock (America/Guayaquil), never an Event's timezone. span is 7d, 30d, 90d, 12m (52 whole weeks) or custom, defaulting to 30d; a custom span takes from and to (YYYY-MM-DD, both included), which the other spans ignore. Every span ends at the close of yesterday: a custom range reaching today or later is cut at yesterday, and one with no day before today is refused. Buckets are days for a span of up to 90 days and weeks beyond, as granularity states; a weekly bucket is seven days counted back from the span's last day, so the oldest may be a shorter stub, and span.buckets states the first and last day of every bucket, one entry per entry of every figure's buckets, at the same index. Each figure states data_since, the first day it has data: a bucket of a count figure wholly before it is null, as is every one of a figure with no data at all, since nothing was counted there and that is not zero; a bucket holding it starts on it, and a bucket from it on with nothing in it is zero, never absent. Today is returned on each series as a separate bucket flagged partial, in no total, whenever the span ends yesterday. Each figure states whether it is recorded at all and the day exact recording began (null when always exact). compare is previous (the default), the span of the same length ending the day before the span starts, or year_earlier, the span moved back 52 weeks so every day sits beside the same weekday; comparison states it. Every series carries comparison_buckets, one entry per bucket at the same index and as long as it, its comparison_total and its change; a summable figure carries them across its series too. change is a signed ratio (0.5 for half as much again), null, never zero or infinite, when the comparison total is zero or null. A comparison bucket is stated from data_since on in the same way, so missing history is absent, never zero, and a comparison with no data has a null total. A comparison span that begins before data_since is covered only in part: its buckets from data_since on are still stated, and its comparison_total over them, but change is null on the figure and on every series of it, since the two totals are not of the same number of days. The same holds for the change of an activity figure, whose comparison_mean is still stated. Today has no comparison bucket and is in no total. Tickets Sold is the quantity over active Ticket Sales by the day sold, one series per Sales Channel; its data begins on the day the earliest Ticket Sale, reversed or not, was sold, as the money figures' does. Money sold and Platform Fee earned are stated per currency, one series per currency an Organization is in, keyed by the currency code, every value in that currency's minor units; no currency is converted or added to another, so both figures have a null total and each series carries its own total, comparison_total and change. Money sold is the buyer price times the quantity, before the Platform Fee and Fee IVA are withheld, over active Ticket Sales on every Sales Channel by the day sold. Platform Fee earned is the Platform Fee, excluding Fee IVA, on active Online Sales and on reversed ones whose fee the platform kept, by the day sold; over every day up to today, today's bucket included, it equals the Platform Fee the operator summary states per currency. New Verified Customers, new Organizations and Page Views are each one count in one series keyed all, with a total, comparison_total and change of their own, always exact, their data beginning on the day of the first verification, the first Organization and the first Page View. New Verified Customers counts a Customer on the day they first proved their email, by One-time Passcode, Google Sign-In or an accepted link, never on the day a sale created their record; a Customer never verified counts on no day. New Organizations counts an Organization on the day it was created, House Organizations included. Page Views is the loads of every Event's storefront page: it counts loads and not people, and a load that arrived through an Affiliate Link is counted once. No figure is divided into Page Views; the response carries no rate. Events Published is one count in one series keyed all, like those three: each Event once, on the day it first became published, across every Organization. An Event cancelled afterwards stays counted on that day, a draft never counts, and an Event with External Registration counts like any other. Its exact_since is the day the platform began recording the instant of publication: an Event published before that day is counted on its creation day as a stand-in, and one already cancelled by then is not counted at all. Active Organizations is an activity figure and is shaped apart: an Organization is active on the day a Ticket Sale was recorded for it, by the day the sale reached the platform and not the day it says it was sold, on any Sales Channel, reversed or not, and on a day a Member worked in it through a Staff Session, sale or no sale; one that did both is counted once, and a Platform Operator on the Operator Dashboard marks no Organization. It is read only from the totals the daily close froze, which never change afterwards, so it has no bucket for today, and a bucket the close has frozen no day of is null: absent, never zero. Because a count of different Organizations cannot be added across days, the figure states no total: mean is the mean daily count over the frozen days of the span, a weekly bucket's value is the mean of its frozen days, whose first and last it states, and change is mean against comparison_mean. distinct_7d and distinct_30d are the different Organizations active across the 7 and the 30 days ending on the span's last day, each counted once, null while that day is not frozen. distinct_7d_days and distinct_30d_days state how many of those 7 and 30 days are recorded, frozen with the figure in them, null exactly when their count is and otherwise from 1 to the run's length: a run reaching back before data_since, or for Active Customers holding a day closed while recording was off, is below its length, and its count is of the recorded days alone. Its data_since is the first frozen day. Its exact_since is the first day the daily close froze with a Member's work counted in it: a day frozen before it holds the Organizations a Ticket Sale was recorded for and nothing else, and stays so. sale_half_only is true and exact_since null while no day has been frozen with a Member's work in it. Active Customers is the same shape and read the same way, from frozen totals alone: a Customer is active on a day they acted as themselves, through a Customer Session in any way or by accepting a Ticket Assignment, and is counted once however often they acted; opening a Confirmation Link does not count, since it may have been forwarded, and neither does a sale staff recorded for the Customer. Recording it is switched off until the Privacy Policy describes it: while it is off and no day has a figure, recorded is false and every bucket is null, which is not yet recorded and not zero. Once a day has been closed with recording on and a Customer counted, recorded is true, exact_since and data_since both state that day, the day recording began, and no day before it has a bucket; a day closed while recording was off has none either. sale_half_only is always false for it. The figure states counts alone: no field of this response, and no route or parameter anywhere, returns a Customer for a day. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        span  query  string  false  "Span"  Enums(7d, 30d, 90d, 12m, custom)  default(30d)
// @Param        from  query  string  false  "First day of a custom span (YYYY-MM-DD)"
// @Param        to    query  string  false  "Last day of a custom span (YYYY-MM-DD)"
// @Param        compare  query  string  false  "Comparison span"  Enums(previous, year_earlier)  default(previous)
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

// parseSpanQuery checks the span's shape: a known comparison (previous when
// absent), a known preset (30d when absent) and, for a custom span, two
// well-formed days with the first not after the last, covering at most
// service.MaxCustomSpanDays. An unknown comparison or preset is refused
// first; past them every field error is returned at once, and an inverted or
// overlong range only when both days parsed. Where the span falls against
// today is the service's to resolve.
func parseSpanQuery(values url.Values) (service.SpanQuery, []platform.FieldError) {
	compare := service.ComparisonKind(strings.TrimSpace(values.Get("compare")))
	if compare != "" && !slices.Contains(service.Comparisons, compare) {
		return service.SpanQuery{}, []platform.FieldError{invalidEnum("compare", service.Comparisons)}
	}
	preset := service.SpanPreset(strings.TrimSpace(values.Get("span")))
	if preset == "" {
		return service.SpanQuery{Compare: compare}, nil
	}
	if !slices.Contains(service.Presets, preset) {
		return service.SpanQuery{}, []platform.FieldError{invalidEnum("span", service.Presets)}
	}
	if preset != service.PresetCustom {
		return service.SpanQuery{Preset: preset, Compare: compare}, nil
	}

	var fields []platform.FieldError
	from, fromOK := parseDay("from", values.Get("from"), requiredForCustomSpan, &fields)
	to, toOK := parseDay("to", values.Get("to"), requiredForCustomSpan, &fields)
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
	return service.SpanQuery{Preset: preset, From: from, To: to, Compare: compare}, nil
}

// invalidEnum refuses a value that is not one of allowed, naming them.
func invalidEnum[T ~string](field string, allowed []T) platform.FieldError {
	names := make([]string, 0, len(allowed))
	for _, value := range allowed {
		names = append(names, string(value))
	}
	return platform.FieldError{Field: field, Code: platform.CodeInvalidEnum, Message: "must be one of " + strings.Join(names, ", ")}
}

// requiredForCustomSpan is how an absent end of a custom range is refused.
const requiredForCustomSpan = "is required for a custom span"

// parseDay reads one day of a query, appending its refusal to fields: an
// absent one in the words of required, a malformed one as an invalid date.
func parseDay(field, raw, required string, fields *[]platform.FieldError) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeRequired, Message: required})
		return time.Time{}, false
	}
	day, err := time.Parse(service.DayFormat, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	return day, true
}

// GetPlatformTrendBreakdown returns what one bucket of one figure was made of.
//
// @Summary      Get what a bucket of a Platform Trends figure was made of
// @Description  Returns the breakdown of one figure of Platform Trends for one bucket: the Organizations and Events that contributed, ranked, each with the ids the page links on. figure is the figure's name in the Platform Trends response. A bucket is named by its first day (first_day, YYYY-MM-DD on the platform clock, America/Guayaquil) and its length (granularity, day by default, or week, as the Platform Trends response states for the span). A day may be today, which is answered with partial true; a day after today is refused. Today is always named as a day, whatever the span is drawn in, as Platform Trends states it: one day beside the span. A week is the seven days from first_day and must end before today, as every weekly bucket of a span does; the one weekly bucket shorter than seven days, the stub a custom range may begin with, is named with its last_day too, which a day ignores. totals states what Platform Trends states for the bucket and the contributors keyed the same add up to it, except for an Active Organizations bucket that says sale_half_only, as told below: one total keyed all for a count, and for a money figure one per currency an Organization is in, keyed by the currency code, in its minor units, never converted or added together. Every figure is broken down by the definition Platform Trends counts it by. Tickets Sold is the quantity over active Ticket Sales by the day sold, summed across Sales Channels, by Organization and by Event. Money sold and Platform Fee earned are stated by Organization and by Event, each keyed by its Organization's currency and ranked within it; a reversed Ticket Sale is in neither Tickets Sold nor money sold, and a reversed Online Sale whose fee the platform kept stays in Platform Fee earned. Page Views is ranked by Event. Events Published lists the Events first published in the bucket and new Organizations the Organizations created in it, oldest first, each with value 1. New Verified Customers states its total alone: no field of this response, and no parameter of this route, returns or takes a Customer. A contributor with nothing for the figure is left out, and a figure not broken down by Organization or by Event states an empty list there. An Event carries its Organization's id and name: there is no operator page for an Event, so it links to its Organization's. Ranked lists are ordered by key, then largest first, then by name. Active Organizations lists the Organizations active in the bucket, each with its cause: sale when a Ticket Sale was recorded for it, member when a Member worked in it through a Staff Session, both when both. Each counts the days of the bucket it was active on, 1 for a day, and the total keyed all is their sum: the count Platform Trends states for a day, and for a week the Organization-days its mean is taken over. It is read from the Ticket Sales recorded and the Organization markers, the one place a marker is read at request time, so today is answered too. Markers are kept thirteen months and exist from the day the daily close first froze with a Member's work counted: a bucket with a day before either lists, for that day, the Organizations a Ticket Sale was recorded for and no others, and says so with sale_half_only true; an Organization active on such a day by a Member alone is not listed, though the frozen figure of a day whose markers were deleted since counted it, so the total, being the sum of the Organizations listed, can then be less than the figure Platform Trends states. A week holding such a day and later ones says sale_half_only and still reads the markers of the later days. cause is null and sale_half_only false for every other figure. Active Customers is never listed: customer_kinds states the counts the daily close froze, new (verified on the day they were active), returning (every other one) and returning_bought (the part of returning that owns an Online Sale sold that day), the total keyed all being new plus returning, and for a week each is the sum of its days. They are read from the frozen totals alone, so they do not change when a Customer's markers are deleted. A bucket no day of which has an Active Customers figure, closed while Customer recording was off, before it began or not closed yet, is answered with recorded false, no total and customer_kinds null: not recorded, never zero. recorded is true for every other figure. No route, parameter or field of Platform Trends returns a Customer for a day. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        figure       query  string  true   "Figure"  Enums(tickets_sold, money_sold, platform_fee_earned, new_verified_customers, new_organizations, page_views, events_published, active_organizations, active_customers)
// @Param        first_day    query  string  true   "First day of the bucket (YYYY-MM-DD)"
// @Param        granularity  query  string  false  "Length of the bucket"  Enums(day, week)  default(day)
// @Param        last_day     query  string  false  "Last day of a weekly bucket shorter than seven days (YYYY-MM-DD)"
// @Success      200  {object}  openapi.EnvelopePlatformTrendBreakdown
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/platform-trends/breakdown [get]
func (h *Handler) GetPlatformTrendBreakdown(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	figure, query, fields := parseBreakdownQuery(r.URL.Query())
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	bucket, ok := h.svc.ResolveBucket(query)
	if !ok {
		// The refusal names the day that put the bucket past today: the
		// last day when the caller named one, the first day otherwise.
		refusal := platform.FieldError{Field: "first_day", Code: platform.CodeStartAfterEnd, Message: "must not be after today"}
		if query.Granularity == service.GranularityWeek {
			refusal.Message = "a weekly bucket must end before today: a span ends at the close of yesterday"
			if query.LastDay != nil {
				refusal.Field = "last_day"
			}
		}
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{refusal})
		return
	}

	breakdown, err := h.svc.Breakdown(r.Context(), figure, bucket)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, breakdown)
}

// parseBreakdownQuery checks the shape of a breakdown request: a known
// figure, a known granularity (day when absent), a well-formed first day
// and, for a week alone, an optional last day within the seven days from the
// first. Every field error is returned at once. Where the bucket falls
// against today is the service's to resolve.
func parseBreakdownQuery(values url.Values) (service.BreakdownFigure, service.BucketQuery, []platform.FieldError) {
	var fields []platform.FieldError

	figure := service.BreakdownFigure(strings.TrimSpace(values.Get("figure")))
	switch {
	case figure == "":
		fields = append(fields, platform.FieldError{Field: "figure", Code: platform.CodeRequired, Message: "is required"})
	case !slices.Contains(service.BreakdownFigures, figure):
		fields = append(fields, invalidEnum("figure", service.BreakdownFigures))
	}

	granularity := service.PlatformTrendGranularity(strings.TrimSpace(values.Get("granularity")))
	if granularity == "" {
		granularity = service.GranularityDay
	}
	if !slices.Contains(service.Granularities, granularity) {
		fields = append(fields, invalidEnum("granularity", service.Granularities))
	}

	query := service.BucketQuery{Granularity: granularity}
	firstDay, firstOK := parseDay("first_day", values.Get("first_day"), "is required", &fields)
	query.FirstDay = firstDay
	if granularity == service.GranularityWeek && strings.TrimSpace(values.Get("last_day")) != "" {
		lastDay, lastOK := parseDay("last_day", values.Get("last_day"), "is required", &fields)
		switch {
		case !firstOK || !lastOK:
		case lastDay.Before(firstDay):
			fields = append(fields, platform.FieldError{Field: "last_day", Code: platform.CodeEndBeforeStart, Message: "must be on or after first_day"})
		case lastDay.Sub(firstDay) >= service.WeekDays*24*time.Hour:
			fields = append(fields, platform.FieldError{
				Field:   "last_day",
				Code:    platform.CodeTooManyItems,
				Message: fmt.Sprintf("a weekly bucket covers at most %d days", service.WeekDays),
			})
		default:
			query.LastDay = &lastDay
		}
	}
	return figure, query, fields
}
