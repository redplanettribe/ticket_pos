package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog/service"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
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
// @Param        tags    query     string  false  "Comma-separated tag names; events carrying any of them match"
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
				{Field: "limit", Code: platform.CodeInvalidNonNegativeInt, Message: "must be a non-negative integer"},
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
		Tags:   parseCSV(q.Get("tags")),
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, page)
}

// ListPublicTags returns the Preset Tags available as explorer filter chips.
//
// @Summary      List public filter tags
// @Description  Lists preset tags carried by at least one discoverable upcoming event, for the storefront explorer chip bar. Independent of any q/date filter.
// @Tags         public
// @Produce      json
// @Success      200  {object}  openapi.EnvelopeTagList
// @Failure      400  {object}  platform.Envelope
// @Router       /api/v1/public/tags [get]
func (h *Handler) ListPublicTags(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	tags, err := h.svc.ListAvailablePresetTags(r.Context())
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, tags)
}

// parseCSV splits a comma-separated query param into trimmed, non-empty values.
func parseCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			values = append(values, v)
		}
	}
	return values
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
			{Field: "slug", Code: platform.CodeRequired, Message: "is required"},
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
// A Customer Session presented in Authorization is optional and adds one thing:
// that Customer's own holdings per Ticket Type. It never gates the read — the
// event page is public and an absent, expired or garbage token is an ordinary
// anonymous visitor, never a 401 (see OptionalCustomerSession on this route).
//
// @Summary      Get public event
// @Description  Returns a published event with its ticket types for the Storefront event page. A Customer Session presented in Authorization is optional and changes nothing but one field: each ticket type then carries already_held, how many of it that Customer already holds — their active Ticket Sales plus their live Capacity Holds, the same count begin-checkout refuses on, so the picker can bound itself at max(0, max_per_customer - already_held) and a ticket type whose allowance is spent can say so instead of claiming to be sold out (ADR 0025). An absent, expired or invalid token reads the event as an anonymous visitor rather than failing, and already_held is then null — null means "we do not know who is asking", which is not the same statement as 0. already_held may exceed max_per_customer, because lowering a Purchase Limit is never retroactive. No unauthenticated lookup of anybody's holdings exists: the count comes from the session and from nothing in the URL. The Event also states its Tickets Sold as tickets_sold: an integer at or above the floor of 5, null beneath it and null on an Event with External Registration; 0 is never sent (ADR 0072). A session also fills surrenderable_free_tickets: how many free Tickets this Customer could give up on this Event through an Upgrade (ADR 0074) — their own accepted Self-held Tickets, each on an active Online Sale of this Event carrying that one Ticket and nothing else, sold at zero. It is a COUNT and not a verdict: an Upgrade is offered only where exactly one free Ticket is in play counting the basket too, which this read cannot see, so the client adds the free Tickets in its cart and offers only on a total of exactly one — the same arithmetic the commit repeats inside its own transaction. Null carries already_held's meaning, "we do not know who is asking", and must not be read as 0; 0 is a real answer to a Customer we can identify, and is also what an Event with External Registration and a dark Ticket Assignment build answer.
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
			{Field: "slug", Code: platform.CodeRequired, Message: "organization and event slug are required"},
		})
		return
	}

	detail, err := h.svc.GetPublicEvent(r.Context(), slug, eventSlug, publicEventViewer(r))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, detail)
}

// publicEventViewer is who the Event page may be told about their own holdings:
// the Customer whose full Customer Session the request carried, or nobody.
//
// The address comes from the session and only from the session, which is the
// whole of the privacy property (ADR 0025, #168): there is no email in the path,
// the query or a body here, so there is nothing a caller could ask about but
// themselves, and no lookup for anyone to point at a stranger's address.
//
// A Confirmation Link session is deliberately NOT enough, exactly as it is not
// enough to self-assert a Tax ID at checkout: it is minted from a token in an
// email that gets forwarded, not from Proof of Email Ownership, and it exists to
// show one Ticket Sale. Whoever a confirmation was forwarded to must not learn
// what the buyer holds.
func publicEventViewer(r *http.Request) *service.PublicEventViewer {
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok || session.TicketSaleID != "" {
		return nil
	}
	return &service.PublicEventViewer{Email: session.Email}
}

func parseOptionalTime(raw, field string, fields *[]platform.FieldError) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidTimestamp, Message: "must be a valid RFC3339 timestamp"})
		return nil, false
	}
	return &t, true
}
