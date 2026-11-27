package service

import (
	"context"
	"sort"
	"time"

	"github.com/peter/ticket_pos/backend/internal/affiliates"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// eventRecapAffiliateLinkLimit is the most Affiliate Links the Event Recap
// lists: the busiest three. A revisitable ruling of #887, not one the spec's
// author made explicitly.
const eventRecapAffiliateLinkLimit = 3

// EventRecapReach is the attention one Event's page drew before the Event was
// over, for its Event Recap (spec #884, #887): its Page Views, and its
// busiest Affiliate Links with their Clicks and Attributed Sales.
//
// NO NEW READ. The buckets are the stored rows Reach Trends draws
// (repo.ViewBuckets) and the links are the list the Reach surface shows
// (repo.ListByEventID), so the mail agrees with the surface it links to.
//
// CUT AT THE OVER INSTANT, BY THE HOUR. A bucket counts when its hour BEGAN
// before overAt. The hourly bucket is the floor of this data (ADR 0057), so
// the cut cannot be finer: an Event over at 23:30 counts the whole of the
// 23:00 bucket, the loads of its last half hour with it, and one over at
// 23:00 exactly counts nothing from that hour on.
//
//   - Page Views are the whole-page buckets alone, so a load that came
//     through an Affiliate Link is counted once.
//   - A link's Clicks are its own buckets, cut the same way. This is not the
//     link's lifetime click_count, which goes on counting after the Event.
//   - A link's Attributed Sales are the figure the Affiliate Links list
//     states: its active attributed Ticket Sales, a reversed one dropped.
//     They are not cut at the over instant. Nil on an Event with External
//     Registration, as on that list.
//
// THE BUSIEST LINKS are the ones with the most Clicks before overAt, at most
// three, and only links that drew a Click at all, as the Reach Trends surface
// lists only what has something to say: a link nobody followed has nothing.
// Equal Clicks are ordered by Attributed Sales, most first, then by name.
//
// The two reads are separate statements, like the sales figures' beside them.
func (s *Service) EventRecapReach(ctx context.Context, organizationID, eventID string, overAt time.Time) (*platform.EventRecapReach, error) {
	target, err := s.repo.GetLinkTarget(ctx, organizationID, eventID)
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
	buckets, err := s.repo.ViewBuckets(ctx, eventID)
	if err != nil {
		return nil, err
	}

	reach := &platform.EventRecapReach{}
	clicks := make(map[string]int, len(links))
	for _, bucket := range buckets {
		if !bucket.Hour.Before(overAt) {
			continue
		}
		if bucket.LinkID == nil {
			reach.PageViews += int(bucket.Views)
			continue
		}
		clicks[*bucket.LinkID] += int(bucket.Views)
	}

	sellsHere := attributesSales(*target)
	for _, link := range links {
		if clicks[link.ID] == 0 {
			continue
		}
		row := platform.EventRecapAffiliateLink{Name: link.Name, Clicks: clicks[link.ID]}
		if sellsHere {
			sales := link.SalesCount
			row.AttributedSales = &sales
		}
		reach.AffiliateLinks = append(reach.AffiliateLinks, row)
	}
	sort.SliceStable(reach.AffiliateLinks, func(i, j int) bool {
		a, b := reach.AffiliateLinks[i], reach.AffiliateLinks[j]
		if a.Clicks != b.Clicks {
			return a.Clicks > b.Clicks
		}
		if a.AttributedSales != nil && b.AttributedSales != nil && *a.AttributedSales != *b.AttributedSales {
			return *a.AttributedSales > *b.AttributedSales
		}
		return a.Name < b.Name
	})
	if len(reach.AffiliateLinks) > eventRecapAffiliateLinkLimit {
		reach.AffiliateLinks = reach.AffiliateLinks[:eventRecapAffiliateLinkLimit]
	}
	return reach, nil
}
