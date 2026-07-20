package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

type setEventTagsBody struct {
	Tags []string `json:"tags"`
}

// SearchTags returns pool Tags matching a query for the staff typeahead.
//
// @Summary      Search tags
// @Description  Searches the shared Tag pool for the staff tag editor, preset tags first.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        q    query     string  false  "Search query"
// @Success      200  {object}  openapi.EnvelopeTagList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/staff/tags [get]
func (h *Handler) SearchTags(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	tags, err := h.svc.SearchTags(r.Context(), query)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, tags)
}

// ListEventTags returns the Tags assigned to an Event.
//
// @Summary      List event tags
// @Description  Lists the tags assigned to a catalog event.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id   path      string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeTagList
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/tags [get]
func (h *Handler) ListEventTags(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	tags, err := h.svc.ListEventTags(r.Context(), actor, eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, tags)
}

// SetEventTags replaces the Tags on an Event, coining Custom Tags as needed.
//
// @Summary      Set event tags
// @Description  Replaces the tags on a catalog event, coining custom tags where none exist. Org Admin only.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string            true  "Event ID"
// @Param        body  body      setEventTagsBody  true  "Tag names"
// @Success      200   {object}  openapi.EnvelopeTagList
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/tags [put]
func (h *Handler) SetEventTags(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "id", Message: "is required"},
		})
		return
	}

	var body setEventTagsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	if body.Tags == nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "tags", Message: "is required"},
		})
		return
	}

	actor := actorFromRequest(r)

	tags, err := h.svc.SetEventTags(r.Context(), actor, eventID, body.Tags)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, tags)
}
