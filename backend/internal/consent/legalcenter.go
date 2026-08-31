package consent

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// The Legal Center's drafting refusals (#561, spec #556).
//
// They sit in a file of their own rather than at the bottom of errors.go
// because they are about a different reader. Everything in errors.go is a
// refusal a CUSTOMER or a capture surface can provoke — a policy not published
// in a language, a box left unticked. These five are refusals only a Platform
// Operator authoring an edition can reach, and only the Legal Center answers
// them.
//
// All five are about the SHAPE of a draft: a document that does not exist, a
// language the platform cannot render, a slug that names nothing or names the
// same thing twice. None is about a draft being UNFINISHED. An artifact with no
// Spanish yet is SAVED, because it is somebody's afternoon; it is the publish
// step that refuses to ship it (#563).

// ErrLegalDocumentNotFound is returned when the Legal Center is asked for a
// document that is not one of the two.
//
// 404, because the address named a document and there is no such document —
// the same answer POLICY_LOCALE_NOT_PUBLISHED gives, for the same reason.
func ErrLegalDocumentNotFound() apperror.DomainError {
	return apperror.New(
		"LEGAL_DOCUMENT_NOT_FOUND",
		"There is no such legal document.",
		nil,
	)
}

// ErrLegalDraftLocaleUnsupported is returned when a draft names a language the
// platform does not serve.
//
// The bound is `platform.ParseLocale`, a closed switch, and it is enforced here
// rather than by a CHECK constraint: publishing a language stopped needing a
// deploy in #558, but SERVING one never did. A draft in a third language would
// publish text no Storefront route can reach and no mail template can compose.
// The offending token travels in the details, so the editor can say which.
func ErrLegalDraftLocaleUnsupported(token string) apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_LOCALE_UNSUPPORTED",
		"That language is not one this platform publishes in.",
		map[string]string{"locale": token},
	)
}

// ErrLegalDraftLocalesRequired is returned when a draft would publish in no
// language at all.
//
// The published-language set is explicit precisely so that a half-translated
// language is a draft that CANNOT publish rather than one quietly dropped by an
// empty textarea. An empty set is that same failure taken to its end, and it is
// refused on the way in rather than left for publish to discover.
func ErrLegalDraftLocalesRequired() apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_LOCALES_REQUIRED",
		"A draft must publish in at least one language.",
		nil,
	)
}

// ErrLegalDraftSlugRequired is returned for an artifact with no slug.
//
// The slug is how a surface asks for its text — the Short Notice, a checkbox
// label, the policy itself — so an artifact without one is text that nothing
// can ever render.
func ErrLegalDraftSlugRequired() apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_SLUG_REQUIRED",
		"Every artifact needs a slug.",
		nil,
	)
}

// ErrLegalDraftDuplicateSlug is returned when one draft carries the same slug
// twice.
//
// It could not be stored — the primary key on `legal_draft_artifacts` says so
// — and it could not be published either: the slug is what a surface asks for,
// and two answers to one question is not a document.
func ErrLegalDraftDuplicateSlug(slug string) apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_DUPLICATE_SLUG",
		"That artifact appears twice in the draft.",
		map[string]string{"slug": slug},
	)
}
