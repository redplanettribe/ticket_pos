package invoicing

import "time"

// The annulment of a factura (#745, spec #742, ADR 0078): the Annulment
// Window, read strictly, and the record Mark annulled leaves behind.
//
// THE SRI OFFERS NO MACHINE ROUTE FOR ANNULMENT (ADR 0078 section 1): a
// Platform Operator annuls at the SRI en linea portal and the platform
// records it, as one act whether the portal step came before the recording
// or after it. What the platform keeps is the window it may be recorded in
// and who said so, when, and why.

// IsFactura reports whether a document kind is a factura: a Sale Invoice or
// a manual Tax Invoice. Mark annulled widens to `authorized` on these alone;
// a Credit Note keeps its Mark annulled exactly as ADR 0061 and #484 built
// it, from `pending` and `needs_attention`, with no window and no record.
func IsFactura(kind DocumentKind) bool {
	return kind == DocumentKindSale || kind == DocumentKindManual
}

// AnnulmentWindow is how long the Tax Authority lets a factura be annulled:
// in Ecuador until day 7 of the month after its Emission Date (Res.
// NAC-DGERCGC25-00000014 Art. 3, as amended). The platform reads it
// STRICTLY: the deadline is day 7 itself, to the end of that day in the
// zone it was built for, and the SRI's rollover to the next business day
// when day 7 is a rest day or a holiday is never claimed, so the platform's
// deadline is never later than the SRI's. Recording past it is admitted
// only with the operator's acknowledgement that the portal took it, because
// a rollover-day annulment is real.
//
// The dates (IssuedOn, Deadline, WarningFrom) are calendar days as date-only
// values at midnight UTC, the shape issued_on is read in; ClosesAt and
// WarnsAt are the instants those days begin and end in the zone.
type AnnulmentWindow struct {
	IssuedOn time.Time
	// Deadline is the last day an annulment is inside the window: day 7 of
	// the month after IssuedOn.
	Deadline time.Time
	// WarningFrom is day 1 of that month, from which the factura's page
	// reads "Annullable until" in a warning tone (ADR 0078 section 8).
	WarningFrom time.Time
	// ClosesAt is the instant the window closes: 00:00 on the day after the
	// Deadline, in the zone. WarnsAt is 00:00 on WarningFrom, in the zone.
	ClosesAt time.Time
	WarnsAt  time.Time
	zone     *time.Location
}

// AnnulmentWindowFor is the window of a factura emitted on issuedOn, read in
// zone - sri.Guayaquil for Ecuador, passed in because the core never imports
// its adapter. Only issuedOn's calendar day is read.
//
// One pure function, so every surface that speaks of the window (the gate,
// the detail view the staff page draws from, the later Annulment Check and
// Closing Sweep) reads the same day; the staff app never computes it.
func AnnulmentWindowFor(issuedOn time.Time, zone *time.Location) AnnulmentWindow {
	y, m, d := issuedOn.Date()
	// Day 1 of the following month; time.Date normalises December + 1 into
	// January of the next year.
	nextY, nextM, _ := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC).Date()
	return AnnulmentWindow{
		IssuedOn:    time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Deadline:    time.Date(nextY, nextM, 7, 0, 0, 0, 0, time.UTC),
		WarningFrom: time.Date(nextY, nextM, 1, 0, 0, 0, 0, time.UTC),
		ClosesAt:    time.Date(nextY, nextM, 8, 0, 0, 0, 0, zone),
		WarnsAt:     time.Date(nextY, nextM, 1, 0, 0, 0, 0, zone),
		zone:        zone,
	}
}

// Open reports whether now is still inside the window: before the end of
// the Deadline in the zone.
func (w AnnulmentWindow) Open(now time.Time) bool {
	return now.Before(w.ClosesAt)
}

// Warning reports whether now is in the window's warning stretch: from day
// 1 of the following month to the Deadline, inclusive. False once closed.
func (w AnnulmentWindow) Warning(now time.Time) bool {
	return w.Open(now) && !now.Before(w.WarnsAt)
}

// Today is now's calendar day in the zone, as a date-only value: the day the
// staff page marks on the window's timeline.
func (w AnnulmentWindow) Today(now time.Time) time.Time {
	y, m, d := now.In(w.zone).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DaysLeft is how many whole days remain after today before the Deadline:
// 0 on the last day, and 0 once closed.
func (w AnnulmentWindow) DaysLeft(now time.Time) int {
	if !w.Open(now) {
		return 0
	}
	return int(w.Deadline.Sub(w.Today(now)).Hours() / 24)
}

// TotalDays is the window's length in days, from the Emission Date to the
// Deadline: what the staff page's timeline is drawn over.
func (w AnnulmentWindow) TotalDays() int {
	return int(w.Deadline.Sub(w.IssuedOn).Hours() / 24)
}

// Annulment is one Mark annulled on a factura, as the append-only
// annulments ledger keeps it (migration 131): the act's own record, kept
// whatever the factura later becomes. The invoice row's annulled_by and
// annulled_at are the CURRENT state; this is the history, and a Reinstate
// (a later ticket) stamps ReinstatedAt and opens the way for another.
type Annulment struct {
	ID        string
	InvoiceID string
	// FromStatus is the state the factura was annulled from: pending,
	// needs_attention or authorized.
	FromStatus InvoiceStatus
	AnnulledBy string
	AnnulledAt time.Time
	// Note is the operator's optional note, "" when none was left.
	Note string
	// LateAcknowledged is the operator's word, given past the Annulment
	// Window's close, that the portal took the annulment all the same.
	LateAcknowledged bool
	// ReinstatedAt is nil while the annulment stands. Nothing writes it yet.
	ReinstatedAt *time.Time
	// Check is the annulment's Annulment Check (#746), nil for one recorded
	// before the check existed: that reads unconfirmed and is never asked.
	Check *AnnulmentCheck
	// Notice is the buyer's Annulment Notice on this annulment (#748).
	Notice AnnulmentNotice
}

// AnnulmentNoticeState is what the annulments row records of the buyer's
// Annulment Notice (#748, spec #742). Held and due are not states of their
// own: an owed notice is held while its annulment's check is checking or at
// mismatch and due once it is confirmed or unconfirmed (AnnulmentNotice.Due).
type AnnulmentNoticeState string

const (
	// AnnulmentNoticeOwed: Mark annulled was recorded on a Sale Invoice the
	// buyer was sent, and the notice has not gone yet.
	AnnulmentNoticeOwed AnnulmentNoticeState = "owed"
	// AnnulmentNoticeSent: the sender took it. Never sent again.
	AnnulmentNoticeSent AnnulmentNoticeState = "sent"
	// AnnulmentNoticeDropped: a Reinstate withdrew it unsent.
	AnnulmentNoticeDropped AnnulmentNoticeState = "dropped"
	// AnnulmentNoticeNotOwed: a manual Tax Invoice, a factura the buyer was
	// never sent, or an annulment recorded before the notice existed.
	AnnulmentNoticeNotOwed AnnulmentNoticeState = "not_owed"
)

// AnnulmentNotice is one annulment's notice: its recorded state, when it
// was sent, and how many sends were tried.
type AnnulmentNotice struct {
	State    AnnulmentNoticeState
	SentAt   *time.Time
	Attempts int
}

// AnnulmentNoticeReleased reports whether a check's verdict releases an owed
// notice: confirmed or unconfirmed. Checking and mismatch hold it, and an
// annulment with no check (recorded before it existed) owes none.
func AnnulmentNoticeReleased(check *AnnulmentCheck) bool {
	return check != nil && (check.Verdict == AnnulmentConfirmed || check.Verdict == AnnulmentUnconfirmed)
}

// AnnulmentCheck is where one annulment's check stands: the verdict and the
// instant of the ask that reached it, when the next scheduled ask falls due
// (nil when none ever will), and every ask made, oldest first.
type AnnulmentCheck struct {
	Verdict   AnnulmentVerdict
	Since     *time.Time
	NextAskAt *time.Time
	Asks      []AnnulmentAsk
}

// AnnulmentRequest is what the operator types to confirm Mark annulled on a
// factura: the document's number in full, an optional note, and past the
// window the acknowledgement that the portal took it. A Credit Note's Mark
// annulled reads none of it.
type AnnulmentRequest struct {
	Number           string
	Note             string
	LateAcknowledged bool
}
