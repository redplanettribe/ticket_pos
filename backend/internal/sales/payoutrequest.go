package sales

import "strings"

// The Payout Request: an Organization asking to be paid (ADR 0026, CONTEXT.md
// "Payout Request").
//
// This file holds the vocabulary the whole feature is stated in — the six
// statuses and the bounds on its free text — so the service, the repository and
// migrations 043 and 045 can never disagree about what a status is called.
// Everything about WHERE the money goes lives in payoutprofile.go beside it,
// because a request snapshots a profile and the two must be judged by one
// validator.

// The six states a Payout Request can be in (ADR 0026 and its amendment):
//
//	pending ──→ processing ──→ paid
//	   │            └────────→ failed
//	   ├──→ paid          (an instant transfer skips processing)
//	   ├──→ declined
//	   └──→ cancelled
//
// There is deliberately no `approved`. An operator transfers the money and then
// records it, exactly as they always have, and an approved-but-unpaid request is
// a debt with a state name. `processing` is not that state: it records a
// transfer that has ALREADY happened, in the world, outside the platform's
// control, and is resolved by the platform finding out what the bank did rather
// than by the platform keeping a promise.
//
// All four end states are final. A request that has not ended cannot be edited,
// only cancelled and re-asked while it is still `pending` — which is what keeps
// the Organization's single slot singular, and singular is what the partial
// unique index enforces (migrations 043, 045).
const (
	// PayoutRequestPending means nobody has answered the ask yet — UNTOUCHED BY
	// AN OPERATOR, which is what makes it the only state an Organization may
	// cancel from and an operator may decline from.
	//
	// It is not the definition of OUTSTANDING, which is the set of states
	// occupying the Organization's single slot and lives in exactly one place:
	// the outstandingPayoutRequest predicate in the sales repository, beside the
	// partial unique index that enforces it. The two named the same rows until
	// `processing` arrived, and were never the same question (#183, #184).
	PayoutRequestPending = "pending"
	// PayoutRequestProcessing means an operator has submitted the transfer and
	// the bank has not confirmed it (#184).
	//
	// NO PAYOUT EXISTS WHILE A REQUEST IS IN THIS STATE. A Payout is money that
	// moved (ADR 0014), and a transfer the bank later rejects must leave nothing
	// behind. It is outstanding — it holds the Organization's single slot — and
	// neither party may take the ask back: the bank is already acting on it.
	PayoutRequestProcessing = "processing"
	// PayoutRequestPaid means an operator transferred the money and recorded the
	// Payout the request points at (#177).
	PayoutRequestPaid = "paid"
	// PayoutRequestDeclined means an operator considered it and said no, with a
	// reason the asker is shown (#177).
	PayoutRequestDeclined = "declined"
	// PayoutRequestCancelled means the Organization withdrew it. It is the only
	// end state this Organization-facing surface can reach.
	PayoutRequestCancelled = "cancelled"
	// PayoutRequestFailed means the bank sent the transfer back, most often
	// because the account number was wrong.
	//
	// It is distinct from `declined` and must stay so: a decline is a judgement a
	// person made, a failure is a bank returning money and no judgement at all.
	// Collapsing them would tell an organizer with a typo that the platform
	// refused them.
	//
	// NO TRANSITION REACHES IT YET. The state is named here and permitted by
	// migration 045's CHECK because the vocabulary is written once; the
	// compare-and-swap that marks a `processing` request failed, with the reason
	// its asker reads, is #185.
	PayoutRequestFailed = "failed"
)

// MaxPayoutTransferReferenceLength bounds the reference the bank hands back when
// a transfer is submitted, mirroring the CHECK in migration 045. Stated here so
// an over-long one comes back as a field error naming the field rather than as a
// constraint violation an operator cannot act on.
const MaxPayoutTransferReferenceLength = 200

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
