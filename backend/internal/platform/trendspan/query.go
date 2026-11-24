package trendspan

import (
	"fmt"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The checks every trends handler makes on a span's shape before the span is
// resolved: Platform Trends and Organization Trends alike. Each module names
// its own presets and comparisons; the custom range, its days and the way a
// value outside a list is refused are shared here.

// InvalidEnum refuses a value of field that is not one of allowed, naming
// them.
func InvalidEnum[T ~string](field string, allowed []T) platform.FieldError {
	names := make([]string, 0, len(allowed))
	for _, value := range allowed {
		names = append(names, string(value))
	}
	return platform.FieldError{Field: field, Code: platform.CodeInvalidEnum, Message: "must be one of " + strings.Join(names, ", ")}
}

// ParseDay reads one day of a query, DayFormat, appending its refusal to
// fields: an absent one in the words of required, a malformed one as an
// invalid date.
func ParseDay(field, raw, required string, fields *[]platform.FieldError) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeRequired, Message: required})
		return time.Time{}, false
	}
	day, err := time.Parse(DayFormat, raw)
	if err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return time.Time{}, false
	}
	return day, true
}

// ParseCustomRange reads the two days of a custom span, from and to: both
// well formed, from not after to, covering at most MaxCustomDays. Every field
// error of the two days is returned at once, and an inverted or overlong
// range only when both days parsed. Where the range falls against today is
// Resolve's to say.
func ParseCustomRange(fromRaw, toRaw string) (from, to time.Time, fields []platform.FieldError) {
	const required = "is required for a custom span"
	from, fromOK := ParseDay("from", fromRaw, required, &fields)
	to, toOK := ParseDay("to", toRaw, required, &fields)
	if !fromOK || !toOK {
		return time.Time{}, time.Time{}, fields
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, []platform.FieldError{{Field: "from", Code: platform.CodeStartAfterEnd, Message: "must be on or before to"}}
	}
	if to.Sub(from) >= MaxCustomDays*24*time.Hour {
		return time.Time{}, time.Time{}, []platform.FieldError{{
			Field:   "from",
			Code:    platform.CodeTooManyItems,
			Message: fmt.Sprintf("the range must cover at most %d days", MaxCustomDays),
		}}
	}
	return from, to, nil
}
