package invoicing

import "time"

// The Closing Sweep's calendar (#750, spec #742, ADR 0078): once a month,
// after the Annulment Window of every factura of the month just closed has
// closed, the platform asks the Tax Authority about each one it holds as
// authorized, so an annulment somebody made at the portal without telling
// the platform (an Unrecorded Annulment) is found before the month is
// declared.
//
// It RUNS from 00:00 on day 8 of the following month - the first instant
// after every window of the month has closed (day 7, read strictly) - and
// retries facturas with no reading through the end of day 12, which covers
// the SRI's holiday rollover and still lands before the IVA declaration. A
// factura still without a reading after that is UNSWEPT: the Operator
// Dashboard counts it until an operator sweeps it again.
//
// One pure function per question, read in the zone passed in (sri.Guayaquil
// for Ecuador, since the core never imports its adapter), so the tick that
// asks and the dashboard that counts can never disagree about the month.

// ClosingSweepFromDay and ClosingSweepThroughDay are the first and the last
// day of the following month the sweep runs on, inclusive.
const (
	ClosingSweepFromDay    = 8
	ClosingSweepThroughDay = 12
)

// ClosingSweepRun says whether the sweep is running at now and, when it is,
// which month it sweeps: day 1 of the month just closed, as a date-only
// value at midnight UTC (the shape issued_on is read in). The zero time and
// false outside days 8 to 12.
func ClosingSweepRun(now time.Time, zone *time.Location) (time.Time, bool) {
	y, m, d := now.In(zone).Date()
	if d < ClosingSweepFromDay || d > ClosingSweepThroughDay {
		return time.Time{}, false
	}
	return time.Date(y, m-1, 1, 0, 0, 0, 0, time.UTC), true
}

// ClosingSweepLastEnded is the latest month whose run has ended at now, as
// day 1 of it at midnight UTC: the month just closed once day 12 is over,
// and the month before it until then. The month whose unswept facturas the
// Operator Dashboard counts, and which Sweep again acts on.
func ClosingSweepLastEnded(now time.Time, zone *time.Location) time.Time {
	y, m, d := now.In(zone).Date()
	if d > ClosingSweepThroughDay {
		return time.Date(y, m-1, 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(y, m-2, 1, 0, 0, 0, 0, time.UTC)
}

// UnrecordedAnnulment is a factura the platform holds as authorized that
// the Tax Authority reports annulled (#750): annulled at the portal by
// somebody holding the issuer's credentials who never told the platform.
// The platform never records the annulment itself; it warns and queues the
// factura until an operator records it with Mark annulled or, where a live
// Credit Note forbids that, acknowledges it.
type UnrecordedAnnulment struct {
	// FoundAt is when the sweep's ANULADO landed; AttemptID the status call
	// that read it, on the attempts ledger (zero if its write failed).
	FoundAt   time.Time
	AttemptID int64
	// AcknowledgedBy and AcknowledgedAt are the operator's acknowledgement,
	// "" and nil until given. It clears the queue entry; the flag stays,
	// for the archive.
	AcknowledgedBy string
	AcknowledgedAt *time.Time
}
