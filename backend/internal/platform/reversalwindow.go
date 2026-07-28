package platform

import (
	"sync"
	"time"
)

// EcuadorTimeZone is the platform's own wall clock.
//
// Some of this system's rules are stated in Ecuadorian local time rather than in
// elapsed hours, because that is how the business states them — the Reversal
// Window's 20:00 cutoff is the first. Such a rule is identical for every Event on
// the platform no matter where the Event is: it is a property of the operator,
// not of the show.
//
// It must never be confused with an Event's own timezone, which is stored per
// Event, may be any IANA zone, and is the only thing that interprets an Event's
// schedule. The two are separate clocks that happen to be spelled the same way,
// and conflating them is the mistake this constant is named to make visible at
// every call site.
const EcuadorTimeZone = "America/Guayaquil"

// reversalCutoffHour is the hour, in EcuadorTimeZone, at which the Reversal
// Window closes on the calendar date of purchase. 20:00 is the platform's rule
// (ADR 0018); that the launch Payment Provider happens to enforce the same hour
// is a fact about that provider, kept inside it.
const reversalCutoffHour = 20

// ReversalWindow is the period during which a Customer may reverse their own
// Online Sale: from the moment its Payment is approved until the earlier of
// 20:00 Ecuador time on the calendar date of purchase, or the Event's start.
//
// It is a half-open interval, [OpensAt, ClosesAt). The closing instant is the one
// the window runs *until*, so a Customer arriving exactly at 20:00, or exactly as
// the doors open, is late.
//
// A window can be empty — ClosesAt at or before OpensAt — and that is a real,
// ordinary outcome rather than an error: somebody buying at 20:30 never has a
// window at all, and somebody buying after their Event has started never has one
// either. IsOpenAt reports false throughout, with no special case.
type ReversalWindow struct {
	// OpensAt is the instant the Payment was approved.
	OpensAt time.Time
	// ClosesAt is the earlier of the Ecuadorian cutoff and the Event's start.
	ClosesAt time.Time
}

// NewReversalWindow computes the Reversal Window for one Online Sale, and is the
// single place in the system that does.
//
// paymentApprovedAt is the instant the Sale's Payment was approved; it both opens
// the window and, read in EcuadorTimeZone, fixes the calendar date whose 20:00 is
// the cutoff. eventStartsAt is the Event's start instant. Every Online Sale has
// one: publishing an Event requires a start and a valid timezone, and only a
// published Event is sellable.
//
// Both arguments are instants and are compared as instants. Whatever location
// they arrive carrying — the database driver's, UTC, the Event's own — is
// irrelevant to the result: the only zone conversion here is the one that turns
// the approval instant into an Ecuadorian calendar date.
//
// Nothing about the money is a parameter. The window does not know or care what
// was paid, or through which Payment Provider, so a free Online Sale settled by
// the platform itself gets exactly the window a PayPhone sale gets.
func NewReversalWindow(paymentApprovedAt, eventStartsAt time.Time) ReversalWindow {
	closesAt := ecuadorCutoffOn(paymentApprovedAt)
	if eventStartsAt.Before(closesAt) {
		closesAt = eventStartsAt
	}
	return ReversalWindow{OpensAt: paymentApprovedAt, ClosesAt: closesAt}
}

// IsOpenAt reports whether the window contains now.
func (w ReversalWindow) IsOpenAt(now time.Time) bool {
	return !now.Before(w.OpensAt) && now.Before(w.ClosesAt)
}

// ecuadorCutoffOn is 20:00 Ecuador time on the calendar date, in Ecuador, that
// the given instant falls on.
//
// The date comes from converting the instant into EcuadorTimeZone first, which is
// the entire point: an approval at 03:00 UTC belongs to the previous Ecuadorian
// day, and reading its date anywhere else — UTC, the server's local zone, the
// Event's zone — would move somebody's deadline by a day.
func ecuadorCutoffOn(instant time.Time) time.Time {
	loc := ecuadorLocation()
	local := instant.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), reversalCutoffHour, 0, 0, 0, loc)
}

// ecuadorLocation resolves EcuadorTimeZone once.
//
// The lookup cannot realistically fail — the server embeds the IANA database
// (see cmd/server/main.go) — but a hard dependency on the host's zoneinfo is not
// worth a panic in a computation that governs whether somebody may have their
// money back. Ecuador is UTC-5 with no daylight saving and has been since 1993,
// so the fallback is exact rather than approximate, and the offset is the same
// answer the database would have given.
var ecuadorLocation = sync.OnceValue(func() *time.Location {
	if loc, err := time.LoadLocation(EcuadorTimeZone); err == nil {
		return loc
	}
	return time.FixedZone("-05", -5*60*60)
})
