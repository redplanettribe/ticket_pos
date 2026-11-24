// Package trendspan is the span arithmetic every trends reading shares:
// Platform Trends and Organization Trends alike. It names the spans a reader
// may choose, resolves one against today on the platform clock, cuts it into
// buckets, finds the span it is compared with, and sets today apart as the
// partial bucket outside every total.
//
// A span longer than 90 days is drawn in a long grain the caller chooses.
// Platform Trends draws it in weeks, its 12m being 52 weeks and a year earlier
// 364 days back; Organization Trends draws it in calendar months, its 12m
// being the current month and the eleven before it, with an all time span.
// The package knows nothing of the wire: each module states a span in its
// own response types.
package trendspan

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// DayFormat is the calendar-date layout every day of a trends reading speaks
// in, YYYY-MM-DD. A day is a civil date on the platform clock, carried as
// midnight UTC so that stepping from one to the next is plain date
// arithmetic, and turned into an instant only through
// platform.StartOfEcuadorDate.
const DayFormat = "2006-01-02"

// Preset is the span a reader chose: one of the fixed spans ending at the
// close of yesterday, all time, or a custom range of days.
type Preset string

const (
	Preset7Days    Preset = "7d"
	Preset30Days   Preset = "30d"
	Preset90Days   Preset = "90d"
	Preset12Months Preset = "12m"
	// PresetAll runs from the first of the month holding Query.Since; it is
	// offered in month mode alone and carries no comparison.
	PresetAll    Preset = "all"
	PresetCustom Preset = "custom"
)

// presetDays is the length in days of each fixed span counted back from
// yesterday. 12m is not here: its length depends on the long grain.
var presetDays = map[Preset]int{
	Preset7Days:  7,
	Preset30Days: 30,
	Preset90Days: 90,
}

// weekYearDays is 12m in week mode, and how far back a year earlier reaches
// for day and week buckets: 52 whole weeks, not a calendar year, so every day
// is set beside the same weekday (a Saturday beside a Saturday), the yearly
// span divides into weeks exactly, and it is compared with the 52 weeks
// before it.
const weekYearDays = 364

// MaxDailyDays is the longest span drawn in days; anything longer is drawn in
// the long grain.
const MaxDailyDays = 90

// MaxCustomDays bounds a custom range, so a typo in a year cannot ask for a
// century of buckets. Ten years is far beyond any history the platform has.
const MaxCustomDays = 3660

// Granularity is the length of a bucket.
type Granularity string

const (
	GranularityDay   Granularity = "day"
	GranularityWeek  Granularity = "week"
	GranularityMonth Granularity = "month"
)

// Comparison is the span a span is set beside.
type Comparison string

const (
	// ComparePrevious is the span just before: as many days ending the day
	// before it starts, or, in months, as many months just before it.
	ComparePrevious Comparison = "previous"
	// CompareYearEarlier is the span a year earlier: 364 days back for day
	// and week buckets, the same calendar months a year before for month
	// buckets.
	CompareYearEarlier Comparison = "year_earlier"
	// CompareNone is no comparison, which all time alone carries.
	CompareNone Comparison = "none"
)

// Query is a span as the caller asked for it, already checked for shape by
// the handler. From and To are set for a custom span alone, as the first and
// last day asked for, both included and From never after To. An empty Preset
// is the default, 30 days, and an empty Compare the default, the previous
// span. Since is read by all time alone: the day its first month holds,
// supplied by the caller.
type Query struct {
	Preset  Preset
	From    time.Time
	To      time.Time
	Compare Comparison
	Since   time.Time
}

// Span is a resolved span: its whole days on the platform clock, the bucket
// length chosen for them, today, which lies outside it, and the comparison
// it is set beside. A comparison span has a zero Today.
type Span struct {
	Preset      Preset
	FirstDay    time.Time
	LastDay     time.Time
	Today       time.Time
	Granularity Granularity
	Comparison  Comparison
}

// Days is how many days the span covers, both ends included.
func (s Span) Days() int {
	return int(s.LastDay.Sub(s.FirstDay).Hours()/24) + 1
}

// EndsYesterday reports whether the span runs up to today, which is when today
// is read beside it as the partial bucket.
func (s Span) EndsYesterday() bool {
	return !s.Today.IsZero() && s.LastDay.Equal(s.Today.AddDate(0, 0, -1))
}

// Resolve turns a query into the span it names on the platform clock, long
// being the grain of a span longer than MaxDailyDays: GranularityWeek or
// GranularityMonth. It reports false for a custom range with no closed day in
// it, and for all time in week mode.
//
// A span ends at the close of yesterday: today is never inside one, because a
// day still running would read as a collapse beside the days before it. A
// preset runs back from yesterday: 7d, 30d and 90d their length, 12m 52 weeks
// in week mode and, in month mode, to the first of the month eleven months
// before the current one. All time starts on the first of the month holding
// Since, or of the month holding yesterday when Since is later or unset, so it
// always holds a closed day. A custom range keeps its first day and is cut at
// yesterday when it reaches today or later; one whose first day is today or
// later has nothing left once cut.
func Resolve(query Query, long Granularity, now time.Time) (Span, bool) {
	today := CivilDate(now)
	yesterday := today.AddDate(0, 0, -1)

	compare := query.Compare
	if compare == "" {
		compare = ComparePrevious
	}
	preset := query.Preset
	if preset == "" {
		preset = Preset30Days
	}

	switch preset {
	case Preset12Months:
		if long == GranularityMonth {
			return newSpan(preset, firstOfMonth(today).AddDate(0, -11, 0), yesterday, today, compare, long), true
		}
		return newSpan(preset, yesterday.AddDate(0, 0, -(weekYearDays-1)), yesterday, today, compare, long), true
	case PresetAll:
		if long != GranularityMonth {
			return Span{}, false
		}
		since := query.Since
		if since.IsZero() || since.After(yesterday) {
			since = yesterday
		}
		span := newSpan(preset, firstOfMonth(since), yesterday, today, CompareNone, long)
		span.Granularity = GranularityMonth
		return span, true
	case PresetCustom:
		if query.From.After(yesterday) {
			return Span{}, false
		}
		to := query.To
		if to.After(yesterday) {
			to = yesterday
		}
		return newSpan(preset, query.From, to, today, compare, long), true
	}
	length, ok := presetDays[preset]
	if !ok {
		return Span{}, false
	}
	return newSpan(preset, yesterday.AddDate(0, 0, -(length-1)), yesterday, today, compare, long), true
}

func newSpan(preset Preset, first, last, today time.Time, compare Comparison, long Granularity) Span {
	span := Span{Preset: preset, FirstDay: first, LastDay: last, Today: today, Granularity: GranularityDay, Comparison: compare}
	if span.Days() > MaxDailyDays {
		span.Granularity = long
	}
	return span
}

// Compared is the comparison span, bucketed the same way so its buckets line
// up with the span's one for one, and false when the span carries none (all
// time). It has no today beside it.
//
// For day and week buckets it is as many days as the span: the previous span
// ends the day before the span starts, and the year-earlier span is the span
// moved back 52 weeks, which a custom range longer than that overlaps. For
// month buckets it is the span moved back by whole months, keeping its cuts:
// by a year for the year-earlier span, so each month is set beside the same
// calendar month a year before, and by as many months as the span touches for
// the previous span, which so ends in the month before the span's first.
func (s Span) Compared() (Span, bool) {
	if s.Comparison == CompareNone {
		return Span{}, false
	}
	compared := Span{Preset: s.Preset, Granularity: s.Granularity, Comparison: s.Comparison}
	if s.Granularity == GranularityMonth {
		months := monthIndex(s.LastDay) - monthIndex(s.FirstDay) + 1
		if s.Comparison == CompareYearEarlier {
			months = 12
		}
		compared.FirstDay = shiftMonths(s.FirstDay, -months)
		compared.LastDay = shiftMonths(s.LastDay, -months)
		return compared, true
	}

	last := s.FirstDay.AddDate(0, 0, -1)
	if s.Comparison == CompareYearEarlier {
		last = s.LastDay.AddDate(0, 0, -weekYearDays)
	}
	compared.FirstDay = last.AddDate(0, 0, -(s.Days() - 1))
	compared.LastDay = last
	return compared, true
}

// Bucket is one bucket's first and last day, both included. Partial is set on
// a month the span cuts, and on today's bucket.
type Bucket struct {
	First   time.Time
	Last    time.Time
	Partial bool
}

// Buckets cuts the span into its buckets, oldest first.
//
// A daily span is one bucket per day. A monthly span is one bucket per
// calendar month it touches, cut at the span's first and last day, a cut
// month flagged partial. A weekly span is counted BACK from its last day in
// runs of seven, so two spans of the same length always line up bucket to
// bucket, whatever weekday either ends on; when the span's length is not a
// whole number of weeks, the stub is its oldest bucket, cut at the span's
// first day.
func (s Span) Buckets() []Bucket {
	if s.Granularity == GranularityMonth {
		var out []Bucket
		for first := s.FirstDay; !first.After(s.LastDay); first = firstOfMonth(first).AddDate(0, 1, 0) {
			last := lastOfMonth(first)
			bucket := Bucket{First: first, Last: last}
			if last.After(s.LastDay) {
				bucket.Last = s.LastDay
			}
			bucket.Partial = bucket.First.Day() != 1 || !bucket.Last.Equal(last)
			out = append(out, bucket)
		}
		return out
	}

	step := 1
	if s.Granularity == GranularityWeek {
		step = 7
	}
	var reversed []Bucket
	for last := s.LastDay; !last.Before(s.FirstDay); last = last.AddDate(0, 0, -step) {
		first := last.AddDate(0, 0, -(step - 1))
		if first.Before(s.FirstDay) {
			first = s.FirstDay
		}
		reversed = append(reversed, Bucket{First: first, Last: last})
	}
	out := make([]Bucket, len(reversed))
	for i, b := range reversed {
		out[len(reversed)-1-i] = b
	}
	return out
}

// TodayBucket is today alone, flagged partial, when the span runs up to it:
// the day still running, drawn beside the span and outside it. In month mode
// it is the current month's today portion, the current month's bucket ending
// at yesterday.
func (s Span) TodayBucket() (Bucket, bool) {
	if !s.EndsYesterday() {
		return Bucket{}, false
	}
	return Bucket{First: s.Today, Last: s.Today, Partial: true}, true
}

// Valued is a bucket with its value.
type Valued struct {
	Bucket
	Value int
}

// Series is one per-day figure stated over a span: a value per bucket and
// their total, today's partial value apart and in neither, and the same over
// the comparison span, nil when the span carries none.
type Series struct {
	Buckets         []Valued
	Total           int
	Today           *Valued
	Comparison      []Valued
	ComparisonTotal *int
}

// Series states a figure, given as its values per day keyed by DayFormat,
// over the span: each bucket the sum of its days, zero where a day has none.
// Today is never in a bucket or a total, the span's or the comparison's.
func (s Span) Series(perDay map[string]int) Series {
	var series Series
	series.Buckets, series.Total = valued(s.Buckets(), perDay)
	if today, ok := s.TodayBucket(); ok {
		series.Today = &Valued{Bucket: today, Value: perDay[today.First.Format(DayFormat)]}
	}
	if compared, ok := s.Compared(); ok {
		var total int
		series.Comparison, total = valued(compared.Buckets(), perDay)
		series.ComparisonTotal = &total
	}
	return series
}

func valued(buckets []Bucket, perDay map[string]int) ([]Valued, int) {
	out := make([]Valued, 0, len(buckets))
	total := 0
	for _, bucket := range buckets {
		value := 0
		for d := bucket.First; !d.After(bucket.Last); d = d.AddDate(0, 0, 1) {
			value += perDay[d.Format(DayFormat)]
		}
		total += value
		out = append(out, Valued{Bucket: bucket, Value: value})
	}
	return out, total
}

// CivilDate is the platform day an instant falls on, carried as midnight UTC.
func CivilDate(instant time.Time) time.Time {
	day, _ := time.Parse(DayFormat, platform.EcuadorDate(instant))
	return day
}

func firstOfMonth(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func lastOfMonth(day time.Time) time.Time {
	return firstOfMonth(day).AddDate(0, 1, -1)
}

// monthIndex numbers calendar months consecutively.
func monthIndex(day time.Time) int {
	return day.Year()*12 + int(day.Month()) - 1
}

// shiftMonths moves a day by whole months, keeping its day of the month where
// the target month has it and a month's last day on the target month's last
// day, so a whole month stays whole and a cut one is cut the same.
func shiftMonths(day time.Time, months int) time.Time {
	target := firstOfMonth(day).AddDate(0, months, 0)
	if day.Equal(lastOfMonth(day)) || day.Day() > lastOfMonth(target).Day() {
		return lastOfMonth(target)
	}
	return target.AddDate(0, 0, day.Day()-1)
}
