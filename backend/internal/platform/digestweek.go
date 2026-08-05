package platform

import "time"

// DigestWeekStart is the Monday that begins the Follow Digest week the given
// instant falls in, as a date at midnight in Ecuador's zone (#220, ADR 0030).
//
// It is the ONE place a "week" is defined for this feature, and everything that
// makes a double-send impossible rests on it. The uniqueness on
// `follow_digests (customer_id, week_start)` only prevents anything if two runs
// looking at the same week compute the same value, so if a second definition of
// "which week is it" ever appears beside this one, that constraint quietly stops
// being a guarantee.
//
// ECUADOR'S ZONE rather than UTC, for the reason StartOfEcuadorDay converts
// before reading a date: the weekly enqueue is scheduled for Thursday morning in
// Guayaquil, and a run at 03:00 UTC on Monday belongs to the Ecuadorian week
// that has not started yet. Reading the date in UTC would put two runs of one
// Ecuadorian week on either side of a boundary and let both enqueue.
//
// MONDAY rather than the day the schedule happens to fire. The obvious cheaper
// key — the date of the run — makes the week identity depend on WHEN the job
// ran, so a cron that fires Thursday and an operator who curls the endpoint on
// Friday produce two different weeks for the same seven days, and the Customer
// gets two Digests. Anchoring on the Monday means every instant in one calendar
// week maps to one value however many times, and from whatever day, the enqueue
// is driven. Which weekday the Digest actually goes out on is the scheduler's
// business and never this function's.
//
// The result is midnight rather than an arbitrary time of day because it is
// stored in a DATE column: a value with a time component would be truncated on
// the way in, and two callers could disagree about a week while agreeing about
// the row.
func DigestWeekStart(instant time.Time) time.Time {
	loc := ecuadorLocation()
	local := instant.In(loc)
	// Go numbers Sunday as 0, so the offset back to Monday is not simply the
	// weekday: Sunday belongs to the week that began six days earlier, not to the
	// one starting tomorrow.
	daysSinceMonday := (int(local.Weekday()) + 6) % 7
	monday := local.AddDate(0, 0, -daysSinceMonday)
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, loc)
}
