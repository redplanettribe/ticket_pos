// Package handler exposes the staff HTTP surface for Affiliate Links.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/affiliates/service"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Handler exposes HTTP endpoints for Affiliate Links.
type Handler struct {
	svc *service.Service
}

// New returns an Affiliate Links HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// createAffiliateLinkBody is the create request. It carries a name and nothing
// else: the code is system-generated and immutable, so there is no field for it.
type createAffiliateLinkBody struct {
	Name string `json:"name"`
}

func actorFromRequest(r *http.Request) service.ActorContext {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.ActorContext{
		MemberID:       member.MemberID,
		OrganizationID: member.OrganizationID,
	}
}

// CreateAffiliateLink adds a named Affiliate Link to an Event.
//
// @Summary      Create affiliate link
// @Description  Creates a named Affiliate Link on an Event and returns it with its system-generated, immutable code and the full copyable Storefront URL ({storefrontBase}/{orgSlug}/events/{eventSlug}?ref=CODE). The code is never client-settable; any code in the request body is ignored. Org Admin and Event Owner only.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string                   true  "Event ID"
// @Param        body  body      createAffiliateLinkBody  true  "Affiliate link name"
// @Success      201   {object}  openapi.EnvelopeAffiliateLink
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/affiliate-links [post]
func (h *Handler) CreateAffiliateLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body createAffiliateLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	name, ok := service.NormalizeName(body.Name)
	if !ok {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "name", Message: fmt.Sprintf("is required and must be at most %d characters", service.MaxNameLength)},
		})
		return
	}

	link, err := h.svc.CreateAffiliateLink(r.Context(), actorFromRequest(r), eventID, name)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, link)
}

// ListAffiliateLinks returns an Event's Affiliate Links.
//
// @Summary      List affiliate links
// @Description  Lists an Event's Affiliate Links, newest first: name, immutable code, active status, the full copyable Storefront URL, when it was created, and the link's Affiliate Attribution figures — sales_count, the ACTIVE Ticket Sales it drove, and net_proceeds_cents, what those sales left the Organization after the Platform Fee and its Fee IVA. Both figures are display-only and count active sales only: a Sale Reversal by any route drops the sale out of each, and an attributed free claim counts as a sale worth nothing. Org Admin and Event Owner only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeAffiliateLinkList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/affiliate-links [get]
func (h *Handler) ListAffiliateLinks(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	links, err := h.svc.ListAffiliateLinks(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, links)
}
