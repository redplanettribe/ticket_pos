package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Suggested Follows (#231, parent #229, ADR 0031). One route, sitting beside the
// Follows surface rather than inside it.
//
// It is a SIXTH route and not a field on the Follows listing. That listing
// resolves the Follow control on the explorer and on every Event and
// Organization page (ADR 0030 built no per-subject probe), so a ranking folded
// into it would run on every render of the platform's hot public surfaces and be
// discarded. Its own route is also what makes a cache, if this is ever worth
// caching, a change in one place.

// ListFollowSuggestions returns the Tags and Organizations the signed-in
// Customer does not yet Follow.
//
// @Summary      List Suggested Follows
// @Description  Returns Tags and Organizations the signed-in Customer does not Follow, ranked by Activity — the count of discoverable upcoming Events carrying a Tag or run by an Organization, over exactly the subset the public explorer lists. Activity measures supply and never audience: it counts Events, and no follower count is computed, stored or exposed. Two groups rather than one interleaved list, because a Tag's rank and an Organization's are not the same unit; subjects reuse the shapes the Follows listing publishes. Each entry carries the reason it was chosen, naming the producing Tag by canonical key so no language crosses the wire — null while ranking is by Activity alone. Subjects the Customer already Follows are excluded, a Custom Tag needs more than one upcoming Event to qualify, and an Organization with nothing upcoming never appears. An empty result is a 200 with empty groups, never an error. Requires a full Customer Session: a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT, because what a person is suggested is derived from what they Follow, which is private to them.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerFollowSuggestions
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/customer/follow-suggestions [get]
func (h *Handler) ListFollowSuggestions(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	suggestions, err := h.svc.ListFollowSuggestions(r.Context(), customerSessionToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, suggestions)
}
