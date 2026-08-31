package consent

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// The acceptance browsers' refusals (#565, spec #556, ADR 0067).
//
// A file of their own, beside legalcenter.go's drafting refusals rather than in
// it, because they are about a different act. Those are refusals an operator
// AUTHORING an edition provokes; these two are refusals an operator BROWSING
// the population provokes, and the browsers publish nothing and draft nothing.
//
// There are only two, and the shortness is the design. The browsers have no
// per-edition filter, no total, no CSV and no export, so there is almost
// nothing a caller can ask for that does not exist: a document, a standing, a
// cursor and a search fragment. An unparseable cursor is not among these — it
// is treated as ABSENT and serves the first page, following the public event
// feed's rule (catalog/service.decodeCursor), because a bad cursor is somebody
// with a stale bookmark and not somebody making a mistake worth a sentence.

// ErrLegalStandingUnknown is returned when a browser is asked to filter by a
// state that is not one of the four.
//
// FOUR STATES AND ONLY FOUR — Current, Outstanding, Never seen and Former —
// and the closed vocabulary is enforced rather than assumed. The offending
// token travels in the details so the screen can say which value it sent, and
// the refusal is a 400: the caller can fix it by asking for a state that
// exists.
//
// It is deliberately NOT "unknown filters are ignored". A filter silently
// widened to everybody would answer "who owes an acceptance?" with the whole
// customer base and look, on a screen with no total, exactly like a re-gate.
func ErrLegalStandingUnknown(token string) apperror.DomainError {
	return apperror.New(
		"LEGAL_STANDING_UNKNOWN",
		"That is not one of the acceptance states.",
		map[string]string{"standing": token},
	)
}

// ErrLegalStandingNotAvailable is returned when a browser is asked for a state
// that exists but not on that screen.
//
// Today that is exactly one case: FORMER ON THE CUSTOMER BROWSER. A Customer
// never becomes former — Customer records are never deleted, because a Ticket
// Sale is a financial record an Organization must be able to reconcile
// (migration 016) — so there is no departure to observe and the only way to
// answer would be to guess from inactivity and present the guess as a fact.
//
// SEPARATE FROM ErrLegalStandingUnknown, and the distinction is worth a second
// error: "there is no such state" and "that state does not apply to these
// people" are different sentences, and a screen that conflated them would tell
// an operator their vocabulary was wrong when what was wrong was their
// population. 400, because it is fixed by asking for a state this screen has.
func ErrLegalStandingNotAvailable(token string) apperror.DomainError {
	return apperror.New(
		"LEGAL_STANDING_NOT_AVAILABLE",
		"That acceptance state does not apply to this population.",
		map[string]string{"standing": token},
	)
}
