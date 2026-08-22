package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// SweepAnswerReminders runs the Answer Reminder sweep: one mail to each person
// who can answer what a Ticket still owes and whose rationing allows it (#317,
// ADR 0044; #328, ADR 0046).
//
// WHO THAT IS CHANGED IN #328 AND NOTHING ELSE DID. The Holder of an `accepted`
// Ticket, the buyer for every other Ticket on the Sale, and a Sale with a mix
// produces both — one message to the buyer covering the Tickets still theirs to
// chase, and one to each Holder about their own. The route, the pause, the
// authentication, the caps and the fact that it is swept rather than triggered
// are all exactly as they were.
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
// Event, not an Organization, not a Ticket Sale, not a Ticket, and above all no
// way to name the moment. A `now` or a `since` parameter would be a button that
// lifts the seven-day silence and mails the platform's entire outstanding
// backlog on demand — the same shape of mistake as letting a caller name the
// purge's cutoff or the Digest's week, with somebody's inbox rather than a table
// on the other end of it. WHO is written to is a property of the database and
// the clock.
//
// WHAT IT PUTS IN THE 200 BODY IS COUNTS, and it names no buyer, no Holder, no
// address, no Ticket, no Ticket Sale and no Event. A response listing who had
// just been mailed about unanswered questions would publish, to anything holding
// the scheduler's token, a list of people and the fact that they are being
// chased — and this feature's whole premise is that an Answer may be health
// data. It does not even break the tally down by recipient kind, which on a
// platform with one Organization would come close to naming somebody. The
// per-run log line obeys the same rule; the per-Sale lines that name a
// ticket_sale_id are failures, where an operator has to be able to find the row,
// and they name no person and carry no link.
//
// Deliberately safe to call by hand, at any time and repeatedly. A second call
// inside the same minute mails nobody: every Ticket the first one covered is
// inside its seven-day cooldown, recorded in the ledger before the response was
// written. Two ticks overlapping can at worst duplicate one message, in the
// narrow window between a send and its ledger rows — reported as `unrecorded`
// rather than hidden.
//
// @Summary      Send Answer Reminders
// @Description  Sweeps the Tickets of active Ticket Sales that still owe required Ticket Question Answers and emails whoever can actually answer them (ADR 0044, ADR 0046). A Ticket that is `accepted` produces a reminder to its HOLDER, carrying their own Assignment Link and naming no buyer, no price and no Sale Confirmation reference; a Ticket that is `unassigned` or `assigned` produces one to the BUYER, pointing at their sale's page where they answer what they know and copy each Ticket's own Answer Link for whoever will use it. A Sale with a mix produces both — one mail to the buyer covering only the Tickets still theirs to chase, and one to each accepted Holder about their own — never one mail listing everything to everybody. Rationed per TICKET: at most one mail every 7 days and at most two ever, so a four-ticket sale can chase two Holders without mailing either twice. Silent once the Event has started, and never sent for a reversed Sale. Swept rather than triggered by an edit, so an Organization authoring four questions in ten minutes cannot mail the same people four times. Transactional: it is not gated by Marketing Consent, exactly as a Sale Confirmation is not — which matters most for a Holder, who accepted a ticket and opted into nothing. Written in the recipient's Mail Locale: the Sale Locale first for the buyer, whose own purchase it is about, and the recipient's own remembered locale first for a Holder, who is not party to the sale. Sends nothing at all while TICKET_QUESTIONS_ENABLED is off (ADR 0045); with TICKET_ASSIGNMENT_ENABLED off every Ticket is addressed to its buyer, exactly as before ADR 0046. The Cloud Scheduler job that drives it ships paused. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Nothing about the run can be named by the caller — not the moment, not an Event, not a Sale, not a Ticket — because a caller who could name the moment could lift the 7-day silence on demand. Safe to call by hand and effectively idempotent: a second run inside the cooldown mails nobody. The response reports how many mails were due, how many went, how many were skipped or refused, how many were sent but could not be recorded, and the standing backlog. It names nobody and does not say which recipients were Holders.
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
