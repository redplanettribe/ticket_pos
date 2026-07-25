package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
)

const (
	defaultPublicEventLimit = 20
	maxPublicEventLimit     = 50
)

// PublicOrganizationSummary is the trust/attribution context shown with an Event.
type PublicOrganizationSummary struct {
	Name    string  `json:"name"`
	Slug    string  `json:"slug"`
	LogoURL *string `json:"logo_url"`
}

// PublicEventCard is a summary row for Storefront listings (global explorer and org page).
type PublicEventCard struct {
	Slug           string                    `json:"slug"`
	Name           string                    `json:"name"`
	StartsAt       *time.Time                `json:"starts_at"`
	EndsAt         *time.Time                `json:"ends_at"`
	Timezone       *string                   `json:"timezone"`
	VenueName      *string                   `json:"venue_name"`
	CoverImageURL  *string                   `json:"cover_image_url"`
	Organization   PublicOrganizationSummary `json:"organization"`
	Currency       string                    `json:"currency"`
	PriceFromCents *int                      `json:"price_from_cents"`
	SoldOut        bool                      `json:"sold_out"`
	Tags           []TagView                 `json:"tags"`
}

// PublicTicketType is a Ticket Type as shown on a Storefront event page.
type PublicTicketType struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	PriceCents  int     `json:"price_cents"`
	Currency    string  `json:"currency"`
	Remaining   int     `json:"remaining"`
	SoldOut     bool    `json:"sold_out"`
}

// PublicEventDetail is the full Storefront event page payload.
type PublicEventDetail struct {
	Slug          string                    `json:"slug"`
	Name          string                    `json:"name"`
	Description   *string                   `json:"description"`
	StartsAt      *time.Time                `json:"starts_at"`
	EndsAt        *time.Time                `json:"ends_at"`
	Timezone      *string                   `json:"timezone"`
	VenueName     *string                   `json:"venue_name"`
	VenueAddress  *string                   `json:"venue_address"`
	CoverImageURL *string                   `json:"cover_image_url"`
	HasEnded      bool                      `json:"has_ended"`
	Organization  PublicOrganizationSummary `json:"organization"`
	Currency      string                    `json:"currency"`
	TicketTypes   []PublicTicketType        `json:"ticket_types"`
	Tags          []TagView                 `json:"tags"`
}

// PublicEventPage is one page of global explorer results.
type PublicEventPage struct {
	Events     []PublicEventCard `json:"events"`
	NextCursor *string           `json:"next_cursor"`
}

// PublicOrganizationEvents is an Organization page: its summary plus split listings.
type PublicOrganizationEvents struct {
	Organization PublicOrganizationSummary `json:"organization"`
	Upcoming     []PublicEventCard         `json:"upcoming"`
	Past         []PublicEventCard         `json:"past"`
}

// PublicEventQuery constrains the global explorer.
type PublicEventQuery struct {
	Query  string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
	// Tags are raw tag names; an Event matches if it carries any of them (OR
	// within the facet). Canonicalized server-side before matching.
	Tags []string
}

// ListDiscoverableEvents returns a page of published, discoverable, not-yet-ended
// Events across all Organizations, soonest first.
func (s *Service) ListDiscoverableEvents(ctx context.Context, q PublicEventQuery) (*PublicEventPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultPublicEventLimit
	}
	if limit > maxPublicEventLimit {
		limit = maxPublicEventLimit
	}

	cursorStartsAt, cursorID := decodeCursor(q.Cursor)

	rows, err := s.repo.ListDiscoverableEvents(ctx, repository.PublicEventFilter{
		Query:          strings.TrimSpace(q.Query),
		From:           q.From,
		To:             q.To,
		Now:            s.now(),
		Limit:          limit + 1,
		CursorStartsAt: cursorStartsAt,
		CursorID:       cursorID,
		TagKeys:        canonicalTagKeys(q.Tags),
	})
	if err != nil {
		return nil, err
	}

	pageRows := rows
	if len(rows) > limit {
		pageRows = rows[:limit]
	}
	tagViews, err := s.tagViewsByEventIDs(ctx, eventIDsOf(pageRows))
	if err != nil {
		return nil, err
	}

	page := &PublicEventPage{Events: make([]PublicEventCard, 0, limit)}
	for i := range rows {
		if i == limit {
			last := rows[limit-1]
			cursor := encodeCursor(last.StartsAt.Time, last.ID)
			page.NextCursor = &cursor
			break
		}
		page.Events = append(page.Events, s.toPublicEventCard(&rows[i], tagViewsFor(tagViews, rows[i].ID)))
	}
	return page, nil
}

// eventIDsOf collects the Event IDs from a set of public rows.
func eventIDsOf(rows []repository.PublicEventRow) []string {
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids
}

// GetOrganizationEvents returns an Organization's public profile with its
// discoverable Events split into upcoming and past.
func (s *Service) GetOrganizationEvents(ctx context.Context, orgSlug string) (*PublicOrganizationEvents, error) {
	orgSlug = strings.ToLower(strings.TrimSpace(orgSlug))
	org, err := s.repo.GetPublicOrganizationBySlug(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, catalog.ErrOrganizationNotFound()
	}

	now := s.now()
	rows, err := s.repo.ListDiscoverableEventsByOrganization(ctx, org.ID, now)
	if err != nil {
		return nil, err
	}

	tagViews, err := s.tagViewsByEventIDs(ctx, eventIDsOf(rows))
	if err != nil {
		return nil, err
	}
	result := &PublicOrganizationEvents{
		Organization: s.publicOrgSummary(org.Name, org.Slug, org.LogoImageKey),
		Upcoming:     make([]PublicEventCard, 0),
		Past:         make([]PublicEventCard, 0),
	}
	// rows are ascending by start; past is shown most-recent first.
	for i := range rows {
		card := s.toPublicEventCard(&rows[i], tagViewsFor(tagViews, rows[i].ID))
		if eventEnded(&rows[i], now) {
			result.Past = append([]PublicEventCard{card}, result.Past...)
		} else {
			result.Upcoming = append(result.Upcoming, card)
		}
	}
	return result, nil
}

// GetPublicEvent returns the Storefront event page for a published Event,
// reachable by direct link regardless of discoverability.
func (s *Service) GetPublicEvent(ctx context.Context, orgSlug, eventSlug string) (*PublicEventDetail, error) {
	orgSlug = strings.ToLower(strings.TrimSpace(orgSlug))
	eventSlug = strings.ToLower(strings.TrimSpace(eventSlug))

	now := s.now()
	row, err := s.repo.GetPublishedEventBySlug(ctx, orgSlug, eventSlug, now)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, catalog.ErrEventNotFound()
	}

	types, err := s.repo.ListTicketTypesByEventID(ctx, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}
	// Live Capacity Holds count against what the Storefront advertises as
	// remaining (ADR 0013): tickets pending Payments speak for are not for sale
	// until those Payments settle or their holds lapse.
	held, err := s.repo.LiveCapacityHolds(ctx, row.ID, now)
	if err != nil {
		return nil, err
	}

	tags, err := s.repo.ListEventTags(ctx, row.ID)
	if err != nil {
		return nil, err
	}

	detail := &PublicEventDetail{
		Slug:          row.Slug,
		Name:          row.Name,
		Description:   nullStringPtr(row.Description),
		CoverImageURL: s.coverURL(row.CoverImageKey),
		HasEnded:      eventEnded(row, now),
		Organization:  s.publicOrgSummary(row.OrgName, row.OrgSlug, row.OrgLogoKey),
		Currency:      row.OrgCurrency,
		TicketTypes:   make([]PublicTicketType, 0, len(types)),
		Tags:          toTagViews(tags),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time
		detail.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time
		detail.EndsAt = &t
	}
	detail.Timezone = nullStringPtr(row.Timezone)
	detail.VenueName = nullStringPtr(row.VenueName)
	detail.VenueAddress = nullStringPtr(row.VenueAddress)

	for _, tt := range types {
		remaining := tt.Capacity - tt.SoldCount - held[tt.ID]
		if remaining < 0 {
			remaining = 0
		}
		detail.TicketTypes = append(detail.TicketTypes, PublicTicketType{
			Name:        tt.Name,
			Description: nullStringPtr(tt.Description),
			PriceCents:  tt.PriceCents,
			Currency:    row.OrgCurrency,
			Remaining:   remaining,
			SoldOut:     remaining == 0,
		})
	}
	return detail, nil
}

func (s *Service) toPublicEventCard(row *repository.PublicEventRow, tags []TagView) PublicEventCard {
	card := PublicEventCard{
		Slug:          row.Slug,
		Name:          row.Name,
		VenueName:     nullStringPtr(row.VenueName),
		Timezone:      nullStringPtr(row.Timezone),
		CoverImageURL: s.coverURL(row.CoverImageKey),
		Organization:  s.publicOrgSummary(row.OrgName, row.OrgSlug, row.OrgLogoKey),
		Currency:      row.OrgCurrency,
		Tags:          tags,
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time
		card.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time
		card.EndsAt = &t
	}
	if row.MinPriceCents.Valid {
		v := int(row.MinPriceCents.Int64)
		card.PriceFromCents = &v
	}
	if row.AllSoldOut.Valid {
		card.SoldOut = row.AllSoldOut.Bool
	}
	return card
}

// tagViewsByEventIDs batch-loads Tags for a set of Events and projects them into
// TagViews keyed by Event ID, Preset Tags first. Events with no Tags are absent
// from the map; callers use tagViewsFor to get a stable empty slice.
func (s *Service) tagViewsByEventIDs(ctx context.Context, eventIDs []string) (map[string][]TagView, error) {
	tagsByEvent, err := s.repo.ListTagsByEventIDs(ctx, eventIDs)
	if err != nil {
		return nil, err
	}
	views := make(map[string][]TagView, len(tagsByEvent))
	for id, tags := range tagsByEvent {
		views[id] = toTagViews(tags)
	}
	return views, nil
}

// tagViewsFor returns the Event's TagViews, or an empty (non-nil) slice so the
// field serializes as [] rather than null.
func tagViewsFor(views map[string][]TagView, eventID string) []TagView {
	if v, ok := views[eventID]; ok {
		return v
	}
	return make([]TagView, 0)
}

func (s *Service) publicOrgSummary(name, slug string, logoKey sql.NullString) PublicOrganizationSummary {
	summary := PublicOrganizationSummary{Name: name, Slug: slug}
	if logoKey.Valid && s.storage != nil {
		url := s.storage.PublicURL(logoKey.String)
		summary.LogoURL = &url
	}
	return summary
}

func (s *Service) coverURL(key sql.NullString) *string {
	if key.Valid && s.storage != nil {
		url := s.storage.PublicURL(key.String)
		return &url
	}
	return nil
}

// eventEnded reports whether an Event's end (or start, if no end) is in the past.
func eventEnded(row *repository.PublicEventRow, now time.Time) bool {
	switch {
	case row.EndsAt.Valid:
		return row.EndsAt.Time.Before(now)
	case row.StartsAt.Valid:
		return row.StartsAt.Time.Before(now)
	default:
		return false
	}
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

// decodeCursor parses a keyset cursor. An unparseable cursor is treated as absent
// so a bad value simply starts from the first page rather than erroring.
func decodeCursor(cursor string) (*time.Time, string) {
	if strings.TrimSpace(cursor) == "" {
		return nil, ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ""
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, ""
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, ""
	}
	return &t, parts[1]
}
