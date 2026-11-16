package invoicing

import (
	"slices"
	"time"
)

// The Annulment Check (#746, spec #742, ADR 0078): after a factura is
// recorded annulled, the platform asks the Tax Authority's status service on
// a schedule whether it agrees, and reaches a verdict without ever trusting
// one negative answer. Two pure functions, so the tick, Check status and the
// detail view all read the same rule: the VERDICT over the ordered asks, and
// the SCHEDULE of the next ask.
//
// An ASK is the unit (service/status_ask.go): up to three calls, stopping at
// the first real reading. A scheduled ask and an operator's Check status are
// both asks and both feed the verdict; only a scheduled ask moves the
// schedule.

// AnnulmentVerdict is where an annulment's check stands.
type AnnulmentVerdict string

const (
	// AnnulmentChecking: no verdict yet, asks still due inside the first
	// hour.
	AnnulmentChecking AnnulmentVerdict = "checking"
	// AnnulmentConfirmed: the authority reported the document annulled, on
	// one ask, whatever came before. The only verdict that ends the check.
	AnnulmentConfirmed AnnulmentVerdict = "confirmed"
	// AnnulmentMismatch: the authority still reports the document
	// authorized, on two different asks, the later at or after
	// AnnulmentMismatchFloor. Never undoes the annulment, and never returns
	// to unconfirmed.
	AnnulmentMismatch AnnulmentVerdict = "mismatch"
	// AnnulmentUnconfirmed: an ask at or after AnnulmentUnconfirmedAfter was
	// done with neither verdict reached. Admits everything on the operator's
	// word; turns into a mismatch on the same two-AUTORIZADO rule. Also what
	// an annulment recorded before the check existed reads as.
	AnnulmentUnconfirmed AnnulmentVerdict = "unconfirmed"
)

// AnnulmentMismatchFloor is how long after the recording the later of two
// AUTORIZADO answers must come for them to be a mismatch: the portal's
// annulment may take that long to reach the status service, and a Check
// status pressed twice in quick succession must not fake a verdict.
const AnnulmentMismatchFloor = 30 * time.Minute

// AnnulmentUnconfirmedAfter is the ask after which no verdict reads as
// unconfirmed: the last rung of the first hour.
const AnnulmentUnconfirmedAfter = 60 * time.Minute

// AnnulmentCheckRungs are the first hour's asks, measured from the
// recording: due FROM each instant, so a tick that comes later asks then.
var AnnulmentCheckRungs = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute}

// AnnulmentCheckEvery is the spacing of the asks after the first hour, at
// mismatch or unconfirmed.
const AnnulmentCheckEvery = 6 * time.Hour

// AnnulmentCheckFor is the least a check goes on asking for: the later of
// this after the recording and the Annulment Window's close.
const AnnulmentCheckFor = 7 * 24 * time.Hour

// AnnulmentAsk is one ask as the verdict reads it: when it was made, what it
// read, and whether an operator's Check status made it.
type AnnulmentAsk struct {
	At      time.Time
	Reading StatusReading
	Manual  bool
}

// AnnulmentCheckState is a verdict and the instant of the ask that reached
// it (zero while checking).
type AnnulmentCheckState struct {
	Verdict AnnulmentVerdict
	Since   time.Time
}

// AnnulmentVerdictOf is THE verdict rule, over an annulment's asks and the
// instant it was recorded. The asks are read in time order whatever order
// they are handed in.
//
//   - confirmed on any ANULADO, whatever came before;
//   - mismatch on AUTORIZADO from two different asks, the later at or after
//     recordedAt + AnnulmentMismatchFloor;
//   - unconfirmed once an ask at or after recordedAt +
//     AnnulmentUnconfirmedAfter is done with neither;
//   - otherwise checking.
//
// It is monotone in the asks: a later ask can only add an ANULADO or a
// second AUTORIZADO, so mismatch never returns to unconfirmed, unconfirmed
// can turn into mismatch, and only confirmed ends the check.
func AnnulmentVerdictOf(recordedAt time.Time, asks []AnnulmentAsk) AnnulmentCheckState {
	ordered := slices.Clone(asks)
	slices.SortStableFunc(ordered, func(a, b AnnulmentAsk) int { return a.At.Compare(b.At) })

	for _, a := range ordered {
		if a.Reading == StatusAnnulled {
			return AnnulmentCheckState{Verdict: AnnulmentConfirmed, Since: a.At}
		}
	}
	floor := recordedAt.Add(AnnulmentMismatchFloor)
	settles := recordedAt.Add(AnnulmentUnconfirmedAfter)
	state := AnnulmentCheckState{Verdict: AnnulmentChecking}
	authorized := 0
	for _, a := range ordered {
		if a.Reading == StatusAuthorized {
			authorized++
			if authorized >= 2 && !a.At.Before(floor) {
				return AnnulmentCheckState{Verdict: AnnulmentMismatch, Since: a.At}
			}
		}
		if state.Verdict == AnnulmentChecking && !a.At.Before(settles) {
			state = AnnulmentCheckState{Verdict: AnnulmentUnconfirmed, Since: a.At}
		}
	}
	return state
}

// FirstAnnulmentAsk is when a freshly recorded annulment's first ask falls
// due: the first rung.
func FirstAnnulmentAsk(recordedAt time.Time) time.Time {
	return recordedAt.Add(AnnulmentCheckRungs[0])
}

// AnnulmentChecksUntil is the instant after which a check asks no more: the
// later of recordedAt + AnnulmentCheckFor and the Annulment Window's close.
func AnnulmentChecksUntil(recordedAt, windowClosesAt time.Time) time.Time {
	until := recordedAt.Add(AnnulmentCheckFor)
	if windowClosesAt.After(until) {
		return windowClosesAt
	}
	return until
}

// NextAnnulmentAsk is the schedule: after an ask made at `after`, the next
// ask is the first rung strictly later - +1, +5, +15, +30, +60 min, then
// every AnnulmentCheckEvery from +60 min - up to AnnulmentChecksUntil, and
// none past it. A confirmed check asks nothing more. A tick that comes late
// and covers two rungs therefore asks once, not twice in a row.
func NextAnnulmentAsk(recordedAt, windowClosesAt time.Time, verdict AnnulmentVerdict, after time.Time) (time.Time, bool) {
	if verdict == AnnulmentConfirmed {
		return time.Time{}, false
	}
	for _, rung := range AnnulmentCheckRungs {
		if at := recordedAt.Add(rung); at.After(after) {
			return at, true
		}
	}
	until := AnnulmentChecksUntil(recordedAt, windowClosesAt)
	last := recordedAt.Add(AnnulmentCheckRungs[len(AnnulmentCheckRungs)-1])
	// The first six-hourly rung strictly after `after`.
	steps := after.Sub(last)/AnnulmentCheckEvery + 1
	next := last.Add(steps * AnnulmentCheckEvery)
	if next.After(until) {
		return time.Time{}, false
	}
	return next, true
}
