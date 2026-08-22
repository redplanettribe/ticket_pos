package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// PurgeUnacceptedHolderAddresses runs the Holder Address Purge: the address a
// buyer typed for a friend who never accepted it, taken once the Event has
// started (#331, parent #322, ADR 0046).
//
// INTERNAL SERVICE-TO-SERVICE, on exactly the terms PurgeAbandonedAnswers and
// DrainReversalRequests set out: the API is deployed
// --no-allow-unauthenticated, so Cloud Run IAM rejects any caller without a
// Google-signed OIDC ID token — which Cloud Scheduler presents in
// `Authorization` — before the container is reached (ADR 0008). The route
// carries no application middleware because there is no application credential
// that grants this, and none is read here.
//
// THERE IS NOTHING TO VALIDATE AND NOTHING THE CALLER MAY AIM. The body is
// empty, the path carries nothing, and — the point of this particular endpoint —
// THE MOMENT IS NOT THE CALLER'S TO NAME. A parameter naming the instant Event
// starts are compared against would turn a retention job into a button that
// takes the holder address off every future Event on the platform, reachable by
// anything that ever obtained the token. Neither may an Event, an Organization
// or a Ticket ever appear here: an aimable deletion is a different and much
// worse thing than an unaimable one.
//
// WHAT IT PUTS IN THE 200 BODY IS COUNTS AND A TIMESTAMP, and it names no
// address, no Ticket, no buyer and no Event. That is not an accident of it
// having nothing useful to say: a holder address belongs to somebody who never
// came here and agreed to nothing, so a response listing what had just been
// deleted would publish precisely what the deletion exists to remove. Nobody may
// widen this to "which Tickets", and the per-run log line is bounded by the same
// rule.
//
// Deliberately safe to call by hand, at any time and repeatedly. It is one
// UPDATE against a predicate: a second call inside the same minute purges
// nothing and reports zeros, two ticks overlapping write disjoint sets, and a
// run killed halfway leaves the remainder for the next one.
//
// @Summary      Purge unaccepted holder addresses at Event start
// @Description  Deletes the holder email address from every Ticket still in `assigned` — an address was given and nobody has accepted it — whose Event has started (ADR 0046). Read as an instant: `events.starts_at` is fixed in the Event's own timezone, so the comparison already carries it. The purge takes the ADDRESS ONLY: the Ticket, its Ticket Question Answers, its Ticket Sale and the fact that the Ticket was assigned all survive, the last of them as a purge timestamp on the Ticket, because the platform is entitled to remember that it sold a ticket and that somebody was named for it and is not entitled to keep the name. An `accepted` Ticket loses nothing at any age: its Holder proved the address from their own inbox and is an ordinary Customer under ordinary Customer retention. An Event that has never said when it starts is never purged, matching the reading the assignment window gives a missing start. A reversed Ticket Sale is purged like any other. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. The moment cannot be named by the caller; it is taken from the clock, and the instant actually used is echoed back. Not gated on the Ticket Assignment feature flag, deliberately — the switch that turns a deletion off must never be the switch that turns collection off. Safe to call by hand at any time and idempotent: a second run purges nothing and reports zeros. The response reports how many addresses went, how many Events they came off, the instant used, and how many unaccepted addresses are still held across the platform, so two runs a day apart say whether anybody is assigning at all. It names no address, no Ticket, no buyer and no Event, because the address is the data this job exists to remove.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeHolderAddressPurge
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/holder-addresses/purge [post]
func (h *Handler) PurgeUnacceptedHolderAddresses(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.PurgeUnacceptedHolderAddresses(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
