package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// SweepAssignmentReminders runs the Assignment Reminder sweep: one mail to the
// buyer of each online Ticket Sale that still has Tickets nobody holds, whose
// rationing allows it (#362, parent #361, ADR 0051).
//
// THE ANSWER REMINDER'S ROUTE, WITH THE BUYER AS THE READER. Every property
// SweepAnswerReminders states holds here unchanged and for the same reasons:
//
// INTERNAL SERVICE-TO-SERVICE. The API is deployed --no-allow-unauthenticated,
// so Cloud Run IAM rejects any caller without a Google-signed OIDC ID token —
// which Cloud Scheduler presents — before the container is reached (ADR 0008).
// No application middleware, because no application credential grants this.
//
// THE CALLER CANNOT AIM IT. No body, no path parameter, no query: not an
// Event, not an Organization, not a Sale, and above all no moment. A `now`
// would be a button that lifts the seven-day silence and mails the platform's
// whole backlog on demand. WHO is written to is a property of the database and
// the clock.
//
// COUNTS IN THE 200 BODY AND NOTHING ELSE: no buyer, no address, no Sale, no
// Event. A response that listed who had just been reminded would hand a list
// of people to anything holding the scheduler's token. The per-run log line
// obeys the same rule; the per-Sale lines that name a ticket_sale_id are
// failures, where an operator has to find the row, and name no person.
//
// SAFE TO CALL BY HAND, at any time and repeatedly. A second call inside the
// same hour mails nobody: every Sale the first one covered is inside its
// seven-day cooldown, recorded before the response was written. Two ticks
// overlapping can at worst duplicate one message, reported as `unrecorded`.
//
// THE FIRST RUN IS THE ANNOUNCEMENT. Sales made before Ticket Assignment went
// live are ordinary candidates of this sweep; ADR 0051 gives them one extra
// sentence (#364) and no separate mechanism. The launch procedure — migrate,
// review the candidate count against a production copy, unpause, force one
// run — is the Operator's and is documented on the PR, not automated here.
//
// @Summary      Send Assignment Reminders
// @Description  Sweeps the online Ticket Sales that have more than one Ticket, at least one of them still unassigned, and an Event that has not started, and emails each Sale's BUYER once: the Event, how many of their tickets still have no address, a fresh Confirmation Link to the Sale's page, and the sentence ADR 0047 requires about what giving an address does. Rationed per Ticket Sale (ADR 0051): not before 24 hours after the Sale, at most one mail every 7 days, at most two ever; silent once the Event has started or every Ticket is assigned; never for a single-Ticket Sale, a reversed Sale or an import Sale. A reassigned Self-held Ticket counts as assigned. Transactional: not gated by Marketing Consent, exactly as the Sale Confirmation that carried the same link is not. Written in the Sale Locale, then the buyer's Mail Locale, then English. Sends nothing at all while TICKET_ASSIGNMENT_ENABLED is off. The Cloud Scheduler job that drives it ships paused, and its first run is the announcement to buyers who bought before Ticket Assignment existed. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Nothing about the run can be named by the caller — not the moment, not an Event, not a Sale — because a caller who could name the moment could lift the 7-day silence on demand. Safe to call by hand and effectively idempotent: a second run inside the cooldown mails nobody. Drains a large backlog in batches, oldest Sale first. The response reports how many Sales were due in this batch, how many mails went, how many were skipped or refused, how many were sent but could not be recorded, and the standing backlog. It names nobody.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeAssignmentReminderSweep
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/assignment-reminders/sweep [post]
func (h *Handler) SweepAssignmentReminders(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.SweepAssignmentReminders(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
