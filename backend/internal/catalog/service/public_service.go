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
	})
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
		page.Events = append(page.Events, s.toPublicEventCard(&rows[i]))
	}
	return page, nil
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

	rows, err := s.repo.ListDiscoverableEventsByOrganization(ctx, org.ID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	result := &PublicOrganizationEvents{
		Organization: s.publicOrgSummary(org.Name, org.Slug, org.LogoImageKey),
		Upcoming:     make([]PublicEventCard, 0),
		Past:         make([]PublicEventCard, 0),
	}
	// rows are ascending by start; past is shown most-recent first.
	for i := range rows {
		card := s.toPublicEventCard(&rows[i])
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

	row, err := s.repo.GetPublishedEventBySlug(ctx, orgSlug, eventSlug)
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

	detail := &PublicEventDetail{
		Slug:          row.Slug,
		Name:          row.Name,
		Description:   nullStringPtr(row.Description),
		CoverImageURL: s.coverURL(row.CoverImageKey),
		HasEnded:      eventEnded(row, s.now()),
		Organization:  s.publicOrgSummary(row.OrgName, row.OrgSlug, row.OrgLogoKey),
		Currency:      row.OrgCurrency,
		TicketTypes:   make([]PublicTicketType, 0, len(types)),
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
		remaining := tt.Capacity - tt.SoldCount
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

func (s *Service) toPublicEventCard(row *repository.PublicEventRow) PublicEventCard {
	card := PublicEventCard{
		Slug:          row.Slug,
		Name:          row.Name,
		VenueName:     nullStringPtr(row.VenueName),
		Timezone:      nullStringPtr(row.Timezone),
		CoverImageURL: s.coverURL(row.CoverImageKey),
		Organization:  s.publicOrgSummary(row.OrgName, row.OrgSlug, row.OrgLogoKey),
		Currency:      row.OrgCurrency,
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
