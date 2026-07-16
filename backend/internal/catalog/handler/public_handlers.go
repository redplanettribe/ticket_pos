package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// ListPublicEvents returns discoverable, published, upcoming Events across all
// Organizations for the Storefront global explorer.
//
// @Summary      List public events
// @Description  Lists discoverable, published, not-yet-ended events across all organizations, soonest first.
// @Tags         public
// @Produce      json
// @Param        q       query     string  false  "Search over event and organization name"
// @Param        from    query     string  false  "Only events starting on or after this RFC3339 time"
// @Param        to      query     string  false  "Only events starting on or before this RFC3339 time"
// @Param        limit   query     int     false  "Page size (default 20, max 50)"
// @Param        cursor  query     string  false  "Pagination cursor from a previous response"
// @Success      200     {object}  openapi.EnvelopePublicEventPage
// @Failure      400     {object}  platform.Envelope
// @Router       /api/v1/public/events [get]
func (h *Handler) ListPublicEvents(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	q := r.URL.Query()

	var fields []platform.FieldError
	from, ok := parseOptionalTime(q.Get("from"), "from", &fields)
	if !ok {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	to, ok := parseOptionalTime(q.Get("to"), "to", &fields)
	if !ok {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
				{Field: "limit", Message: "must be a non-negative integer"},
			})
			return
		}
		limit = parsed
	}

	page, err := h.svc.ListDiscoverableEvents(r.Context(), service.PublicEventQuery{
		Query:  q.Get("q"),
		From:   from,
		To:     to,
		Limit:  limit,
		Cursor: q.Get("cursor"),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}

// GetPublicOrganizationEvents returns an Organization's public profile and its
// discoverable Events split into upcoming and past.
//
// @Summary      Get public organization events
// @Description  Returns an organization's discoverable events split into upcoming and past.
// @Tags         public
// @Produce      json
// @Param        slug  path      string  true  "Organization slug"
// @Success      200   {object}  openapi.EnvelopePublicOrganizationEvents
// @Failure      404   {object}  platform.Envelope
// @Router       /api/v1/public/organizations/{slug}/events [get]
func (h *Handler) GetPublicOrganizationEvents(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	slug := strings.TrimSpace(r.PathValue("slug"))
	if slug == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "slug", Message: "is required"},
		})
		return
	}

	result, err := h.svc.GetOrganizationEvents(r.Context(), slug)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// GetPublicEvent returns the Storefront event page for a published Event.
//
// @Summary      Get public event
// @Description  Returns a published event with its ticket types for the Storefront event page.
// @Tags         public
// @Produce      json
// @Param        slug        path      string  true  "Organization slug"
// @Param        eventSlug   path      string  true  "Event slug"
// @Success      200         {object}  openapi.EnvelopePublicEventDetail
// @Failure      404         {object}  platform.Envelope
// @Router       /api/v1/public/organizations/{slug}/events/{eventSlug} [get]
func (h *Handler) GetPublicEvent(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	slug := strings.TrimSpace(r.PathValue("slug"))
	eventSlug := strings.TrimSpace(r.PathValue("eventSlug"))
	if slug == "" || eventSlug == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "slug", Message: "organization and event slug are required"},
		})
		return
	}

	detail, err := h.svc.GetPublicEvent(r.Context(), slug, eventSlug)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}

func parseOptionalTime(raw, field string, fields *[]platform.FieldError) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Message: "must be a valid RFC3339 timestamp"})
		return nil, false
	}
	return &t, true
}
