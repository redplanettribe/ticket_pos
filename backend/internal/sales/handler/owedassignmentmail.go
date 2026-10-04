package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// SweepOwedAssignmentMails runs the Owed Assignment Mail sweep: the Assignment
// mails a Named Tickets checkout owes, sent paced and retried after the Ticket
// Sale is recorded (#671, parent #665, ADR 0076).
//
// THE ASSIGNMENT REMINDER'S ROUTE, on every one of its terms: internal
// service-to-service behind Cloud Run IAM (ADR 0008), no application
// middleware, no body, no parameter and no moment - whom it writes to is a
// property of the database and the backend's clock - and counts in the 200 body
// and nothing else.
//
// SAFE TO CALL BY HAND, at any time and repeatedly. A sent mail is no longer
// owed, a failed one is backed off, and two runs at once skip each other's
// claims, so no mail goes twice.
//
// @Summary      Send owed Assignment mails
// @Description  Sends the Assignment mails a Named Tickets checkout owes: one per Ticket the buyer named at checkout for an address other than their own, written as owed by the commit that recorded the Ticket Sale. Each is the ordinary Assignment mail with the ordinary Assignment Link, written in the Holder's Mail Locale, then the Sale Locale, then English. Before sending, the Ticket is re-read: an owed mail whose Ticket has been reassigned since, has no address any more, or was accepted, whose Ticket Sale was reversed, or whose Event has started is dropped and never sent. A sent mail is recorded in the Assignment mail ledger, where it counts against its Ticket's lifetime allowance and never against the buyer's rolling window, and is owed no more. A send the provider refuses or that fails stays owed and is retried by a later run after a backoff. Sends are paced by the gap the Reminder sweeps share, one at a time, oldest owed first, at most a batch per run and inside a run budget. Holds everything, sending and dropping nothing, while TICKET_ASSIGNMENT_ENABLED is off or no mailer or Assignment Link secret is configured. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Nothing about the run can be named by the caller. Safe to call by hand and concurrently: claims are leased and skip each other, so no mail is sent twice. The response reports how many mails went, how many were dropped, how many failed, how many were sent but could not be recorded, and the standing backlog. It names nobody.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeOwedAssignmentMailSweep
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/owed-assignment-mails/sweep [post]
func (h *Handler) SweepOwedAssignmentMails(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.SweepOwedAssignmentMails(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
