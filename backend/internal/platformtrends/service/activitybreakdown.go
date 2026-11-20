package service

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The breakdown of the two activity figures (#819, ADR 0080).
//
// Active Organizations is listed: the Organizations active in the bucket,
// each with its cause. It is read from what the daily close counts a day
// from, the Ticket Sales recorded that day and the Organization markers, and
// is the one read of Platform Trends that touches a marker at request time.
//
// Active Customers is never listed. It is the three counts by kind the daily
// close froze, read from the frozen totals alone: no Customer marker is read
// here, and no read that returns a Customer is reachable from a request.

// breakDownActiveOrganizations lists the Organizations active in the bucket,
// each with its cause and the number of days of the bucket it was active on,
// so that they add up to the daily counts of the bucket.
//
// The sale half is every day's. The Member half is read for the days of the
// bucket it is known for (memberHalfKnownFrom), and a bucket with a day
// before that says it holds the sale half only.
func (s *Service) breakDownActiveOrganizations(ctx context.Context, breakdown *PlatformTrendBreakdown, bucket Bucket) error {
	type activity struct {
		days         map[string]struct{}
		sale, member bool
	}
	active := map[string]*activity{}
	on := func(organizationID, day string) *activity {
		organization := active[organizationID]
		if organization == nil {
			organization = &activity{days: map[string]struct{}{}}
			active[organizationID] = organization
		}
		organization.days[day] = struct{}{}
		return organization
	}

	from, until := bucket.window()
	recorded, err := s.sales.PlatformOrganizationsWithSalesRecordedByDay(ctx, from, until, platform.EcuadorTimeZone)
	if err != nil {
		return err
	}
	for _, sale := range recorded {
		on(sale.OrganizationID, sale.Day).sale = true
	}

	knownFrom, err := s.memberHalfKnownFrom(ctx)
	if err != nil {
		return err
	}
	breakdown.SaleHalfOnly = knownFrom == nil || knownFrom.After(bucket.FirstDay)
	if knownFrom != nil && !knownFrom.After(bucket.LastDay) {
		firstMarked := bucket.FirstDay
		if knownFrom.After(firstMarked) {
			firstMarked = *knownFrom
		}
		markers, err := s.markers.OrganizationMarkersBetween(ctx, firstMarked.Format(dayFormat), bucket.LastDay.Format(dayFormat))
		if err != nil {
			return err
		}
		for _, marker := range markers {
			on(marker.OrganizationID, marker.Day).member = true
		}
	}

	names, err := s.organizations.PlatformOrganizationNames(ctx, slices.Collect(maps.Keys(active)))
	if err != nil {
		return err
	}
	total := 0
	for organizationID, organization := range active {
		name, ok := names[organizationID]
		if !ok {
			// Deleted between the two reads, its sales and markers with it.
			continue
		}
		cause := CauseBoth
		switch {
		case !organization.member:
			cause = CauseSale
		case !organization.sale:
			cause = CauseMember
		}
		total += len(organization.days)
		breakdown.Organizations = append(breakdown.Organizations, PlatformTrendBreakdownOrganization{
			OrganizationID: organizationID, Name: name,
			Key: countSeriesKey, Value: len(organization.days), Cause: &cause,
		})
	}
	slices.SortFunc(breakdown.Organizations, func(a, b PlatformTrendBreakdownOrganization) int {
		return rank(a.Key, b.Key, a.Value, b.Value, a.Name, b.Name, a.OrganizationID, b.OrganizationID)
	})
	breakdown.Totals = []PlatformTrendBreakdownTotal{{Key: countSeriesKey, Value: total}}
	return nil
}

// memberHalfKnownFrom is the first platform day the Member half of Active
// Organizations can be read for, and nil when there is none.
//
// It is the later of two days. The first is the day the daily close first
// froze with a Member's work counted: a day frozen before it holds the sale
// half alone, and a marker that turned up for it since is not part of what
// it was frozen from. Until the close has frozen such a day, it is the day
// of the oldest marker, which is the day the close will write down. The
// second is today less thirteen months, the day markers are kept from: an
// older day's markers are deleted by the close, and are not read here even
// on the night before it runs, so the answer does not turn on the job.
func (s *Service) memberHalfKnownFrom(ctx context.Context) (*time.Time, error) {
	began, err := firstDay(ctx, func(ctx context.Context, _ string) (string, bool, error) {
		return s.markers.OrganizationMarkersBeganOn(ctx)
	})
	if err != nil {
		return nil, err
	}
	if began == nil {
		began, err = firstDay(ctx, func(ctx context.Context, _ string) (string, bool, error) {
			return s.markers.FirstOrganizationMarkerDay(ctx)
		})
		if err != nil || began == nil {
			return nil, err
		}
	}
	if keptFrom := monthsBefore(civilDate(s.now()), markerRetentionMonths); keptFrom.After(*began) {
		return &keptFrom, nil
	}
	return began, nil
}

// breakDownActiveCustomers states the Active Customers of the bucket by
// kind, from the totals the daily close froze and nothing else: the counts
// of each day of the bucket that has an Active Customers figure, added up. A
// bucket no day of which has one is not recorded, which is not a zero.
func (s *Service) breakDownActiveCustomers(ctx context.Context, breakdown *PlatformTrendBreakdown, bucket Bucket) error {
	frozen, err := s.totals.ActivityTotalsBetween(ctx, bucket.FirstDay.Format(dayFormat), bucket.LastDay.Format(dayFormat))
	if err != nil {
		return err
	}
	var kinds *PlatformTrendBreakdownCustomerKinds
	active := 0
	for _, day := range frozen {
		if day.Customers == nil {
			continue
		}
		if kinds == nil {
			kinds = &PlatformTrendBreakdownCustomerKinds{}
		}
		active += day.Customers.Active
		kinds.New += day.Customers.New
		kinds.Returning += day.Customers.Returning
		kinds.ReturningBought += day.Customers.ReturningBought
	}
	if kinds == nil {
		breakdown.Recorded = false
		return nil
	}
	breakdown.CustomerKinds = kinds
	breakdown.Totals = []PlatformTrendBreakdownTotal{{Key: countSeriesKey, Value: active}}
	return nil
}
