package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// Handler exposes HTTP endpoints for the catalog domain.
type Handler struct {
	svc *service.Service
}

// New returns a catalog HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type createEventBody struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type updateEventBody struct {
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	StartsAt      *string `json:"starts_at"`
	EndsAt        *string `json:"ends_at"`
	Timezone      *string `json:"timezone"`
	VenueName     *string `json:"venue_name"`
	VenueAddress  *string `json:"venue_address"`
	Description   *string `json:"description"`
	CoverImageKey *string `json:"cover_image_key"`
	// CoverVideoKey attaches or clears the Cover Video. Absent leaves it alone,
	// an empty string clears it, mirroring cover_image_key.
	CoverVideoKey *string `json:"cover_video_key"`
	// FeeHandling is the Event's Fee Handling mode. Absent means "leave it
	// alone": a form that does not know about the switch must not reset it.
	FeeHandling *string `json:"fee_handling"`
}

type coverUploadURLBody struct {
	ContentType string  `json:"content_type"`
	FileName    *string `json:"file_name"`
}

type videoUploadURLBody struct {
	ContentType string `json:"content_type"`
	// FileName is accepted for symmetry with the cover upload request and to let
	// clients send what the user picked, but it never shapes the key: a Cover
	// Video is always an MP4, so the extension is fixed.
	FileName *string `json:"file_name"`
}

func actorFromRequest(r *http.Request) service.ActorContext {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.ActorContext{
		MemberID:       member.MemberID,
		OrganizationID: member.OrganizationID,
	}
}

// ListEvents returns Events for the organization.
//
// @Summary      List events
// @Description  Lists catalog events for the active organization.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  openapi.EnvelopeEventList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/events [get]
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	actor := actorFromRequest(r)

	events, err := h.svc.ListEvents(r.Context(), actor)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, events)
}

// CreateEvent creates a draft Event.
//
// @Summary      Create event
// @Description  Creates a draft catalog event.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      createEventBody  true  "Event details"
// @Success      201   {object}  openapi.EnvelopeEventDetail
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events [post]
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body createEventBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateCreateEvent(body.Name, body.Slug); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.CreateEvent(r.Context(), actor, service.CreateEventInput{
		Name: body.Name,
		Slug: body.Slug,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, event)
}

// GetEvent returns an Event by ID.
//
// @Summary      Get event
// @Description  Returns a catalog event by ID.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeEventDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id} [get]
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.GetEvent(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, event)
}

// UpdateEvent updates an Event.
//
// @Summary      Update event
// @Description  Updates catalog event fields.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string           true  "Event ID"
// @Param        body  body      updateEventBody  true  "Event fields"
// @Success      200   {object}  openapi.EnvelopeEventDetail
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id} [patch]
func (h *Handler) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body updateEventBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	input, fields := parseUpdateEvent(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.UpdateEvent(r.Context(), actor, eventID, input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, event)
}

// DeleteEvent deletes a draft Event.
//
// @Summary      Delete event
// @Description  Deletes a draft catalog event.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeMessage
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id} [delete]
func (h *Handler) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	if err := h.svc.DeleteEvent(r.Context(), actor, eventID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Event deleted.",
	})
}

// PublishEvent publishes a draft Event when requirements are met.
//
// @Summary      Publish event
// @Description  Publishes a draft catalog event when required fields and ticket types are present.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeEventDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/publish [post]
func (h *Handler) PublishEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.PublishEvent(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, event)
}

// CancelEvent cancels a published Event.
//
// @Summary      Cancel event
// @Description  Cancels a published catalog event.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeEventDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/cancel [post]
func (h *Handler) CancelEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.CancelEvent(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, event)
}

type setDiscoverableBody struct {
	Discoverable *bool `json:"discoverable"`
}

// SetEventDiscoverable toggles whether a published Event is listed publicly.
//
// @Summary      Set event discoverability
// @Description  Sets whether a published event is listed in public discovery surfaces. Available to any organization member.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string               true  "Event ID"
// @Param        body  body      setDiscoverableBody  true  "Discoverability flag"
// @Success      200   {object}  openapi.EnvelopeEventDetail
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/discoverable [put]
func (h *Handler) SetEventDiscoverable(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body setDiscoverableBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if body.Discoverable == nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "discoverable", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	event, err := h.svc.SetEventDiscoverable(r.Context(), actor, eventID, *body.Discoverable)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, event)
}

// CreateCoverUploadURL returns a presigned URL for uploading an event cover image.
//
// @Summary      Create cover upload URL
// @Description  Returns a presigned PUT URL for uploading an event cover image.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string              true  "Event ID"
// @Param        body  body      coverUploadURLBody  true  "Upload details"
// @Success      200   {object}  openapi.EnvelopeCoverUploadURL
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/cover-upload-url [post]
func (h *Handler) CreateCoverUploadURL(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body coverUploadURLBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(body.ContentType))
	if contentType == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Message: "is required"},
		})
		return
	}
	if !storage.CoverContentTypeAllowed(contentType) {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Message: "must be image/jpeg, image/png, or image/webp"},
		})
		return
	}

	fileName := ""
	if body.FileName != nil {
		fileName = strings.TrimSpace(*body.FileName)
	}

	actor := actorFromRequest(r)

	result, err := h.svc.CreateCoverUploadURL(r.Context(), actor, eventID, service.CreateCoverUploadURLInput{
		ContentType: contentType,
		FileName:    fileName,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// CreateVideoUploadURL returns a presigned URL for uploading an event Cover Video.
//
// @Summary      Create cover video upload URL
// @Description  Returns a presigned PUT URL for uploading an event cover video (MP4 only).
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string              true  "Event ID"
// @Param        body  body      videoUploadURLBody  true  "Upload details"
// @Success      200   {object}  openapi.EnvelopeCoverUploadURL
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/video-upload-url [post]
func (h *Handler) CreateVideoUploadURL(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body videoUploadURLBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(body.ContentType))
	if contentType == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Message: "is required"},
		})
		return
	}
	if !storage.VideoContentTypeAllowed(contentType) {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "content_type", Message: "must be video/mp4"},
		})
		return
	}

	actor := actorFromRequest(r)

	result, err := h.svc.CreateVideoUploadURL(r.Context(), actor, eventID, service.CreateVideoUploadURLInput{
		ContentType: contentType,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

type createTicketTypeBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	PriceCents  int     `json:"price_cents"`
	Capacity    int     `json:"capacity"`
}

type updateTicketTypeBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	PriceCents  int     `json:"price_cents"`
	Capacity    int     `json:"capacity"`
	SortOrder   int     `json:"sort_order"`
}

// ListTicketTypes returns Ticket Types for an Event.
//
// @Summary      List ticket types
// @Description  Lists ticket types for a catalog event.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeTicketTypeList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types [get]
func (h *Handler) ListTicketTypes(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	types, err := h.svc.ListTicketTypes(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, types)
}

// CreateTicketType creates a Ticket Type on an Event.
//
// @Summary      Create ticket type
// @Description  Creates a ticket type on a catalog event.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string               true  "Event ID"
// @Param        body  body      createTicketTypeBody  true  "Ticket type details"
// @Success      201   {object}  openapi.EnvelopeTicketTypeDetail
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types [post]
func (h *Handler) CreateTicketType(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body createTicketTypeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateTicketType(body.Name, body.PriceCents, body.Capacity); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorFromRequest(r)

	created, err := h.svc.CreateTicketType(r.Context(), actor, eventID, service.CreateTicketTypeInput{
		Name:        body.Name,
		Description: body.Description,
		PriceCents:  body.PriceCents,
		Capacity:    body.Capacity,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, created)
}

// UpdateTicketType updates a Ticket Type on an Event.
//
// @Summary      Update ticket type
// @Description  Updates a ticket type on a catalog event.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string               true  "Event ID"
// @Param        ticketTypeId   path      string               true  "Ticket type ID"
// @Param        body           body      updateTicketTypeBody  true  "Ticket type fields"
// @Success      200            {object}  openapi.EnvelopeTicketTypeDetail
// @Failure      400            {object}  platform.Envelope
// @Failure      401            {object}  platform.Envelope
// @Failure      403            {object}  platform.Envelope
// @Failure      404            {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId} [patch]
func (h *Handler) UpdateTicketType(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	ticketTypeID := strings.TrimSpace(r.PathValue("ticketTypeId"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}
	if ticketTypeID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "ticket_type_id", Message: "is required"},
		})
		return
	}

	var body updateTicketTypeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if fields := validateTicketType(body.Name, body.PriceCents, body.Capacity); len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	actor := actorFromRequest(r)

	updated, err := h.svc.UpdateTicketType(r.Context(), actor, eventID, ticketTypeID, service.UpdateTicketTypeInput{
		Name:        body.Name,
		Description: body.Description,
		PriceCents:  body.PriceCents,
		Capacity:    body.Capacity,
		SortOrder:   body.SortOrder,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, updated)
}

// DeleteTicketType removes a Ticket Type from a draft Event.
//
// @Summary      Delete ticket type
// @Description  Deletes a ticket type when the parent event is a draft.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id             path      string  true  "Event ID"
// @Param        ticketTypeId   path      string  true  "Ticket type ID"
// @Success      200            {object}  openapi.EnvelopeMessage
// @Failure      401            {object}  platform.Envelope
// @Failure      403            {object}  platform.Envelope
// @Failure      404            {object}  platform.Envelope
// @Failure      409            {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/ticket-types/{ticketTypeId} [delete]
func (h *Handler) DeleteTicketType(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	ticketTypeID := strings.TrimSpace(r.PathValue("ticketTypeId"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}
	if ticketTypeID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "ticket_type_id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	if err := h.svc.DeleteTicketType(r.Context(), actor, eventID, ticketTypeID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, map[string]string{
		"message": "Ticket type deleted.",
	})
}

func validateTicketType(name string, priceCents, capacity int) []platform.FieldError {
	var fields []platform.FieldError
	if strings.TrimSpace(name) == "" {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if priceCents < 0 {
		fields = append(fields, platform.FieldError{Field: "price_cents", Message: "must be zero or greater"})
	}
	if capacity <= 0 {
		fields = append(fields, platform.FieldError{Field: "capacity", Message: "must be greater than zero"})
	}
	return fields
}

func validateCreateEvent(name, slug string) []platform.FieldError {
	name = strings.TrimSpace(name)
	slug = strings.ToLower(strings.TrimSpace(slug))
	var fields []platform.FieldError
	if name == "" {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if slug == "" {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "is required"})
	} else if !slugPattern.MatchString(slug) {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "must be URL-safe (lowercase letters, numbers, and hyphens)"})
	}
	return fields
}

func parseUpdateEvent(body updateEventBody) (service.UpdateEventInput, []platform.FieldError) {
	var fields []platform.FieldError

	name := strings.TrimSpace(body.Name)
	slug := strings.ToLower(strings.TrimSpace(body.Slug))
	if name == "" {
		fields = append(fields, platform.FieldError{Field: "name", Message: "is required"})
	}
	if slug == "" {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "is required"})
	} else if !slugPattern.MatchString(slug) {
		fields = append(fields, platform.FieldError{Field: "slug", Message: "must be URL-safe (lowercase letters, numbers, and hyphens)"})
	}

	var startsAt, endsAt *time.Time
	if body.StartsAt != nil {
		if strings.TrimSpace(*body.StartsAt) == "" {
			startsAt = nil
		} else {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.StartsAt))
			if err != nil {
				fields = append(fields, platform.FieldError{Field: "starts_at", Message: "must be a valid RFC3339 timestamp"})
			} else {
				startsAt = &t
			}
		}
	}
	if body.EndsAt != nil {
		if strings.TrimSpace(*body.EndsAt) == "" {
			endsAt = nil
		} else {
			t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.EndsAt))
			if err != nil {
				fields = append(fields, platform.FieldError{Field: "ends_at", Message: "must be a valid RFC3339 timestamp"})
			} else {
				endsAt = &t
			}
		}
	}
	if startsAt != nil && endsAt != nil && endsAt.Before(*startsAt) {
		fields = append(fields, platform.FieldError{Field: "ends_at", Message: "must be after start time"})
	}

	if body.Timezone != nil && strings.TrimSpace(*body.Timezone) != "" {
		if _, err := time.LoadLocation(strings.TrimSpace(*body.Timezone)); err != nil {
			fields = append(fields, platform.FieldError{Field: "timezone", Message: "must be a valid IANA timezone"})
		}
	}

	var feeHandling *sales.FeeHandling
	if body.FeeHandling != nil {
		parsed, ok := sales.ParseFeeHandling(strings.TrimSpace(*body.FeeHandling))
		if !ok {
			fields = append(fields, platform.FieldError{Field: "fee_handling", Message: "must be pass_on or absorb"})
		} else {
			feeHandling = &parsed
		}
	}

	if len(fields) > 0 {
		return service.UpdateEventInput{}, fields
	}

	return service.UpdateEventInput{
		Name:          name,
		Slug:          slug,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		Timezone:      body.Timezone,
		VenueName:     body.VenueName,
		VenueAddress:  body.VenueAddress,
		Description:   body.Description,
		CoverImageKey: body.CoverImageKey,
		CoverVideoKey: body.CoverVideoKey,
		FeeHandling:   feeHandling,
	}, nil
}
