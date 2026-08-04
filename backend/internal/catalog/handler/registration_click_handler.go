package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// RecordRegistrationClick counts one hand-off to an Event's Registration Link.
//
// @Summary      Record registration link click
// @Description  Counts one hand-off from an Event page to its Registration Link. Public and unauthenticated: the caller is the Storefront redirect route a Customer is passing through on their way to the registration site, not a browser talking to this API directly. Raw counting — a Customer who returns counts again, with no dedup and no visitor identification — and it counts CLICKS, never registrations and never people: the platform loses sight of the Customer at the link and never learns whether they signed up. An address that resolves to nothing (unknown Organization, unknown Event, or an Event that sells Ticket Types here) is accepted and counts nothing, so a tracking miss can never become an error that stops somebody registering.
// @Tags         public
// @Produce      json
// @Param        slug       path      string  true  "Organization slug"
// @Param        eventSlug  path      string  true  "Event slug"
// @Success      202  {object}  platform.Envelope
// @Router       /api/v1/public/organizations/{slug}/events/{eventSlug}/registration-link/click [post]
func (h *Handler) RecordRegistrationClick(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	// Every outcome is 202, exactly as the Affiliate Link click endpoint answers:
	// recorded, and accepted-but-counted-nothing, are the same answer to a caller
	// whose next move does not depend on the result. A storage failure is logged
	// inside the service and swallowed here for the same reason — the Customer on
	// the other end of this is mid-navigation, and a display-only counter may
	// never cost them their journey.
	_ = h.svc.RecordRegistrationClick(
		r.Context(),
		strings.TrimSpace(r.PathValue("slug")),
		strings.TrimSpace(r.PathValue("eventSlug")),
	)
	_ = platform.WriteSuccess(w, reqID, http.StatusAccepted, nil)
}
