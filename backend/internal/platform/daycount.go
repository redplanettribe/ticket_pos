package platform

// DayCount is how many of something the platform counted on one day. Day is
// a calendar date on the clock the caller of the read named, formatted
// YYYY-MM-DD: a label on a clock and not an instant, so it travels as a
// string. It is what each module hands Platform Trends for a figure that is
// one plain count per day.
type DayCount struct {
	Day   string
	Count int
}
