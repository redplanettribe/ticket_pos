package platform

import "time"

// StartOfEcuadorDay is midnight, Ecuador time, on the calendar date the given
// instant falls on — the instant "today in Ecuador" begins.
//
// It is the second rule this platform states in Ecuadorian local time rather
// than in elapsed hours (see EcuadorTimeZone). The Reversal Window's 20:00
// cutoff was the first; this one bounds the Payable Balance, which counts only
// the sales recorded BEFORE today (ADR 0025). Both are properties of the
// operator rather than of any Event, so neither ever reads an Event's own
// timezone.
//
// The conversion into Ecuador's zone before the date is read is the whole point,
// exactly as it is in ecuadorCutoffOn: a sale recorded at 03:00 UTC belongs to
// the previous Ecuadorian day, and reading its date in UTC or in the server's
// local zone would clear somebody's money a day early or a day late.
//
// The result is a half-open lower bound. A sale is cleared when its created_at
// is strictly before this instant, so a sale recorded at exactly midnight
// belongs to today and waits, and one recorded a nanosecond earlier does not.
func StartOfEcuadorDay(instant time.Time) time.Time {
	loc := ecuadorLocation()
	local := instant.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}
