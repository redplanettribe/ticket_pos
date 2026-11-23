// Package handler exposes Home over HTTP (#858, ADR 0081).
//
// Its routes sit on the staff namespace and are open to any Member of the
// Active Member's Organization, Event Staff included; what a role may not see
// is withheld by the service from the response, never hidden by the page.
package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/organizationtrends/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler exposes Home's endpoints.
type Handler struct {
	svc *service.Service
}

// New returns a Home HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// readerFromRequest is the Active Member reading Home.
func readerFromRequest(r *http.Request) service.Reader {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.Reader{OrganizationID: member.OrganizationID, Role: member.Role}
}

// GetHomeNow returns what stands now across the Organization.
//
// @Summary      Get what stands now on the Organization's Home
// @Description  Returns the top of the Active Member's Organization's Home, the whole Organization whichever Member reads it. active_events counts the Active Events, those neither cancelled nor over (the end instant passed, or the start where there is no end), drafts included; has_events is false for an Organization with no Event in any status. next_event is the published, non-cancelled Event with the earliest start that is not over, so an Ongoing Event (started, end not reached) is chosen over a later one and ongoing says so; a draft is never next. It carries the Event's Tickets Sold so far on every Sales Channel, reversed sales left out, and is null when there is no such Event. today_so_far is today's sales on the platform clock (America/Guayaquil), by the instant each sale was made, so a Sale Import backdated to today counts and one backdated to another day does not; reversed Ticket Sales are left out, as Sales Trends leaves them out. It is always partial. takings_cents is the Takings of those sales, as Sales Trends computes it, in the Organization's currency: null for Event Staff, never zero in its place. payable_balance_cents is the Payable Balance exactly as the Payouts page states it, null for anyone but an Org Admin. Any Member.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeHomeNow
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/organization/home/now [get]
func (h *Handler) GetHomeNow(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	now, err := h.svc.Now(r.Context(), readerFromRequest(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, now)
}
