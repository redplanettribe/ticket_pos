package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// SweepEventRecaps runs the Event Recap sweep: one mail per Event Owner of
// each published Event that is over, settled and not yet recapped, or per Org
// Admin of its Organization when it has no Event Owner (spec #884, #891,
// #885).
//
// THE ASSIGNMENT REMINDER'S ROUTE, on every one of its terms: internal
// service-to-service behind Cloud Run IAM (ADR 0008), no application
// middleware, no body, no parameter and no moment - which Events are recapped
// is a property of the database and the backend's clock - and counts in the
// 200 body and nothing else.
//
// SAFE TO CALL BY HAND, at any time and repeatedly. A run is the run of the
// latest 09:00 of the platform clock, so a call in the afternoon decides what
// the morning's run decided, and an Event already recapped is never due again.
//
// @Summary      Send Event Recaps
// @Description  Sweeps the published Events that are over (the end instant passed, or the start where there is no end), were over at or after the instant the Event Recap went live, were over at least six hours before the run's 09:00 on the platform clock (America/Guayaquil), and were never recapped, and emails each one's Event Recap to every Event Owner of the Event, as its Event assignments stand when the mail is sent, or, when the Event has no Event Owner then, to every Org Admin of its Organization, one copy per address compared normalized, each in their own Staff Locale and English where none is stored. The run is that of the latest 09:00 the platform clock has reached, whenever it is called, so a late or repeated call decides what the scheduled one would have, and a missed day is made good by the next run. A draft, a cancelled Event and a dateless Event are never recapped; an Event that sold nothing is. An Event is recorded as recapped once at least one address accepted its mail, and is never due again whatever happens to its dates afterwards; rescheduling it later before it is recapped makes the recap wait for the new time. An Event none of whose readers could be written to, or whose readers or sales figures could not be read, stays due. An Event with no Event Owner and no Org Admin sends nothing and is recorded as recapped all the same, so it is never retried. Event Staff are never written to, and nobody is written to for being a Platform Operator. The mail names the Event and no Customer, says its figures are as of the instant it was sent, states the Event's sales figures over its active Ticket Sales (Tickets Sold and Ticket Sales for the Event entire and by Sales Channel, Tickets Sold by Ticket Type, Takings, the Net Proceeds of its Online Sales, and how many of its Sales are reversed), the money in the Organization's currency and in the marks of the reader's Staff Locale, and links to the Event's Sales Trends, Reach and Sales list in the staff application. Sends are paced by the gap the Reminder sweeps share, the Event over longest first, at most a batch of Events per run and inside a run budget. Internal service-to-service only: Cloud Run IAM authenticates the caller by Google-signed OIDC ID token before the request reaches the API (ADR 0008), and no Customer Session or staff token reaches it. Nothing about the run can be named by the caller. The response reports how many Events were due in this batch, how many were recapped, how many mails went and how many were refused, how many Events were skipped, how many were mailed but could not be recorded, and how many were recorded with nobody to write to. It names nobody and no Event.
// @Tags         internal
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeEventRecapSweep
// @Failure      500  {object}  platform.Envelope
// @Router       /api/v1/internal/event-recaps/sweep [post]
func (h *Handler) SweepEventRecaps(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	result, err := h.svc.SweepEventRecaps(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
