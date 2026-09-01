package consent

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// The consent access log reader's refusals (#569, spec #556, ADR 0067).
//
// TWO, AND THERE IS ALMOST NOTHING ELSE A CALLER CAN GET WRONG, because there is
// almost nothing else to ask for: the reader takes an actor, an act, two dates
// and a cursor, and offers NO SUBJECT FILTER, NO PAGE SIZE, NO SORT ORDER and NO
// EXPORT. An unparseable cursor is not among these — it is treated as ABSENT and
// serves the first page, the rule the acceptance browsers and the record history
// already follow, because a bad cursor is a stale bookmark.
//
// Both are 400s: each is fixed by asking for something that exists.

// ErrLegalAccessActUnknown is returned when the log is asked to filter by an act
// that is not one of the four.
//
// FOUR ACTS AND ONLY FOUR — `list_read`, `subject_read`, `evidence_export` and
// `audit_read` — matching migration 116's CHECK, so the vocabulary is closed in
// the same place twice and cannot be widened on one side alone.
//
// IT IS NOT SILENTLY IGNORED, following ErrLegalStandingUnknown and for a
// sharper version of its reason: a filter widened to "everything" would show an
// operator the entire log while their screen said it was narrowed. On a browser
// that mistake produces a list that is too long; on an audit screen it produces
// a claim about what did and did not happen.
func ErrLegalAccessActUnknown(token string) apperror.DomainError {
	return apperror.New(
		"LEGAL_ACCESS_ACT_UNKNOWN",
		"That is not one of the logged access acts.",
		map[string]string{"act": token},
	)
}

// ErrLegalAccessDateInvalid is returned when either end of the date range is not
// a calendar day.
//
// A DAY (YYYY-MM-DD) AND NOT AN INSTANT, because "what happened on the 3rd" is
// the question, and a timestamp filter would make the answer depend on a
// timezone nobody chose. Refused rather than dropped: a date the server could
// not read, quietly ignored, would widen the window without saying so — and on
// this screen a wider window looks exactly like more access having happened.
func ErrLegalAccessDateInvalid(token string) apperror.DomainError {
	return apperror.New(
		"LEGAL_ACCESS_DATE_INVALID",
		"That is not a calendar day (YYYY-MM-DD).",
		map[string]string{"date": token},
	)
}
