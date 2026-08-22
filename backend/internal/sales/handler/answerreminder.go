package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// SweepAnswerReminders runs the Answer Reminder sweep: one mail to each buyer
// whose Ticket Sale still owes Answers and whose rationing allows it (#317,
// ADR 0044).
//
// INTERNAL SERVICE-TO-SERVICE, on exactly the terms PurgeAbandonedAnswers and
// DrainReversalRequests set out: the API is deployed
// --no-allow-unauthenticated, so Cloud Run IAM rejects any caller without a
// Google-signed OIDC ID token — which Cloud Scheduler presents in
// `Authorization` — before the container is reached (ADR 0008). The route
// carries no application middleware because there is no application credential
// that grants this, and none is read here.
//
// THE CALLER CANNOT AIM IT, which matters more on this route than on any other
// in the namespace. There is no body, no path parameter and no query: not an
// Event, not an Organization, not a Ticket Sale, and above all no way to name
// the moment. A `now` or a `since` parameter would be a button that lifts the
// seven-day silence and mails the platform's entire outstanding backlog on
// demand — the same shape of mistake as letting a caller name the purge's cutoff
// or the Digest's week, with somebody's inbox rather than a table on the other
// end of it. WHO is written to is a property of the database and the clock.
//
// WHAT IT PUTS IN THE 200 BODY IS COUNTS, and it names no buyer, no address, no
// Ticket Sale and no Event. A response listing who had just been mailed about
// unanswered questions would publish, to anything holding the scheduler's token,
// a list of people and the fact that they are being chased — and this feature's
// whole premise is that an Answer may be health data. The per-run log line obeys
// the same rule; the per-Sale lines that name a ticket_sale_id are failures,
// where an operator has to be able to find the row, and they name no person.
//
// Deliberately safe to call by hand, at any time and repeatedly. A second call
// inside the same minute mails nobody: every Sale the first one reached is
// inside its seven-day cooldown, recorded in the ledger before the response was
// written. Two ticks overlapping can at worst duplicate one message, in the
// narrow window between a send and its ledger row — reported as `unrecorded`
// rather than hidden.
//
// @Summary      Send Answer Reminders
// @Description  Sweeps the active Ticket Sales whose Tickets still owe required Ticket Question Answers and emails each buyer one reminder pointing at their sale's page, where they can answer what they know and copy each Ticket's own Answer Link for whoever will use it (ADR 0044). Addressed to the BUYER and never to a holder: the platform stores no holder address and asks for none. Rationed per Ticket Sale — at most one mail every 7 days and at most two ever — silent once the Event has started, and never sent for a reversed Sale. Swept rather than triggered by an edit, so an Organization authoring four questions in ten minutes cannot mail the same people four times. Transactional: it is not gated by Marketing Consent, exactly as a Sale Confirmation is not, and it is written in the recipient's Mail Locale (the Sale Locale first, then the Customer's, then English). Sends nothing at all while TICKET_QUESTIONS_ENABLED is off (ADR 0045), and the Cloud Scheduler job that drives it ships paused. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Nothing about the run can be named by the caller — not the moment, not an Event, not a Sale — because a caller who could name the moment could lift the 7-day silence on demand. Safe to call by hand and effectively idempotent: a second run inside the cooldown mails nobody. The response reports how many Sales were due, how many mails went, how many were skipped or refused, how many were sent but could not be recorded, and the standing backlog. It names no buyer, no address and no sale.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeAnswerReminderSweep
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/answer-reminders/sweep [post]
func (h *Handler) SweepAnswerReminders(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.SweepAnswerReminders(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
