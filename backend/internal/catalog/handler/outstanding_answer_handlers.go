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

// ListOutstandingAnswers returns the Event's Tickets that still owe required
// Answers, and — since #329 — who is coming on each of them.
//
// ONE ROUTE AND NOT TWO. The guest list rides on this read rather than on a
// second staff endpoint, because this one already walks the Event's Tickets and
// an Organizer asking "who is coming" is looking at the same list as an Organizer
// asking "who has not told me their size". The handler is unchanged by it: the
// widening is entirely in what a row says.
//
// A READ AND NOTHING ELSE. There is no act on this route and there is no state
// behind it: an Outstanding Answer is DERIVED on every read from what the Ticket
// Type asks and what the Ticket has said, so the list empties by itself as
// Answers arrive from any of the three routes — Event Staff, the checkout
// capture, or an Answer Link — and a reversed Ticket Sale's Tickets drop out of
// it whole.
//
// @Summary      List an event's outstanding answers
// @Description  A page of the Event's Tickets that still owe required Ticket Questions an Answer, oldest sale first, each naming the questions it owes. An Outstanding Answer is a debt and never a defect: nothing was refused for want of one, on any channel. ONLY REQUIRED questions produce one — an unanswered optional question is not a debt. A RETIRED question produces none either, because every write path into an Answer refuses a retired question, so a debt under one could never be discharged; the Answers already given to a retired question are untouched and still read on the Ticket. Tickets of `in_person` and `import` sales appear beside the `online` ones and start out owing everything, because those buyers were never asked — each row carries its `channel` so that reads as history rather than as loss. Tickets of a REVERSED Ticket Sale never appear. Started Events still report their outstanding answers, even though nothing may be written any more, because "twelve people never told us" is what a reader after the fact came to find out. `outstanding_count` is the Event's total number of debts, while `pagination.total` counts the Tickets carrying them. Answers 404 while the Ticket Question feature flag is off. EACH ROW IS ALSO A GUEST LIST ENTRY: it carries the Ticket's `assignment_state` — `unassigned`, `assigned` or `accepted` — and, once a Holder has ACCEPTED, that Holder's own name and email address beside the Answers they owe (ADR 0047). Nothing about a Holder is disclosed before acceptance: an address a buyer typed and its owner never accepted is reported as `assigned` and never named, and a Ticket whose unaccepted address the retention purge has taken reads `unassigned` like any other. All four fields are ABSENT while the Ticket Assignment feature flag is off, which is a separate flag from the Ticket Question one.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id         path      string  true   "Event ID"
// @Param        page       query     int     false  "Page number (default 1)"
// @Param        page_size  query     int     false  "Rows per page (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeOutstandingAnswers
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/outstanding-answers [get]
func (h *Handler) ListOutstandingAnswers(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ok := pathValueRequired(w, r, reqID, "id")
	if !ok {
		return
	}

	query := r.URL.Query()
	result, err := h.svc.ListOutstandingAnswers(
		r.Context(), actorFromRequest(r), eventID,
		outstandingPageParam(query.Get("page")),
		outstandingPageSizeParam(query.Get("page_size")),
	)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
