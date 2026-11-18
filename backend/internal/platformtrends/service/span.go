package service

import (
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// dayFormat is the calendar-date layout every day of Platform Trends speaks
// in. A day is a civil date on the platform clock, carried as midnight UTC so
// that stepping from one to the next is plain date arithmetic, and turned into
// an instant only through platform.StartOfEcuadorDate.
const dayFormat = "2006-01-02"

// SpanPreset is the span an operator chose: one of four fixed lengths ending at
// the close of yesterday, or a custom range of days.
type SpanPreset string

const (
	Preset7Days    SpanPreset = "7d"
	Preset30Days   SpanPreset = "30d"
	Preset90Days   SpanPreset = "90d"
	Preset12Months SpanPreset = "12m"
	PresetCustom   SpanPreset = "custom"
)

// Presets is every span a caller may name, in the order the page offers them.
var Presets = []SpanPreset{Preset7Days, Preset30Days, Preset90Days, Preset12Months, PresetCustom}

// presetDays is the length in days of each fixed span. Twelve months is read
// as 52 whole weeks: it is the one preset drawn in weeks, and 364 days divide
// into them exactly, so no bucket of the default yearly reading is a stub and
// the same span a year earlier starts on the same weekday.
var presetDays = map[SpanPreset]int{
	Preset7Days:    7,
	Preset30Days:   30,
	Preset90Days:   90,
	Preset12Months: 364,
}

// maxDailySpanDays is the longest span drawn in days; anything longer is drawn
// in weeks.
const maxDailySpanDays = 90

// maxCustomSpanDays bounds a custom range, so a typo in a year cannot ask for
// a century of buckets. Ten years is far beyond any history the platform has.
const maxCustomSpanDays = 3660

// PlatformTrendGranularity is the length of a bucket.
type PlatformTrendGranularity string

const (
	GranularityDay  PlatformTrendGranularity = "day"
	GranularityWeek PlatformTrendGranularity = "week"
)

// SpanQuery is a span as the caller asked for it, before it is resolved
// against today. From and To are read only for a custom span; each is the
// raw YYYY-MM-DD text, empty when absent.
type SpanQuery struct {
	Preset SpanPreset
	From   string
	To     string
}

// Span is a resolved span: its whole days on the platform clock, the bucket
// length chosen for them, and today, which lies outside it.
type Span struct {
	Preset      SpanPreset
	FirstDay    time.Time
	LastDay     time.Time
	Today       time.Time
	Granularity PlatformTrendGranularity
}

// days is how many days the span covers, both ends included.
func (s Span) days() int {
	return int(s.LastDay.Sub(s.FirstDay).Hours()/24) + 1
}

// endsYesterday reports whether the span runs up to today, which is when today
// is read beside it as the partial bucket.
func (s Span) endsYesterday() bool {
	return s.LastDay.Equal(s.Today.AddDate(0, 0, -1))
}

// ResolveSpan turns a query into the span it names on the platform clock, or
// the field errors that refuse it.
//
// A span ends at the close of yesterday: today is never inside one, because a
// day still running would read as a collapse beside the days before it. A
// preset runs back its length from yesterday. A custom range keeps its first
// day and is cut at yesterday when it reaches today or later; a range whose
// first day is after its last, or after yesterday, has no closed day in it and
// is refused.
func ResolveSpan(query SpanQuery, now time.Time) (Span, []platform.FieldError) {
	today := civilDate(now)
	yesterday := today.AddDate(0, 0, -1)

	preset := query.Preset
	if preset == "" {
		preset = Preset30Days // the default span
	}

	if length, ok := presetDays[preset]; ok {
		return newSpan(preset, yesterday.AddDate(0, 0, -(length-1)), yesterday, today), nil
	}
	if preset != PresetCustom {
		return Span{}, []platform.FieldError{{
			Field:   "span",
			Code:    platform.CodeInvalidEnum,
			Message: fmt.Sprintf("must be one of %s", presetList()),
		}}
	}

	var fields []platform.FieldError
	from, fromOK := parseDay("from", query.From, &fields)
	to, toOK := parseDay("to", query.To, &fields)
	if !fromOK || !toOK {
		return Span{}, fields
	}
	if from.After(to) {
		return Span{}, []platform.FieldError{{Field: "from", Code: platform.CodeStartAfterEnd, Message: "must be on or before to"}}
	}
	if from.After(yesterday) {
		return Span{}, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeStartAfterEnd,
			Message: "must be before today: a span ends at the close of yesterday",
		}}
	}
	if to.After(yesterday) {
		to = yesterday
	}
	span := newSpan(PresetCustom, from, to, today)
	if span.days() > maxCustomSpanDays {
		return Span{}, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeTooManyItems,
			Message: fmt.Sprintf("the range must cover at most %d days", maxCustomSpanDays),
		}}
	}
	return span, nil
}

func newSpan(preset SpanPreset, first, last, today time.Time) Span {
	span := Span{Preset: preset, FirstDay: first, LastDay: last, Today: today, Granularity: GranularityDay}
	if span.days() > maxDailySpanDays {
		span.Granularity = GranularityWeek
	}
	return span
}

// parseDay reads one end of a custom range, appending its refusal to fields.
func parseDay(field, raw string, fields *[]platform.FieldError) (time.Time, bool) {
	if raw == "" {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeRequired, Message: "is required for a custom span"})
		return time.Time{}, false
	}
	day, err := time.Parse(dayFormat, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	return day, true
}

func presetList() string {
	out := ""
	for i, p := range Presets {
		if i > 0 {
			out += ", "
		}
		out += string(p)
	}
	return out
}

// civilDate is the platform day an instant falls on, carried as midnight UTC.
func civilDate(instant time.Time) time.Time {
	day, _ := time.Parse(dayFormat, platform.EcuadorDate(instant))
	return day
}

// bucketRange is one bucket's first and last day, both included.
type bucketRange struct {
	first time.Time
	last  time.Time
}

// buckets cuts the span into its buckets, oldest first.
//
// A daily span is one bucket per day. A weekly span is counted BACK from its
// last day in runs of seven, so two spans of the same length always line up
// bucket to bucket, whatever weekday either ends on; when the span's length is
// not a whole number of weeks, the stub is its oldest bucket, cut at the
// span's first day, which its first_day states.
func (s Span) buckets() []bucketRange {
	step := 1
	if s.Granularity == GranularityWeek {
		step = 7
	}
	var reversed []bucketRange
	for last := s.LastDay; !last.Before(s.FirstDay); last = last.AddDate(0, 0, -step) {
		first := last.AddDate(0, 0, -(step - 1))
		if first.Before(s.FirstDay) {
			first = s.FirstDay
		}
		reversed = append(reversed, bucketRange{first: first, last: last})
	}
	out := make([]bucketRange, len(reversed))
	for i, b := range reversed {
		out[len(reversed)-1-i] = b
	}
	return out
}
