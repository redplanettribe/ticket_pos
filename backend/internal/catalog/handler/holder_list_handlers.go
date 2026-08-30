package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Paging for the Outstanding Answers list (#313).
//
// The same defaults and the same clamp as the Sales list (ADR-0006), because
// this is the same kind of screen read by the same people, and a list that paged
// fifty here and twenty-five there would be a difference nobody could explain.
const (
	outstandingDefaultPageSize = 50
	outstandingMaxPageSize     = 100
)

// outstandingPageParam floors the page at 1. A hand-edited or stale shared URL
// stays usable rather than erroring: there is nothing a caller could do about a
// `page=0` refusal except send 1, so this sends 1.
func outstandingPageParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// outstandingPageSizeParam defaults to 50 and clamps to [1, 100]. A value above
// the maximum is clamped DOWN rather than refused, so a caller asking for
// everything gets as much as this platform will serve instead of an error.
func outstandingPageSizeParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return outstandingDefaultPageSize
	}
	if n > outstandingMaxPageSize {
		return outstandingMaxPageSize
	}
	return n
}

// outstandingFilterParam reads the Outstanding Answers filter (#333): `true`
// narrows the Holder List to the Tickets that still owe a required Answer, and
// anything else — including absence, and including a hand-mangled value — is
// the whole roster. Lenient for the pagers' reason: there is nothing a caller
// could do about a refusal of `outstanding=yes` except send the roster request
// they were one typo away from.
func outstandingFilterParam(raw string) bool {
	return strings.TrimSpace(raw) == "true"
}

// holderChannels is what a Sales Channel can be, and the whole of it. Stated
// here rather than trusted from the URL because the value reaches an equality
// on `s.channel`: an unlisted word would not be a security problem — it is a
// bound parameter, not a fragment — but it would be a filter that silently
// matches nothing, which reads to an Organizer as "this Event sold nothing".
// Better to hand back the whole roster and let them see it did.
var holderChannels = []string{"online", "in_person", "import"}

// holderChannelParam reads the Sales Channel filter, IGNORING anything that is
// not one of the three. See holderDateParam for the argument; this is the same
// decision on an enum.
func holderChannelParam(raw string) string {
	value := strings.TrimSpace(raw)
	for _, channel := range holderChannels {
		if value == channel {
			return value
		}
	}
	return ""
}

// holderTicketTypeParam reads the Ticket Type filter, ignoring anything that is
// not a well-formed id.
//
// THE UUID CHECK IS NOT PEDANTRY. The column is `uuid NOT NULL`, so a malformed
// value reaching the comparison is an error from Postgres and a 500 on a screen
// — a hand-edited URL turning into a server fault. Dropping it here makes the
// same URL a roster.
//
// A well-formed id belonging to somebody else's Event is NOT checked and does
// not need to be: the query is already scoped to this Organization and this
// Event, so such a filter matches nothing and discloses nothing — it cannot even
// distinguish an id that exists elsewhere from one that exists nowhere.
func holderTicketTypeParam(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := uuid.Parse(value); err != nil {
		return ""
	}
	return value
}

// holderDateParam reads one calendar-date bound ("YYYY-MM-DD"), IGNORING
// anything else.
//
// THIS SURFACE IGNORES AN UNUSABLE FILTER; IT DOES NOT REFUSE ONE — and that is
// a deliberate departure from the Sales list next door, which answers a
// malformed `sold_from` with a VALIDATION_FAILED 400 (see the sales handler's
// validateDate). The argument, ticket by ticket:
//
//   - CONSISTENCY WITH THIS SURFACE BEATS CONSISTENCY WITH THAT ONE. Every
//     parser above is already lenient, and each says why in the same words:
//     there is nothing a caller could do about a refusal of `page=0` or
//     `outstanding=yes` except send the request they were one typo away from. A
//     Holder List that shrugged at four params and 400'd on the fifth would be
//     arbitrary, and the arbitrariness would land on the reader as an error page
//     from a screen that was working a moment ago.
//
//   - ADR 0065 HAS ALREADY RULED THIS WAY ONCE, for a different reason that
//     points the same direction: a filter belonging to a dark feature flag is
//     "ignored, not refused", because a refusal turns a stale bookmark into an
//     error page and forces the client to know a flag ADR 0045 exists to keep it
//     from knowing. #524 generalises that to every unusable filter on this list.
//     A malformed date is the same stale bookmark by another route.
//
//   - THE COST IS REAL AND IS ACCEPTED. An ignored filter shows MORE rows than
//     the reader asked for, and on this particular list the extra rows are
//     attendees' names and addresses. That is the right way round: this is a
//     read, and a roster wider than intended is a screen the reader can see is
//     wrong — the filter bar shows the date field empty — whereas a 400 hides
//     the roster entirely. It would NOT be the right way round on a write, or on
//     the Holder Export, where the file must describe only the filters actually
//     honoured (ADR 0065) precisely so that nobody reads a whole roster
//     believing it is a filtered one.
//
// The date is returned as the STRING it came as. The service resolves it against
// the Event's own timezone, because a day is only a span of time once you know
// where you are standing.
func holderDateParam(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

// ListHolderList returns the Event's Holder List: every Ticket of the Event,
// who is coming on each, and — where the Event asks Ticket Questions — what
// each still owes (#333; the Outstanding Answers read of #313, widened to the
// roster it was always standing on).
//
// ONE ROUTE AND NOT TWO. The Holder List rides on the read #313 built rather
// than on a second staff endpoint, because both walk the Event's Tickets and an
// Organizer asking "who is coming" is looking at the same list as an Organizer
// asking "who has not told me their size". What #333 changed is what a row IS —
// every Ticket of every live sale — and that Outstanding Answers became the
// `outstanding` FILTER on it rather than the list's definition; #519 then moved
// the path to `/holder-list` so the address says the same thing (ADR 0065). The
// old `/outstanding-answers` path is aliased to this handler for one release
// against deploy skew and is deleted by #531; it is not documented below,
// because a generated client must not learn a path that is about to go.
//
// A READ AND NOTHING ELSE. There is no act on this route and there is no state
// behind it: an Outstanding Answer is DERIVED on every read from what the Ticket
// Type asks and what the Ticket has said, so the debts clear by themselves as
// Answers arrive from any of the three routes — Event Staff, the checkout
// capture, or an Answer Link — and a reversed Ticket Sale's Tickets drop out of
// the roster whole.
//
// @Summary      List an event's holder list
// @Description  A page of the Event's Holder List: EVERY Ticket of every live Ticket Sale, oldest sale first — the Organization's answer to "who is coming". Available while EITHER the Ticket Assignment or the Ticket Question feature flag is open, and 404 only when both are dark. Each row carries the Ticket's `assignment_state` — `unassigned`, `assigned` or `accepted` — and, once a Holder has ACCEPTED, that Holder's own name and email address (ADR 0047). Nothing about a Holder is disclosed before acceptance: an address a buyer typed and its owner never accepted is reported as `assigned` and never named. A Ticket whose unaccepted address the retention purge took reads `assigned` with `never_accepted` beside it, derived at read time from the purge marker — somebody was named and never claimed the Ticket, which after the Event has started is a different fact from nobody having been named; no address travels with it, because the address is gone by definition. All assignment fields are ABSENT while the Ticket Assignment flag is off. Where the Ticket Question feature is open, each row also names the required questions it has not answered, in `outstanding` — an empty array is a Ticket that owes nothing, and stays on the list, because the roster is the point and the questions are a column on it. `outstanding=true` filters the list to the Tickets that still owe — Outstanding Answers is a filter of this list, not its definition. Three further filters narrow the roster STRUCTURALLY and compose with it and with each other (#523, ADR 0065): `ticket_type_id` is the VIP roster apart from general admission — a plain equality, because a Ticket belongs to exactly one Ticket Type, unlike a Ticket Sale, which may span several; `channel` is `online`, `in_person` or `import`, so the buyers who came through the door or through an import — and were therefore never asked anything — can be listed on their own; and `sold_from`/`sold_to` are calendar days (`YYYY-MM-DD`) READ IN THE EVENT'S TIMEZONE, inclusive of the whole end day, so "sold in January" means January where the Event is and not where the reader is standing, matching the Sales list. `pagination.total` reflects the FILTERED view, so the page count never promises pages that do not exist. An unusable filter is IGNORED, never refused: a malformed date, an unknown channel or a malformed Ticket Type id leaves that dimension unfiltered, because a stale bookmark should be a wide roster the reader can see is wide, not an error page. There is deliberately NO status filter: a Sale Reversal means the Tickets cease to exist (ADR 0043), so reversed Tickets are not rows being hidden — they are not rows — and no payment-method or source filter either, both being facts about a sale with no roster meaning. `outstanding_count` is the Event's total number of debts across the whole roster, unaffected by the filter, while `pagination.total` counts the Tickets of the current view. Both `outstanding` and `outstanding_count` are absent while the Ticket Question flag is off. Tickets of `in_person` and `import` sales appear beside the `online` ones and start out owing everything, because those buyers were never asked — each row carries its `channel` so that reads as history rather than as loss. Started Events still report the whole roster and its debts, because "who came and who never told us" is what a reader after the fact came to find out. Org Admin and Event Owner only (#521, ADR 0065): one rule for holder data, the same gate the Sales Export carries — its per-Ticket sheet already emits this Event's accepted Holders' names and addresses to an Event Owner. Event Staff are refused; they work the door, and this is the platform's densest concentration of attendee personal data.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id           path      string  true   "Event ID"
// @Param        page         query     int     false  "Page number (default 1)"
// @Param        page_size    query     int     false  "Rows per page (default 50, max 100)"
// @Param        outstanding     query     bool    false  "Only the Tickets that still owe a required Answer"
// @Param        ticket_type_id  query     string  false  "Only the Tickets of this Ticket Type"
// @Param        channel         query     string  false  "Only the Tickets of sales on this Sales Channel (online, in_person, import)"
// @Param        sold_from       query     string  false  "Only the Tickets of sales made on or after this calendar day (YYYY-MM-DD), read in the Event's timezone"
// @Param        sold_to         query     string  false  "Only the Tickets of sales made on or before this calendar day (YYYY-MM-DD), read in the Event's timezone — the whole day is included"
// @Success      200  {object}  openapi.EnvelopeHolderList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/holder-list [get]
func (h *Handler) ListHolderList(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}

	query := r.URL.Query()
	result, err := h.svc.ListHolderList(
		r.Context(), actorFromRequest(r), eventID,
		service.ListHolderListParams{
			Page:         outstandingPageParam(query.Get("page")),
			PageSize:     outstandingPageSizeParam(query.Get("page_size")),
			OwingOnly:    outstandingFilterParam(query.Get("outstanding")),
			TicketTypeID: holderTicketTypeParam(query.Get("ticket_type_id")),
			Channel:      holderChannelParam(query.Get("channel")),
			SoldFrom:     holderDateParam(query.Get("sold_from")),
			SoldTo:       holderDateParam(query.Get("sold_to")),
		},
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
