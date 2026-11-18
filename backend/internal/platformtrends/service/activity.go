package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platformtrends/repository"
)

// Activity figures and the daily close that freezes them (#815, ADR 0080).
//
// An activity figure counts different subjects per day: Active Organizations
// here, Active Customers after it. Unlike every other figure of Platform
// Trends it is not read live. The daily close freezes each closed platform
// day into a totals row, once, and the read serves those rows alone: a day
// the close has not frozen is absent, not zero, and a frozen day reads the
// same for good.
//
// A count of different subjects cannot be added across days, since Monday's
// Organization and Thursday's may be the same one. So a span is stated as
// the mean daily count, a weekly bucket as the mean of its days, and the
// 7-day and 30-day distinct counts, which the close froze as of each day,
// ride beside it as of the span's last day.

// ActivityTotalsStore is what Platform Trends keeps for itself: the totals
// the daily close freezes per platform day.
type ActivityTotalsStore interface {
	FreezeActivityTotals(ctx context.Context, totals repository.ActivityTotals, frozenAt time.Time) (bool, error)
	ActivityTotalsBetween(ctx context.Context, firstDay, lastDay string) ([]repository.ActivityTotals, error)
	FirstFrozenActivityDay(ctx context.Context) (string, bool, error)
}

// The two runs of days a distinct count is frozen over, each ending on the
// day it is frozen for.
const (
	distinctShortDays = 7
	distinctLongDays  = 30
)

// DailyCloseResult is what one run of the daily close did. It carries days
// and a count and names no Organization.
type DailyCloseResult struct {
	// DaysFrozen is how many platform days this run froze. Zero is the
	// answer of a second run the same night, and of a platform no
	// Organization has ever been active on.
	DaysFrozen int `json:"days_frozen"`
	// FirstDayFrozen is the oldest day this run froze. Null when it froze
	// none.
	FirstDayFrozen *string `json:"first_day_frozen" example:"2026-07-06"`
	// LastDayFrozen is the newest day this run froze. Null when it froze
	// none.
	LastDayFrozen *string `json:"last_day_frozen" example:"2026-07-06"`
	// ClosedThrough is the last closed platform day on the clock the run
	// read: yesterday. Every day up to it is frozen once the run answers.
	ClosedThrough string `json:"closed_through" example:"2026-07-06"`
	// OrganizationMarkersDeleted is how many Organization markers older
	// than thirteen months this run deleted. The frozen totals of the days
	// they marked are untouched.
	OrganizationMarkersDeleted int `json:"organization_markers_deleted"`
}

// CloseDays runs the daily close: it freezes every closed platform day that
// has no totals yet, oldest first, from the first day an Organization was
// active through yesterday, and then deletes the Organization markers older
// than thirteen months.
//
// Which days are closed comes from this service's clock and never from the
// caller. A day already frozen is left exactly as it is, so the run is safe
// to repeat and to overlap with itself; a night that was missed is frozen by
// the next run that happens. A day nobody was active on is frozen as zeros.
//
// Days are frozen one at a time, oldest first, and the first failure is
// returned: what was frozen before it stays frozen and the next run carries
// on from there, so the frozen days never have a hole in them. The scheduled
// route turns that error into a failed check-in.
//
// The first day frozen with a marker in it is written down as the day exact
// recording of Active Organizations began, before that day is frozen, so
// that no run can freeze it and leave the day unsaid. Days frozen before it
// hold the sale half alone and stay that way.
//
// Markers are deleted last, and only by a run that froze everything it
// meant to, so a run that fails deletes nothing.
func (s *Service) CloseDays(ctx context.Context) (*DailyCloseResult, error) {
	now := s.now()
	today := civilDate(now)
	yesterday := today.AddDate(0, 0, -1)
	result := &DailyCloseResult{ClosedThrough: yesterday.Format(dayFormat)}

	if err := s.freezeDays(ctx, now, yesterday, result); err != nil {
		return nil, err
	}

	deleted, err := s.markers.DeleteOrganizationMarkersBefore(ctx, monthsBefore(today, markerRetentionMonths).Format(dayFormat))
	if err != nil {
		return nil, err
	}
	result.OrganizationMarkersDeleted = deleted
	return result, nil
}

// freezeDays freezes every closed day through yesterday that has no totals
// yet, and states what it froze on result.
func (s *Service) freezeDays(ctx context.Context, now, yesterday time.Time, result *DailyCloseResult) error {
	first, err := s.firstActiveDay(ctx)
	if err != nil {
		return err
	}
	if first == nil || first.After(yesterday) {
		return nil
	}

	frozen, err := s.totals.ActivityTotalsBetween(ctx, first.Format(dayFormat), yesterday.Format(dayFormat))
	if err != nil {
		return err
	}
	isFrozen := make(map[string]bool, len(frozen))
	for _, totals := range frozen {
		isFrozen[totals.Day] = true
	}
	var unfrozen []time.Time
	for day := *first; !day.After(yesterday); day = day.AddDate(0, 0, 1) {
		if !isFrozen[day.Format(dayFormat)] {
			unfrozen = append(unfrozen, day)
		}
	}
	if len(unfrozen) == 0 {
		return nil
	}

	// The oldest unfrozen day's 30-day count reaches 29 days before it.
	readFrom := unfrozen[0].AddDate(0, 0, -(distinctLongDays - 1))
	if readFrom.Before(*first) {
		readFrom = *first
	}
	active, err := s.organizationsActiveByDay(ctx, readFrom, yesterday)
	if err != nil {
		return err
	}
	_, markersBegan, err := s.markers.OrganizationMarkersBeganOn(ctx)
	if err != nil {
		return err
	}

	for _, day := range unfrozen {
		stated := day.Format(dayFormat)
		if !markersBegan && active.hasMarker[stated] {
			if err := s.markers.RecordOrganizationMarkersBegan(ctx, stated); err != nil {
				return err
			}
			markersBegan = true
		}
		wrote, err := s.totals.FreezeActivityTotals(ctx, repository.ActivityTotals{
			Day:                       stated,
			ActiveOrganizations:       len(active.organizations[stated]),
			ActiveOrganizations7Days:  distinctOver(active.organizations, day, distinctShortDays),
			ActiveOrganizations30Days: distinctOver(active.organizations, day, distinctLongDays),
		}, now)
		if err != nil {
			return err
		}
		if !wrote {
			// Another run froze it first.
			continue
		}
		result.DaysFrozen++
		if result.FirstDayFrozen == nil {
			result.FirstDayFrozen = &stated
		}
		result.LastDayFrozen = &stated
	}
	return nil
}

// firstActiveDay is the first platform day the close answers for: the
// earliest of the day the first Ticket Sale was recorded, the day of the
// oldest marker still kept and the first day already frozen. Nil when there
// is none of them. Every day from it on is one the close freezes. The first
// frozen day is what holds it in place once the oldest markers are deleted:
// without it a platform with no sale would lose its first day with its last
// marker, and the days after it would never be frozen.
func (s *Service) firstActiveDay(ctx context.Context) (*time.Time, error) {
	var earliest *time.Time
	for _, read := range []func(context.Context, string) (string, bool, error){
		s.sales.PlatformFirstSaleRecordedDay,
		func(ctx context.Context, _ string) (string, bool, error) {
			return s.markers.FirstOrganizationMarkerDay(ctx)
		},
		func(ctx context.Context, _ string) (string, bool, error) { return s.totals.FirstFrozenActivityDay(ctx) },
	} {
		day, err := firstDay(ctx, read)
		if err != nil {
			return nil, err
		}
		if day != nil && (earliest == nil || day.Before(*earliest)) {
			earliest = day
		}
	}
	return earliest, nil
}

// activeOrganizationsByDay is the Organizations active on each of a run of
// platform days.
type activeOrganizationsByDay struct {
	// organizations is keyed by day and then by Organization.
	organizations map[string]map[string]struct{}
	// hasMarker holds the days on which a Member worked in any
	// Organization.
	hasMarker map[string]bool
}

// organizationsActiveByDay is the Organizations active on each platform day
// from firstDay through lastDay. It is the one place that says what makes an
// Organization active: a Ticket Sale was recorded for it that day, or a
// Member worked in it that day, and one that did both is there once.
func (s *Service) organizationsActiveByDay(ctx context.Context, firstDay, lastDay time.Time) (activeOrganizationsByDay, error) {
	active := activeOrganizationsByDay{organizations: map[string]map[string]struct{}{}, hasMarker: map[string]bool{}}
	add := func(day, organizationID string) {
		if active.organizations[day] == nil {
			active.organizations[day] = map[string]struct{}{}
		}
		active.organizations[day][organizationID] = struct{}{}
	}

	recorded, err := s.sales.PlatformOrganizationsWithSalesRecordedByDay(ctx,
		platform.StartOfEcuadorDate(firstDay), platform.StartOfEcuadorDate(lastDay.AddDate(0, 0, 1)), platform.EcuadorTimeZone)
	if err != nil {
		return active, err
	}
	for _, sale := range recorded {
		add(sale.Day, sale.OrganizationID)
	}

	markers, err := s.markers.OrganizationMarkersBetween(ctx, firstDay.Format(dayFormat), lastDay.Format(dayFormat))
	if err != nil {
		return active, err
	}
	for _, marker := range markers {
		add(marker.Day, marker.OrganizationID)
		active.hasMarker[marker.Day] = true
	}
	return active, nil
}

// distinctOver counts the different subjects active across the days ending
// on lastDay, each once however many of those days it was active.
func distinctOver(activeOn map[string]map[string]struct{}, lastDay time.Time, days int) int {
	seen := map[string]struct{}{}
	for back := 0; back < days; back++ {
		for subject := range activeOn[lastDay.AddDate(0, 0, -back).Format(dayFormat)] {
			seen[subject] = struct{}{}
		}
	}
	return len(seen)
}

// PlatformTrendActivityFigure is an activity figure for the span: a count of
// different subjects per day, read from the totals the daily close froze.
// It has one line and no total, since such a count cannot be added across
// days, and no bucket for today, which is never frozen.
type PlatformTrendActivityFigure struct {
	// Recorded is false while the platform does not record this figure at
	// all, which the page states as "not yet recorded" rather than drawing a
	// zero that means "not measured".
	Recorded bool `json:"recorded"`
	// ExactSince is the platform day the figure began to be recorded in
	// full: for Active Organizations the first day the daily close froze
	// with a Member's work counted in it. Days frozen before it hold the
	// sale half alone. Null while no such day exists.
	ExactSince *string `json:"exact_since" example:"2026-07-07"`
	// DataSince is the first platform day the daily close froze. Null when
	// it has frozen none.
	DataSince *string `json:"data_since" example:"2026-01-15"`
	// SaleHalfOnly is true while Active Organizations is counted from
	// recorded Ticket Sales alone on every frozen day, which is while
	// ExactSince is null: no day has been frozen with a Member's work in it.
	SaleHalfOnly bool `json:"sale_half_only"`
	// Mean is the mean daily count over the frozen days of the span. Null
	// when the span has no frozen day.
	Mean *float64 `json:"mean" example:"3.5"`
	// ComparisonMean is the mean daily count over the frozen days of the
	// comparison span. Null when it has none.
	ComparisonMean *float64 `json:"comparison_mean" example:"2.8"`
	// Change is Mean against ComparisonMean as a signed ratio. Null, never
	// zero or infinite, when either is null or ComparisonMean is zero.
	Change *float64 `json:"change" example:"0.25"`
	// Distinct7Days is how many different subjects were active across the 7
	// days ending on the span's last day, each counted once. Null while that
	// day is not frozen.
	Distinct7Days *int `json:"distinct_7d"`
	// Distinct30Days is the same across the 30 days ending on the span's
	// last day.
	Distinct30Days *int `json:"distinct_30d"`
	// Buckets has one entry per bucket of the span, oldest first. An entry
	// is null where the daily close has frozen no day of its bucket: absent,
	// never zero.
	Buckets []*PlatformTrendActivityBucket `json:"buckets"`
	// ComparisonBuckets has one entry per entry of Buckets, at the same
	// index, null on the same terms.
	ComparisonBuckets []*PlatformTrendActivityBucket `json:"comparison_buckets"`
}

// PlatformTrendActivityBucket is one bucket of an activity figure: the
// frozen days it covers, both included, and their mean.
type PlatformTrendActivityBucket struct {
	// FirstDay and LastDay are the first and last frozen day of the bucket,
	// which for a weekly bucket the close has only partly frozen are not
	// the bucket's own ends.
	FirstDay string `json:"first_day" example:"2026-07-06"`
	LastDay  string `json:"last_day" example:"2026-07-06"`
	// Value is the count of the day, or for a weekly bucket the mean of its
	// frozen days' counts.
	Value float64 `json:"value"`
}

// activityDay is one frozen day of one activity figure.
type activityDay struct {
	count          int
	distinct7Days  int
	distinct30Days int
}

// activeOrganizations reads Active Organizations for the span and its
// comparison, from frozen totals alone.
func (s *Service) activeOrganizations(ctx context.Context, span Span) (PlatformTrendActivityFigure, error) {
	dataSince, err := firstDay(ctx, func(ctx context.Context, _ string) (string, bool, error) {
		return s.totals.FirstFrozenActivityDay(ctx)
	})
	if err != nil {
		return PlatformTrendActivityFigure{}, err
	}

	frozen := map[string]activityDay{}
	for _, read := range []Span{span, span.compared()} {
		days, err := s.totals.ActivityTotalsBetween(ctx, read.FirstDay.Format(dayFormat), read.LastDay.Format(dayFormat))
		if err != nil {
			return PlatformTrendActivityFigure{}, err
		}
		for _, day := range days {
			frozen[day.Day] = activityDay{
				count:          day.ActiveOrganizations,
				distinct7Days:  day.ActiveOrganizations7Days,
				distinct30Days: day.ActiveOrganizations30Days,
			}
		}
	}

	figure := activityFigureOf(span, dataSince, frozen)
	// The figure is whole from the first day the close froze with markers.
	// Until it has frozen one, every day holds the sale half alone.
	began, ok, err := s.markers.OrganizationMarkersBeganOn(ctx)
	if err != nil {
		return PlatformTrendActivityFigure{}, err
	}
	if ok {
		figure.ExactSince = &began
	}
	figure.SaleHalfOnly = !ok
	return figure, nil
}

// activityFigureOf states an activity figure from its frozen days, keyed by
// day: the span's buckets and mean, the comparison's beside them, the change
// between the two means and the distinct counts as of the span's last day.
// The figure is stated as recorded; one that is not sets Recorded itself.
func activityFigureOf(span Span, dataSince *time.Time, frozen map[string]activityDay) PlatformTrendActivityFigure {
	figure := PlatformTrendActivityFigure{Recorded: true}
	if dataSince != nil {
		since := dataSince.Format(dayFormat)
		figure.DataSince = &since
	}
	figure.Buckets, figure.Mean = activityBuckets(span, frozen)
	figure.ComparisonBuckets, figure.ComparisonMean = activityBuckets(span.compared(), frozen)
	if figure.Mean != nil && figure.ComparisonMean != nil && *figure.ComparisonMean != 0 {
		ratio := (*figure.Mean - *figure.ComparisonMean) / *figure.ComparisonMean
		figure.Change = &ratio
	}
	if last, ok := frozen[span.LastDay.Format(dayFormat)]; ok {
		figure.Distinct7Days = &last.distinct7Days
		figure.Distinct30Days = &last.distinct30Days
	}
	return figure
}

// activityBuckets cuts a span into its buckets, each the mean of its frozen
// days and nil when it has none, and gives the mean over every frozen day of
// the span, nil likewise.
func activityBuckets(span Span, frozen map[string]activityDay) ([]*PlatformTrendActivityBucket, *float64) {
	ranges := span.buckets()
	buckets := make([]*PlatformTrendActivityBucket, 0, len(ranges))
	spanSum, spanDays := 0, 0
	for _, bucket := range ranges {
		var stated *PlatformTrendActivityBucket
		sum, days := 0, 0
		for day := bucket.first; !day.After(bucket.last); day = day.AddDate(0, 0, 1) {
			totals, ok := frozen[day.Format(dayFormat)]
			if !ok {
				continue
			}
			if stated == nil {
				stated = &PlatformTrendActivityBucket{FirstDay: day.Format(dayFormat)}
			}
			stated.LastDay = day.Format(dayFormat)
			sum += totals.count
			days++
		}
		if stated != nil {
			stated.Value = float64(sum) / float64(days)
			spanSum += sum
			spanDays += days
		}
		buckets = append(buckets, stated)
	}
	if spanDays == 0 {
		return buckets, nil
	}
	mean := float64(spanSum) / float64(spanDays)
	return buckets, &mean
}
