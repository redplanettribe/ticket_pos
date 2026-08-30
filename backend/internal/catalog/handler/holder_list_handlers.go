package handler

import (
	"net/http"
	"strconv"
	"strings"

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
// @Description  A page of the Event's Holder List: EVERY Ticket of every live Ticket Sale, oldest sale first — the Organization's answer to "who is coming". Available while EITHER the Ticket Assignment or the Ticket Question feature flag is open, and 404 only when both are dark. Each row carries the Ticket's `assignment_state` — `unassigned`, `assigned` or `accepted` — and, once a Holder has ACCEPTED, that Holder's own name and email address (ADR 0047). Nothing about a Holder is disclosed before acceptance: an address a buyer typed and its owner never accepted is reported as `assigned` and never named. A Ticket whose unaccepted address the retention purge took reads `assigned` with `never_accepted` beside it, derived at read time from the purge marker — somebody was named and never claimed the Ticket, which after the Event has started is a different fact from nobody having been named; no address travels with it, because the address is gone by definition. All assignment fields are ABSENT while the Ticket Assignment flag is off. Where the Ticket Question feature is open, each row also names the required questions it has not answered, in `outstanding` — an empty array is a Ticket that owes nothing, and stays on the list, because the roster is the point and the questions are a column on it. `outstanding=true` filters the list to the Tickets that still owe — Outstanding Answers is a filter of this list, not its definition. `outstanding_count` is the Event's total number of debts across the whole roster, unaffected by the filter, while `pagination.total` counts the Tickets of the current view. Both `outstanding` and `outstanding_count` are absent while the Ticket Question flag is off. Tickets of `in_person` and `import` sales appear beside the `online` ones and start out owing everything, because those buyers were never asked — each row carries its `channel` so that reads as history rather than as loss. Started Events still report the whole roster and its debts, because "who came and who never told us" is what a reader after the fact came to find out.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id           path      string  true   "Event ID"
// @Param        page         query     int     false  "Page number (default 1)"
// @Param        page_size    query     int     false  "Rows per page (default 50, max 100)"
// @Param        outstanding  query     bool    false  "Only the Tickets that still owe a required Answer"
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
		outstandingPageParam(query.Get("page")),
		outstandingPageSizeParam(query.Get("page_size")),
		outstandingFilterParam(query.Get("outstanding")),
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
