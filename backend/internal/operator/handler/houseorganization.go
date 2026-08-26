package handler

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// DesignateHouseOrganization marks an Organization as a House Organization.
//
// @Summary      Designate an Organization a House Organization
// @Description  Marks the Organization as one the platform's own legal entity runs (ADR 0060): from then on every paid Online Sale of one of its Events is the platform's own sale for tax purposes and will owe a Sale Invoice from the platform's Issuer to the buyer. The designation is stamped with the designating operator's email (taken from the Staff Session, never from a body — there is none) and the server's clock, and the response is the Organization as it now stands with house_designated_by and house_designated_at filled. REFUSED with 409 HOUSE_ORGANIZATION_CURRENCY_UNSUPPORTED, naming the currency in the message and in details.currency, when the Organization trades in a currency the Issuer does not invoice in (USD is the only one): no document could ever be built for its sales. NOT refused for a missing Issuer, an Issuer in the test environment or an expired certificate — none of that is consulted; the platform's compliance is the operator's to see and fix, never the buyer's to wait for. Designating an Organization that is already designated changes nothing and answers 200 with the original trail: the trail names the act that made it a House Organization. Designation affects future sales only, nothing is issued retroactively, and nothing is invoiced by this endpoint. An unknown or malformed id is 404 ORGANIZATION_NOT_FOUND. Platform Operator only — an Org Admin has no surface for this anywhere.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        orgID  path  string  true  "Organization ID"
// @Success      200  {object}  openapi.EnvelopeOperatorOrganization
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/organizations/{orgID}/house [put]
func (h *Handler) DesignateHouseOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	// The trail is taken from the session and nowhere else: "who made this
	// Organization the platform's" must not be something a caller can claim.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}

	org, err := h.svc.DesignateHouseOrganization(r.Context(), strings.TrimSpace(r.PathValue("orgID")), session.Email)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, org)
}

// UndesignateHouseOrganization takes the House designation back.
//
// @Summary      Clear an Organization's House designation
// @Description  Takes the House Organization designation back (ADR 0060), emptying house_designated_by and house_designated_at together, and returns the Organization as it now stands. From then on the Organization's sales are its own again and owe nothing; nothing already owed or issued is touched — a mistaken or ended arrangement stops producing Sale Invoices, and undesignation affects future sales only. Clearing an Organization that was never designated is an ordinary 200 rather than a refusal: the state asked for is the state reached. No body. An unknown or malformed id is 404 ORGANIZATION_NOT_FOUND. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        orgID  path  string  true  "Organization ID"
// @Success      200  {object}  openapi.EnvelopeOperatorOrganization
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/organizations/{orgID}/house [delete]
func (h *Handler) UndesignateHouseOrganization(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	org, err := h.svc.UndesignateHouseOrganization(r.Context(), strings.TrimSpace(r.PathValue("orgID")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, org)
}
