package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// promotionBody is the whole Promotion on both writes: there is no partial edit
// of one, so an update states the price and the window again. Timestamps are
// RFC3339 instants — a client entering them in the Event's timezone converts
// before sending.
type promotionBody struct {
	PromotionalPriceCents *int    `json:"promotional_price_cents"`
	StartsAt              *string `json:"starts_at"`
	EndsAt                *string `json:"ends_at"`
}

// SetTicketTypePromotion sets the Ticket Type's Promotion.
//
// @Summary      Set ticket type promotion
// @Description  Sets the promotion on a ticket type. A ticket type holds at most one promotion; setting one where a promotion already exists is rejected.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string         true  "Event ID"
// @Param        ticketTypeId   path      string         true  "Ticket type ID"
// @Param        body           body      promotionBody  true  "Promotional price and window"
// @Success      201            {object}  openapi.EnvelopeTicketTypeDetail
// @Failure      400            {object}  platform.Envelope
// @Failure      401            {object}  platform.Envelope
// @Failure      403            {object}  platform.Envelope
// @Failure      404            {object}  platform.Envelope
// @Failure      409            {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion [post]
func (h *Handler) SetTicketTypePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := promotionPathValues(w, r, reqID)
	if !ok {
		return
	}

	var body promotionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	input, fields := parsePromotion(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	detail, err := h.svc.SetTicketTypePromotion(r.Context(), actorFromRequest(r), eventID, ticketTypeID, input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, detail)
}

// UpdateTicketTypePromotion updates the Ticket Type's existing Promotion.
//
// @Summary      Update ticket type promotion
// @Description  Replaces the promotional price and window of a ticket type's existing promotion.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string         true  "Event ID"
// @Param        ticketTypeId   path      string         true  "Ticket type ID"
// @Param        body           body      promotionBody  true  "Promotional price and window"
// @Success      200            {object}  openapi.EnvelopeTicketTypeDetail
// @Failure      400            {object}  platform.Envelope
// @Failure      401            {object}  platform.Envelope
// @Failure      403            {object}  platform.Envelope
// @Failure      404            {object}  platform.Envelope
// @Failure      409            {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion [patch]
func (h *Handler) UpdateTicketTypePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := promotionPathValues(w, r, reqID)
	if !ok {
		return
	}

	var body promotionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	input, fields := parsePromotion(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	detail, err := h.svc.UpdateTicketTypePromotion(r.Context(), actorFromRequest(r), eventID, ticketTypeID, input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}

// RemoveTicketTypePromotion removes the Ticket Type's Promotion.
//
// @Summary      Remove ticket type promotion
// @Description  Removes a ticket type's promotion, returning it to its list price. The freed slot may be filled again.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketTypeId   path      string  true  "Ticket type ID"
// @Success      200            {object}  openapi.EnvelopeTicketTypeDetail
// @Failure      401            {object}  platform.Envelope
// @Failure      403            {object}  platform.Envelope
// @Failure      404            {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/promotion [delete]
func (h *Handler) RemoveTicketTypePromotion(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, ticketTypeID, ok := promotionPathValues(w, r, reqID)
	if !ok {
		return
	}

	detail, err := h.svc.RemoveTicketTypePromotion(r.Context(), actorFromRequest(r), eventID, ticketTypeID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}

func promotionPathValues(w http.ResponseWriter, r *http.Request, reqID string) (string, string, bool) {
	eventID := strings.TrimSpace(r.PathValue("id"))
	ticketTypeID := strings.TrimSpace(r.PathValue("ticketTypeId"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Code: platform.CodeRequired, Message: "is required"},
		})
		return "", "", false
	}
	if ticketTypeID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "ticket_type_id", Code: platform.CodeRequired, Message: "is required"},
		})
		return "", "", false
	}
	return eventID, ticketTypeID, true
}

// parsePromotion validates everything a Promotion can be judged on without the
// catalog: the price is present and non-negative, the end is present and
// parseable, and the window opens before it closes. Whether the price is below
// the List Price needs the Ticket Type, so it stays a domain rule.
func parsePromotion(body promotionBody) (service.SetPromotionInput, []platform.FieldError) {
	var fields []platform.FieldError

	if body.PromotionalPriceCents == nil {
		fields = append(fields, platform.FieldError{Field: "promotional_price_cents", Code: platform.CodeRequired, Message: "is required"})
	} else if *body.PromotionalPriceCents < 0 {
		fields = append(fields, platform.FieldError{Field: "promotional_price_cents", Code: platform.CodeInvalidNonNegativeInt, Message: "must be zero or greater"})
	}

	startsAt, startFields := parseTimestampField("starts_at", body.StartsAt)
	fields = append(fields, startFields...)

	var endsAt *time.Time
	if body.EndsAt == nil || strings.TrimSpace(*body.EndsAt) == "" {
		fields = append(fields, platform.FieldError{Field: "ends_at", Code: platform.CodeRequired, Message: "is required"})
	} else {
		parsed, endFields := parseTimestampField("ends_at", body.EndsAt)
		fields = append(fields, endFields...)
		endsAt = parsed
	}

	if startsAt != nil && endsAt != nil && !startsAt.Before(*endsAt) {
		fields = append(fields, platform.FieldError{Field: "starts_at", Code: platform.CodeStartAfterEnd, Message: "must be before the end time"})
	}

	if len(fields) > 0 {
		return service.SetPromotionInput{}, fields
	}

	return service.SetPromotionInput{
		PromotionalPriceCents: *body.PromotionalPriceCents,
		StartsAt:              startsAt,
		EndsAt:                *endsAt,
	}, nil
}
