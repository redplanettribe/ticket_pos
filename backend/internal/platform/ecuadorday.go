package platform

import "time"

// StartOfEcuadorDay is midnight, Ecuador time, on the calendar date the given
// instant falls on — the instant "today in Ecuador" begins.
//
// It is the second rule this platform states in Ecuadorian local time rather
// than in elapsed hours (see EcuadorTimeZone). The Reversal Window's 20:00
// cutoff was the first; this one bounds the Payable Balance, which counts only
// the sales recorded BEFORE today (ADR 0026). Both are properties of the
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

// EcuadorDate is the calendar date, Ecuador time, an instant falls on, as
// YYYY-MM-DD — the form a record keeps when it names a day rather than a
// moment. It reads the date the same way StartOfEcuadorDay does: converted
// into Ecuador's zone first, so an instant at 03:00 UTC names the previous
// Ecuadorian day. Locale-free by design; where words are wanted the mail
// templates have their own rendering.
func EcuadorDate(instant time.Time) string {
	return instant.In(ecuadorLocation()).Format("2006-01-02")
}

// StartOfEcuadorDate is midnight, Ecuador time, on a named calendar date: the
// instant that day begins on the platform clock. It is EcuadorDate's reverse,
// for a reader that holds a day and needs the window of instants it covers, as
// Platform Trends does (#810). Only the date's year, month and day are read;
// its location and time of day are ignored, because a civil date is commonly
// carried as midnight UTC and must not be shifted by that carrier.
func StartOfEcuadorDate(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, ecuadorLocation())
}

// LatestEcuadorHour is the latest instant at hour:00, Ecuador time, that is
// not after the given one: today's if the platform clock has reached it,
// yesterday's otherwise. It is how a daily job that runs at a stated hour of
// the platform clock names the run it is, whenever it was actually invoked: a
// run a few seconds late, a run made by hand in the afternoon and a run
// retried before midnight are all the same day's run.
func LatestEcuadorHour(instant time.Time, hour int) time.Time {
	loc := ecuadorLocation()
	local := instant.In(loc)
	at := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, loc)
	if at.After(instant) {
		at = at.AddDate(0, 0, -1)
	}
	return at
}
