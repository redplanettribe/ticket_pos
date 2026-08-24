package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/affiliates"
)

// AffiliateTrends is everything the affiliate tab's graphs draw, in one
// payload (#413, ADR 0057): the zone and currency to label with, the links to
// build a stable legend from, the stored hourly Page View and Click buckets,
// and the Attributed Sales figures derived beside them at read time.
//
// It is deliberately one response rather than several, for Sales Trends'
// reason: the tab switches metrics and ranges client-side, so everything
// arrives at once and no interaction waits on the network. The whole thing is
// bounded — buckets grow with time × links, links with the organizer's
// patience, and sales figures with the hours anything sold in — so there is no
// pagination to apply.
type AffiliateTrends struct {
	// Timezone is the Event's own zone (UTC where it carries none), the one the
	// sales hours below are bucketed in and the one every axis label should
	// speak.
	Timezone string `json:"timezone"`
	// Currency is the Organization's currency, which net_proceeds_cents is
	// denominated in.
	Currency string `json:"currency"`
	// Links is the Event's WHOLE set of Affiliate Links, newest first,
	// deactivated ones included, so a legend built from it is stable and a
	// link's series never vanishes just because it was taken out of
	// circulation.
	Links []AffiliateTrendsLink `json:"links"`
	// ViewBuckets are the stored rows of ADR 0057, exactly as stored: UTC
	// hours, each carrying a count and nothing else. A nil link_id is the
	// Event's whole-page bucket — every load counts there — and a link's bucket
	// counts the subset that arrived through it, so "all Page Views" needs no
	// series of its own and organic traffic is the visible gap between the two.
	// Sparse: an hour nothing happened in has no row, and the client zero-fills.
	ViewBuckets []AffiliateTrendsViewBucket `json:"view_buckets"`
	// SalesBuckets is the per-hour Attributed Sales history, derived at read
	// time so a Sale Reversal retroactively edits it. Null — absent, not empty —
	// on an Event that registers externally: such a link can never attribute a
	// sale, so its success is not measured in sales at all, exactly as the
	// tab's own columns already say (#213). Empty means measured and nothing
	// sold; null means not measured here.
	SalesBuckets []AffiliateTrendsSalesBucket `json:"sales_buckets"`
}

// AffiliateTrendsLink is one Affiliate Link as the legend needs it. Active
// rides along so a deactivated link can be drawn as such; everything else
// about a link lives on the tab's own list.
type AffiliateTrendsLink struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// AffiliateTrendsViewBucket is one stored hourly bucket: a UTC hour, the link
// it counts for (null for the whole page), and how many loads landed in it.
// Loads, never people: every figure downstream is a floor, not a measurement
// (ADR 0022).
type AffiliateTrendsViewBucket struct {
	Hour   time.Time `json:"hour"`
	LinkID *string   `json:"link_id"`
	Views  int64     `json:"views"`
}

// AffiliateTrendsSalesBucket is one derived hour of one link's Attributed
// Sales. Hour is a civil hour in the Event's timezone, YYYY-MM-DDTHH:00 — a
// label on a local clock, not an instant, so it travels as a string like the
// Sales Trends day does.
type AffiliateTrendsSalesBucket struct {
	Hour             string `json:"hour"`
	LinkID           string `json:"link_id"`
	Sales            int    `json:"sales"`
	Tickets          int    `json:"tickets"`
	NetProceedsCents int    `json:"net_proceeds_cents"`
}

// EventAffiliateTrends returns the Event's whole affiliate trends payload.
// Read-only, scoped to the acting Member's Organization, and gated at the
// route exactly as the rest of the affiliate-links resource is (Org Admin and
// Event Owner only).
func (s *Service) EventAffiliateTrends(ctx context.Context, actor ActorContext, eventID string) (*AffiliateTrends, error) {
	target, err := s.repo.GetLinkTarget(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, affiliates.ErrEventNotFound()
	}

	links, err := s.repo.ListByEventID(ctx, eventID)
	if err != nil {
		return nil, err
	}
	legend := make([]AffiliateTrendsLink, 0, len(links))
	for _, link := range links {
		legend = append(legend, AffiliateTrendsLink{ID: link.ID, Name: link.Name, Active: link.Active})
	}

	viewBuckets, err := s.repo.ViewBuckets(ctx, eventID)
	if err != nil {
		return nil, err
	}
	views := make([]AffiliateTrendsViewBucket, 0, len(viewBuckets))
	for _, bucket := range viewBuckets {
		views = append(views, AffiliateTrendsViewBucket{Hour: bucket.Hour, LinkID: bucket.LinkID, Views: bucket.Views})
	}

	// The Event's own zone, resolved the way every sales surface resolves it
	// (UTC when unset or unrecognised), and its RESOLVED name is what the query
	// buckets by, so Go and Postgres cannot disagree about which hour a sale
	// falls in.
	loc := resolveEventLocation(target.Timezone)

	// nil stays nil on an externally registered Event: not measured here, and
	// the JSON says so with a null the client cannot mistake for a quiet hour.
	var salesBuckets []AffiliateTrendsSalesBucket
	if attributesSales(*target) {
		derived, err := s.repo.AttributedSalesByHour(ctx, eventID, loc.String())
		if err != nil {
			return nil, err
		}
		salesBuckets = make([]AffiliateTrendsSalesBucket, 0, len(derived))
		for _, bucket := range derived {
			salesBuckets = append(salesBuckets, AffiliateTrendsSalesBucket{
				Hour:             bucket.Hour,
				LinkID:           bucket.LinkID,
				Sales:            bucket.Sales,
				Tickets:          bucket.Tickets,
				NetProceedsCents: bucket.NetProceedsCents,
			})
		}
	}

	return &AffiliateTrends{
		Timezone:     loc.String(),
		Currency:     target.Currency,
		Links:        legend,
		ViewBuckets:  views,
		SalesBuckets: salesBuckets,
	}, nil
}

// resolveEventLocation resolves an Event timezone name to a *time.Location,
// defaulting to UTC when the timezone is unset or unrecognized — the same rule
// the sales surfaces apply, restated here rather than imported so the two
// modules stay uncoupled over one three-line policy.
func resolveEventLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}
