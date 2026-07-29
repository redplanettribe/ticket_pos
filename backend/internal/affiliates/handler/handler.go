// Package handler exposes the HTTP surface for Affiliate Links: the staff
// section that manages them, and the one public route the Storefront reports
// clicks on.
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

// updateAffiliateLinkBody is the lifecycle request: a rename, an
// activate/deactivate toggle, or both. Both fields are optional — an absent one
// is left as it is — and there is still no field for the code.
type updateAffiliateLinkBody struct {
	Name   *string `json:"name"`
	Active *bool   `json:"active"`
}

// UpdateAffiliateLink renames an Affiliate Link and/or changes whether it is live.
//
// @Summary      Update affiliate link
// @Description  Renames an Affiliate Link and/or takes it out of circulation. Both body fields are optional: an absent field is left unchanged, so a rename never disturbs the link's active state and vice versa. The code and URL are immutable and never change. A deactivated link's code is dead everywhere a buyer could carry it — the click counter ignores it and a checkout begun with it records the sale unattributed — while its historical clicks, attributed sales and Net Proceeds stay on the staff list, marked inactive; reactivating resumes both under the same code. Org Admin and Event Owner only.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string                   true  "Event ID"
// @Param        linkId  path      string                   true  "Affiliate link ID"
// @Param        body    body      updateAffiliateLinkBody  true  "Fields to change"
// @Success      200     {object}  openapi.EnvelopeAffiliateLink
// @Failure      400     {object}  platform.Envelope
// @Failure      401     {object}  platform.Envelope
// @Failure      403     {object}  platform.Envelope
// @Failure      404     {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/affiliate-links/{linkId} [patch]
func (h *Handler) UpdateAffiliateLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, linkID, ok := lifecyclePathValues(w, r, reqID)
	if !ok {
		return
	}

	var body updateAffiliateLinkBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	input := service.UpdateAffiliateLinkInput{Active: body.Active}
	if body.Name != nil {
		name, valid := service.NormalizeName(*body.Name)
		if !valid {
			_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
				{Field: "name", Message: fmt.Sprintf("is required and must be at most %d characters", service.MaxNameLength)},
			})
			return
		}
		input.Name = &name
	}

	link, err := h.svc.UpdateAffiliateLink(r.Context(), actorFromRequest(r), eventID, linkID, input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, link)
}

// DeleteAffiliateLink removes an Affiliate Link that has no history.
//
// @Summary      Delete affiliate link
// @Description  Deletes an Affiliate Link, but only while it has zero clicks, no attributed sales, and no checkout in progress under it — so a link created by mistake can be taken back and one that has actually promoted anything cannot. An attributed Ticket Sale of any status counts as history, a reversed one included, because it still names the link that drove it, and a pending Payment blocks the delete because it may still become such a sale. A checkout that was abandoned or declined does not block it: nobody bought anything, and the Payment simply stops naming the link. A link with history is refused with AFFILIATE_LINK_HAS_HISTORY; deactivate it instead. Org Admin and Event Owner only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string  true  "Event ID"
// @Param        linkId  path      string  true  "Affiliate link ID"
// @Success      200     {object}  openapi.EnvelopeMessage
// @Failure      401     {object}  platform.Envelope
// @Failure      403     {object}  platform.Envelope
// @Failure      404     {object}  platform.Envelope
// @Failure      409     {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/affiliate-links/{linkId} [delete]
func (h *Handler) DeleteAffiliateLink(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, linkID, ok := lifecyclePathValues(w, r, reqID)
	if !ok {
		return
	}

	if err := h.svc.DeleteAffiliateLink(r.Context(), actorFromRequest(r), eventID, linkID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Affiliate link deleted.",
	})
}

// lifecyclePathValues reads the two ids both lifecycle routes address a link by,
// writing the validation error and reporting false when either is missing.
func lifecyclePathValues(w http.ResponseWriter, r *http.Request, reqID string) (eventID, linkID string, ok bool) {
	eventID = strings.TrimSpace(r.PathValue("id"))
	linkID = strings.TrimSpace(r.PathValue("linkId"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return "", "", false
	}
	if linkID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "linkId", Message: "is required"},
		})
		return "", "", false
	}
	return eventID, linkID, true
}

// RecordAffiliateLinkClick counts one visit to an Event page reached through an
// Affiliate Link.
//
// @Summary      Record affiliate link click
// @Description  Counts one visit to an Event page reached through an Affiliate Link's code (?ref=CODE). Public and unauthenticated: the Storefront calls it fire-and-forget while rendering the page. Raw counting — repeat visits count again, with no dedup and no visitor identification. A code that matches nothing live (unknown, mistyped, belonging to another Event, or deactivated) is accepted and counts nothing, so a dead ref in a URL never becomes an error a buyer can see.
// @Tags         public
// @Produce      json
// @Param        slug       path      string  true  "Organization slug"
// @Param        eventSlug  path      string  true  "Event slug"
// @Param        code       path      string  true  "Affiliate link code"
// @Success      202  {object}  platform.Envelope
// @Router       /api/v1/public/organizations/{slug}/events/{eventSlug}/affiliate-links/{code}/click [post]
func (h *Handler) RecordAffiliateLinkClick(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	// Every outcome is 202: recorded, and accepted-but-ignored, are the same
	// answer to a page load that has nothing to do with the result. A storage
	// failure is logged inside the service and swallowed here for the same
	// reason — a display-only counter must never cost a buyer a page.
	_ = h.svc.RecordClick(
		r.Context(),
		strings.TrimSpace(r.PathValue("slug")),
		strings.TrimSpace(r.PathValue("eventSlug")),
		strings.TrimSpace(r.PathValue("code")),
	)
	_ = platform.WriteSuccess(w, reqID, http.StatusAccepted, nil)
}
