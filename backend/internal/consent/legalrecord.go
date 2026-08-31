package consent

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// The per-subject consent record's refusals (#566, spec #556, ADR 0067).
//
// A file of its own beside legalbrowsing.go's and legalcenter.go's, for the
// reason those two are separate from each other: a different reader provokes
// them. Those are refusals an operator BROWSING a population or AUTHORING an
// edition meets; this one is met by an operator READING ONE PERSON'S RECORD.
//
// THERE IS EXACTLY ONE, and the shortness is the design again. Almost nothing
// can go wrong on this path: the record is addressed by an opaque id, the
// history takes a cursor whose unparseable form is treated as absent (the
// browsers' rule), and the only act the screen offers already owns its
// refusals — CONSENT_GRANT_NOT_PERMITTED, from the platform's single
// consent-write path, which refuses a manufactured consent for every caller
// rather than for the ones that remembered.
//
// AND NOTE WHAT IS ABSENT. There is no refusal here for erasing somebody, for
// re-gating one person, or for withdrawing the Terms, because there is no
// route that could ever provoke one. An error code is the wrong place to record
// that an act does not exist; the right place is the absence of the act.

// ErrLegalSubjectNotFound is returned when the record is addressed to a person
// nobody is.
//
// 404, and NOT an empty record. An operator following a link from a browser
// page taken this morning, or a bookmark from last month, is entitled to be
// told the person is not there — being shown a blank record they might then act
// on is how a withdrawal gets filed against nobody.
//
// It carries NO DETAILS, and specifically not the id it was asked for. The id
// is already in the request line the caller sent; echoing it back into an error
// body puts it in one more place a log ships.
func ErrLegalSubjectNotFound() apperror.DomainError {
	return apperror.New(
		"LEGAL_SUBJECT_NOT_FOUND",
		"There is no consent record for that person.",
		nil,
	)
}
