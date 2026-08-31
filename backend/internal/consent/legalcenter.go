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

// The Legal Center's REVIEW refusals (#562). Unlike the five above they are not
// about the shape of a draft but about the state of one: what an operator has
// asked to look at has to exist first.

// ErrLegalDraftNotStored is returned when a preview or a diff is recorded
// against a document that has no saved draft.
//
// 409 and not 400: the request was well formed and the operator was entitled to
// make it, and what stands in the way is a fact about the draft — there is not
// one — that no restatement of the body can fix. A preview promises a look at
// the text that WILL be published, and unsaved text will not be; the editor
// therefore offers the preview only once the draft is saved, and this is the
// backstop rather than a sentence anybody should have to read.
func ErrLegalDraftNotStored() apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_NOT_STORED",
		"Save the draft before previewing it.",
		nil,
	)
}

// ErrLegalDraftCellNotFound is returned when a preview names an artifact and a
// language the draft has no text for.
//
// A cell with nothing written in it is not a thing that can be previewed, and
// pretending it was would let an empty cell count towards "everything has been
// previewed" — the one claim that rule exists to make. An empty cell is a hole,
// and holes are refused by the completeness rule at publish (#563), not here.
func ErrLegalDraftCellNotFound(slug, locale string) apperror.DomainError {
	return apperror.New(
		"LEGAL_DRAFT_CELL_NOT_FOUND",
		"That artifact has no text in that language yet.",
		map[string]string{"slug": slug, "locale": locale},
	)
}

// The Legal Center's PUBLISH refusals (#563). A third reader again: everything
// above is about a draft's SHAPE or a draft's STATE, and each of these is about
// an ACT that a reader would be able to see. They are also the reason there is
// no approval step — production holds one platform_operators row, so a
// two-person rule deadlocks on every act — because the substitute for a second
// pair of eyes is the two review gates below plus an overnight delay: proving
// the operator was SHOWN the consequence, not that anybody agreed with it.

// ErrLegalPublishKindUnknown is returned when the publish call names something
// that is neither a new edition nor a correction.
//
// There is no default. Which of the two acts is being performed decides whether
// the entire customer base is re-gated, and a missing field must never resolve
// to either one.
func ErrLegalPublishKindUnknown(kind string) apperror.DomainError {
	return apperror.New(
		"LEGAL_PUBLISH_KIND_UNKNOWN",
		"Say whether this is a new edition or a correction.",
		map[string]string{"kind": kind},
	)
}

// ErrLegalPublishIncomplete is returned when the draft has a hole in it: an
// artifact with nothing written in a language the draft intends to publish.
//
// Refused so that no reader ever meets a document with a gap — a mandatory
// checkbox with a blank label beside it, or a Short Notice that renders as
// nothing above the consent boxes. The gaps travel in the details, named cell by
// cell, because "it is incomplete" is not something an operator can act on and
// "short-notice has no Spanish" is.
func ErrLegalPublishIncomplete(gaps any) apperror.DomainError {
	return apperror.New(
		"LEGAL_PUBLISH_INCOMPLETE",
		"Every artifact must be written in every language this draft publishes.",
		map[string]any{"gaps": gaps},
	)
}

// ErrLegalPublishNotPreviewed is returned when some cell of the draft has not
// been seen rendered as a reader will see it (#562).
//
// 409 and not 400: the request was well formed and the operator was entitled to
// make it, and what stands in the way is a fact about the draft that no
// restatement of the body can fix. It is HALF THE SUBSTITUTE FOR REVIEW, and it
// is enforced at this seam and not only in the browser for exactly that reason —
// a precondition a client could decline to check is not a precondition.
func ErrLegalPublishNotPreviewed(gaps any) apperror.DomainError {
	return apperror.New(
		"LEGAL_PUBLISH_NOT_PREVIEWED",
		"Preview every artifact, in every language, before publishing.",
		map[string]any{"gaps": gaps},
	)
}

// ErrLegalPublishDiffNotSeen is returned when the diff between this draft and
// the edition people are held to today has not been put on screen.
//
// The other half of the substitute for review, and it lapses ON ITS OWN when
// either side moves: rewrite a paragraph and it stops counting, and so does
// somebody publishing underneath the draft. That second case is why there is no
// separate "the base is stale" refusal — a diff is a statement about a PAIR, so
// a publication underneath the draft is already caught here.
func ErrLegalPublishDiffNotSeen() apperror.DomainError {
	return apperror.New(
		"LEGAL_PUBLISH_DIFF_NOT_SEEN",
		"Look at what this draft changes before publishing it.",
		nil,
	)
}

// ErrLegalCorrectionStructural is returned when a correction would add or remove
// an artifact.
//
// The one structural change the code can PROVE is not a typo. A correction says
// "the words were wrong"; an edition that gained or lost an artifact is not the
// same document with better words — somebody is now being asked for a consent
// they were not asked for, or has stopped being asked for one — and that
// re-gates people however small the text is.
func ErrLegalCorrectionStructural() apperror.DomainError {
	return apperror.New(
		"LEGAL_CORRECTION_STRUCTURAL",
		"A correction cannot add or remove an artifact.",
		nil,
	)
}

// ErrLegalCorrectionLocaleSetChanged is returned when a correction would publish
// a different set of languages.
//
// Refused for a reason about the FINGERPRINT rather than about the words: the
// locale set is part of the hash preimage (legal.ContentHash frames each
// language's code before its artifacts), so adding or dropping a language
// RESHAPES what is hashed rather than merely changing what is hashed. A reshaped
// preimage cannot be passed off as a fingerprint touch-up.
func ErrLegalCorrectionLocaleSetChanged() apperror.DomainError {
	return apperror.New(
		"LEGAL_CORRECTION_LOCALE_SET_CHANGED",
		"A correction cannot change which languages the document is published in.",
		nil,
	)
}

// ErrLegalCorrectionEmptyDiff is returned when a correction would change
// nothing.
//
// A correction that corrects nothing cannot be recorded: the typed reason would
// be a sentence about an act that did not happen. AN EMPTY DIFF IS PUBLISHABLE
// AS A GATING EDITION, deliberately and by the absence of this check on that
// path — re-gating over unchanged text is a real thing an operator may need, and
// migration 110 keeps `content_hash` free of a UNIQUE precisely so it can be
// done.
func ErrLegalCorrectionEmptyDiff() apperror.DomainError {
	return apperror.New(
		"LEGAL_CORRECTION_EMPTY_DIFF",
		"This draft says exactly what is published, so there is nothing to correct.",
		nil,
	)
}

// ErrLegalCorrectionReasonRequired is returned when a correction carries no
// typed reason, or one too short to be one.
//
// The one thing that cannot be recovered from the bytes is WHY somebody thought
// the words were wrong. The diff says what moved; the reason says what it was
// for, and a correction is the act with no other justification on the record —
// a gating edition's justification is the edition itself.
func ErrLegalCorrectionReasonRequired() apperror.DomainError {
	return apperror.New(
		"LEGAL_CORRECTION_REASON_REQUIRED",
		"Say what this correction fixes.",
		nil,
	)
}

// ErrLegalCorrectionEffectiveDateRefused is returned when a correction names an
// effective date.
//
// A CORRECTION TAKES EFFECT IMMEDIATELY, so a typo fix does not wait overnight —
// there is no date to choose and the editor offers none. Refused rather than
// ignored: silently discarding a date somebody typed is how an operator comes to
// believe they scheduled something.
func ErrLegalCorrectionEffectiveDateRefused() apperror.DomainError {
	return apperror.New(
		"LEGAL_CORRECTION_EFFECTIVE_DATE_REFUSED",
		"A correction takes effect immediately and cannot be scheduled.",
		nil,
	)
}

// ErrLegalEffectiveDateInvalid is returned for an effective date that is not a
// date. Date-only, because an effective date is a legal fact stated on the
// document itself and published to the day.
func ErrLegalEffectiveDateInvalid(raw string) apperror.DomainError {
	return apperror.New(
		"LEGAL_EFFECTIVE_DATE_INVALID",
		"An effective date is a calendar day, written as YYYY-MM-DD.",
		map[string]string{"effective_date": raw},
	)
}

// ErrLegalGatingEffectiveDateTooSoon is returned when a gating publication would
// take effect today, or earlier, or before an edition that is already scheduled.
//
// THE OVERNIGHT DELAY IS THE SUBSTITUTE FOR A SECOND PAIR OF EYES. What is
// irreversible about a publication is not the text — a later edition can restate
// it — but the RE-GATING, so the delay lands only on the acts that move the
// gating floor and never on a correction. "Now" is refused for a gating publish,
// and that is what buys the night in which the operator can change their mind.
//
// `earliest` is the first day this document may be re-gated on: the LATER of
// tomorrow and the day after the newest edition already published. An edition
// effective before one that already exists would sort below it forever and
// re-gate nobody, ever, which is the one outcome a re-gating button must not
// have quietly.
func ErrLegalGatingEffectiveDateTooSoon(earliest string) apperror.DomainError {
	return apperror.New(
		"LEGAL_GATING_EFFECTIVE_DATE_TOO_SOON",
		"A new edition takes effect no earlier than tomorrow.",
		map[string]string{"earliest_effective_date": earliest},
	)
}

// ErrLegalProtectedLocaleRequired is returned when either publication would stop
// publishing the language its document may not be published without.
//
// TWO DOCUMENTS, TWO SENTENCES, TWO FOOTINGS, and the copy is ruled verbatim
// because this is the one refusal a regulator is likely to read:
//
//   - The Privacy Policy cites the LOPDP BY NAME AND WITH NO ARTICLE NUMBER.
//     Nothing in this repository derives the LANGUAGE OF THE NOTICE from a
//     numbered article, and an unchecked legal claim in this sentence would be
//     exactly backwards. The law's name is a proper noun and stays untranslated
//     in every locale.
//   - The Términos y Condiciones cite §37, which is verifiable in the document's
//     own text: the Spanish is the contract, and the English one is a
//     translation of it.
//
// Neither "prevailing" nor "mandatory" appears in either sentence. Those are the
// names of the two constants (terms.PrevailingLocale, policy.MandatoryLocale),
// and the reason there are two names is that one rests on a clause the
// Foundation could amend and the other on a statute nobody here can.
//
// REFUSED IN BOTH PUBLISH KINDS. A gating edition that dropped Spanish would
// leave the platform with no notice in the language the law requires it in, and
// a correction that did so would do the same while claiming to have re-gated
// nobody.
func ErrLegalProtectedLocaleRequired(document, locale, message string) apperror.DomainError {
	return apperror.New(
		"LEGAL_PROTECTED_LOCALE_REQUIRED",
		message,
		map[string]string{"document": document, "locale": locale},
	)
}

// The Legal Center's CANCELLATION refusals (#564). A fourth reader again: the
// operator who has changed their mind overnight.
//
// There are only two of them, and there are only two on purpose. CANCELLING IS
// UNGATED AND IMMEDIATE — no reason, no delay, no confirmation ceremony and no
// approval step — because undoing is always cheaper than doing: the act being
// withdrawn re-gates the entire customer base, and the act of withdrawing it,
// before a word of it has been on any screen, moves nobody. Both refusals below
// are therefore about the act being IMPOSSIBLE rather than about it being
// unwise.

// ErrLegalEditionNotFound is returned when the id names no edition of this
// document.
//
// 404, on ErrLegalDocumentNotFound's terms: the address named an edition and
// there is no such edition. The two documents are separate tables (ADR 0066), so
// a Terms edition's id offered on the Policy's path is not found either — one
// document's surface must not confirm the existence of the other's rows.
func ErrLegalEditionNotFound() apperror.DomainError {
	return apperror.New(
		"LEGAL_EDITION_NOT_FOUND",
		"There is no such edition of this document.",
		nil,
	)
}

// ErrLegalEditionAlreadyEffective is returned when the edition's day has already
// come.
//
// THE CONTROL IS GONE BY THEN, so nobody should ever read this sentence: the
// banner and its button describe editions that are still waiting, and an edition
// that took effect at midnight has left that list on its own. This is the
// backstop for the page somebody left open overnight — and the rule is enforced
// at the seam, in the same statement that does the writing, because an interface
// that hides an act is not the same thing as a platform that refuses it.
//
// It is not a "you may not": once the day has come, people are being held to
// those words, and taking them back is a PUBLICATION — a new edition, with a
// diff, a headcount and a night of its own — rather than an undo. The effective
// date travels in the details so the screen can name the day that passed.
func ErrLegalEditionAlreadyEffective(effectiveDate string) apperror.DomainError {
	return apperror.New(
		"LEGAL_EDITION_ALREADY_EFFECTIVE",
		"This edition has already taken effect and can no longer be cancelled.",
		map[string]string{"effective_date": effectiveDate},
	)
}
