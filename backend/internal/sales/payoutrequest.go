package sales

import "strings"

// The Payout Request: an Organization asking to be paid (ADR 0026, CONTEXT.md
// "Payout Request").
//
// This file holds the vocabulary the whole feature is stated in — the four
// statuses and the one bound on free text — so the service, the repository and
// migration 043 can never disagree about what a status is called. Everything
// about WHERE the money goes lives in payoutprofile.go beside it, because a
// request snapshots a profile and the two must be judged by one validator.

// The four states a Payout Request can be in. `pending → paid | declined |
// cancelled`, and there is deliberately no `approved`: an operator transfers the
// money and then records it, exactly as they always have, and an
// approved-but-unpaid request is a debt with a state name (ADR 0026).
//
// All three end states are final. A pending request cannot be edited, only
// cancelled and re-asked, which is what keeps `pending` genuinely singular — and
// singular is what the partial unique index in migration 043 enforces.
const (
	// PayoutRequestPending is the one outstanding state, and the only one the
	// partial unique index treats as occupying the Organization's single slot.
	PayoutRequestPending = "pending"
	// PayoutRequestPaid means an operator transferred the money and recorded the
	// Payout the request points at (#177).
	PayoutRequestPaid = "paid"
	// PayoutRequestDeclined means an operator considered it and said no, with a
	// reason the asker is shown (#177).
	PayoutRequestDeclined = "declined"
	// PayoutRequestCancelled means the Organization withdrew it. It is the only
	// end state this Organization-facing surface can reach.
	PayoutRequestCancelled = "cancelled"
)

// MaxPayoutRequestNoteLength bounds the note, mirroring the CHECK in migration
// 043. A note carries whatever the form does not — "before the festival,
// please" — and an unbounded column on a field an operator reads is an
// invitation to paste a contract into it.
const MaxPayoutRequestNoteLength = 500

// NormalizePayoutRequestNote trims the note and reports whether anything is
// left, so a note of pure whitespace is stored as SQL NULL rather than as an
// empty string pretending to be silence — a distinction somebody would otherwise
// have to test for at every read.
func NormalizePayoutRequestNote(note string) (string, bool) {
	trimmed := strings.TrimSpace(note)
	return trimmed, trimmed != ""
}
