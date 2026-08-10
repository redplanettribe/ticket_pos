// Package handler exposes the Follow Digest pipeline's two internal endpoints
// (#220, parent #215, ADR 0030).
//
// Both live under `/api/v1/internal/`, both take no body and no path parameter,
// and neither reads a session. Their gate is not in this process: the API is
// deployed --no-allow-unauthenticated, so Cloud Run IAM rejects any caller
// without a Google-signed OIDC ID token before the container is reached
// (ADR 0008). See server.registerInternalRoutes, which states that once for the
// whole namespace.
//
// WHY TWO ENDPOINTS AND NOT ONE. A single "send this week's Digests" call would
// have been simpler and is not viable: the mail provider's rate limit is roughly
// two requests per second, which ADR 0009 records as unsolved for bulk sends, so
// mailing every Customer inside one request would outlast the Cloud Run request
// timeout long before it satisfied the provider. Splitting the work also buys
// the property the tests rest on — a week can be produced deterministically and
// then drained in controlled batches — and it is what lets a failed send be
// retried by a process that has since been redeployed.
package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/digest/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler serves the Follow Digest pipeline.
type Handler struct {
	svc *service.Service
}

// New builds the handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// EnqueueFollowDigests declares this week and creates one pending Follow Digest
// per eligible Customer (ADR 0030).
//
// There is nothing to validate, and in particular WHICH WEEK is not the
// caller's to name. The week comes from the clock through
// platform.DigestWeekStart, because an internal endpoint that let its caller
// name a week would be a way to make the platform re-mail an old one to every
// following Customer on demand — the same reason the reversal drain lets nobody
// name a Reversal Request.
//
// Safe to call by hand, at any time and repeatedly. A second call inside one
// week creates nothing: the uniqueness on (Customer, week) refuses it, and the
// response says how many Customers were already enqueued rather than reporting a
// bare zero an operator would read as a failure.
//
// @Summary      Enqueue this week's Follow Digests
// @Description  Creates one pending Follow Digest per eligible Customer for the current week (ADR 0030). A Customer is eligible when they hold at least one Follow — of an Organization or of a Tag — and their email has been verified: a Follow is a request to be written to, and somebody who never pressed one is never enqueued. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Which week is enqueued is taken from the clock and cannot be named by the caller — it is the Monday that begins the current week in Ecuador's timezone, echoed back in the response. Nothing is composed here and no email is sent: the row records only that this Customer is owed a Digest for this week, and what it will say is decided when the drain sends it. Safe to call by hand at any time and idempotent within a week — a second call creates no rows, because the database refuses more than one Digest per Customer per week, and the response reports those Customers as already enqueued.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeFollowDigestEnqueue
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/follow-digests/enqueue [post]
func (h *Handler) EnqueueFollowDigests(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.EnqueueWeek(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// DrainFollowDigests composes and sends the pending Follow Digests that are due.
//
// Each Digest is composed AT SEND TIME rather than at enqueue, so one delayed an
// hour still reflects reality. A Customer whose Follows matched nothing they had
// not already been shown is recorded as having nothing to say and is sent no
// email at all — an empty Digest is worse than silence.
//
// Deliberately safe to call by hand, at any time and repeatedly. An empty queue
// is a 200 with zeros, a Digest another tick is working is skipped rather than
// waited for, and the response carries the standing backlog — so the runbook is
// one curl, and it exists before the schedule that will call this does (#226).
//
// WHAT IS NOT IN THE 200 BODY is worth naming, because the reversal drain beside
// it publishes a slice of the payment ledger and this one deliberately publishes
// nothing of the sort. There is no Customer email here, no name and no Event: the
// response is counts and one week. That is not because the gate is weaker — it is
// the same Cloud Run IAM gate — but because nothing an operator needs from this
// endpoint requires naming a reader, and a mailing pipeline's diagnostics are the
// easiest place in a system to leak an address list.
//
// @Summary      Send due Follow Digests
// @Description  Claims a batch of pending Follow Digests, composes each one at send time, sends it, records everything it carried in the sent-ledger, and marks the row done (ADR 0030). Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008). Composition happens here rather than at enqueue, so a Digest delayed by a backlog or a retry still reflects the catalogue as it stands when it is sent. A Digest lists the Events matched by that Customer's Follows that they have not already been shown, filtered exactly as the public explorer filters — published, discoverable and not yet ended — so an Event an Organization chose not to list is never mailed out. It is written in the Customer's remembered Mail Locale, naming Preset Tags in that language and Custom Tags exactly as their Organization coined them. A Customer whose Follows matched nothing receives no email at all rather than an empty one, and that Digest is recorded as having had nothing to say. Every Event included is written to the sent-ledger and only those Events are, which is what stops a later Digest repeating them. Re-running after a completed send produces no second email for that Customer and week. A delivery failure leaves the Digest in the queue on a backoff and a later run sends exactly one email; after its attempts are spent the Digest is abandoned, because a Digest is about the week it names and one delivered days late is worse than none. Each run claims one Digest at a time and stops at a time budget of its own that expires before any deadline outside it, so a backlog can never wedge it; whatever it did not reach stays exactly as due as it was found. Safe to call by hand at any time and a no-op on an empty queue. The response tallies what the run did and reports the standing backlog, so two calls a minute apart say whether an incident is getting better or worse.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeFollowDigestDrain
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/follow-digests/drain [post]
func (h *Handler) DrainFollowDigests(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.DrainDigests(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
