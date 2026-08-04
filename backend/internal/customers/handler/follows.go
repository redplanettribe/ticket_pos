package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Follows surface (#217, parent #215). Three routes, all behind the Customer
// Session middleware and narrowed once more inside the service to a FULL session.
//
// The Organization is named by SLUG in the path, never by id. Every Storefront
// address already names an Organization that way, the public Organization
// profile publishes no internal id at all, and Tags will key symmetrically on
// their canonical key when they join this surface (#218) — so slug is the one
// identifier a client both has and is meant to have.

// ListFollows returns everything the signed-in Customer Follows.
//
// @Summary      List the Customer's Follows
// @Description  Returns everything the signed-in Customer Follows, most recently followed first. One list rather than one per kind: each entry carries a `type` discriminator and the subject hangs off the field named by it, so a client switches on `type` and keeps working as further kinds of Follow are added. Always scoped by the Customer Session, never by any identifier in the request. Requires a full Customer Session: a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeCustomerFollows
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/customer/follows [get]
func (h *Handler) ListFollows(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	follows, err := h.svc.ListFollows(r.Context(), customerSessionToken(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, follows)
}

// FollowOrganization records that the signed-in Customer Follows an Organization.
//
// 200 rather than 201, and that is the contract rather than an oversight: the
// call is idempotent, so a repeat returns the Follow that already existed, still
// carrying the instant it was first made. There is nothing here that a second
// identical request could conflict with.
//
// @Summary      Follow an Organization
// @Description  Records that the signed-in Customer Follows the Organization named by slug, and returns the Follow. Idempotent: following something already followed returns the existing Follow with its original `followed_at` rather than a conflict, so a retried or double-tapped request is safe. Answers 200 on both the first call and every repeat. A slug no Organization owns is ORGANIZATION_NOT_FOUND. Requires a full Customer Session: a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT, because a forwarded Sale Confirmation is not authority to subscribe somebody's inbox to mail.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        slug  path      string  true  "Organization slug"
// @Success      200   {object}  openapi.EnvelopeCustomerFollow
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/customer/follows/organizations/{slug} [post]
func (h *Handler) FollowOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	follow, err := h.svc.FollowOrganization(r.Context(), customerSessionToken(r), r.PathValue("slug"))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, follow)
}

// UnfollowOrganization removes the signed-in Customer's Follow of an Organization.
//
// @Summary      Unfollow an Organization
// @Description  Removes the signed-in Customer's Follow of the Organization named by slug. Unfollowing something not followed is not an error — the caller asked for a state that already holds. A slug no Organization owns is ORGANIZATION_NOT_FOUND. Requires a full Customer Session: a Confirmation Link session is refused with CUSTOMER_SESSION_SCOPE_INSUFFICIENT. This is an Unfollow and not an Unsubscribe: it removes one Follow, where Unsubscribing would leave every Follow standing and silence the Follow Digest.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        slug  path      string  true  "Organization slug"
// @Success      200   {object}  openapi.EnvelopeCustomerLogout
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/customer/follows/organizations/{slug} [delete]
func (h *Handler) UnfollowOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	if err := h.svc.UnfollowOrganization(r.Context(), customerSessionToken(r), r.PathValue("slug")); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Unfollowed",
	})
}
