// Package consent owns the Privacy Policy, its Policy Versions, and — from #251
// — the evidence of what each Customer authorized.
//
// It is its own module rather than a corner of customers because of who reads
// it. The Privacy Policy is published to anyone at all, signed in or not; a
// Policy Version gates sign-in AND checkout, so both identity and sales depend
// on it; and the Consent Records that follow are evidence about a person rather
// than part of that person's account. Folding this into customers would have
// made the module that owns Customer Sessions also own the thing that decides
// whether a session may be minted, which is a cycle waiting to be discovered.
package consent

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrPolicyLocaleNotPublished is returned when the Privacy Policy is asked for
// in a language this platform does not publish it in.
//
// 404, and deliberately not a fallback to English. A notice the reader cannot
// read, served under their own language's address, would be worse than an
// honest absence: the Storefront's own routing already refuses a Locale it does
// not serve, so reaching this means somebody called the API directly.
func ErrPolicyLocaleNotPublished() apperror.DomainError {
	return apperror.New(
		"POLICY_LOCALE_NOT_PUBLISHED",
		"The Privacy Policy is not published in that language.",
		nil,
	)
}

// ErrPolicyAcceptanceRequired is returned when a capture surface submits
// without accepting the current Policy Version.
//
// THE BACKEND REFUSES THIS, not merely a disabled button on a form. A checkbox
// the Storefront declines to enable is a good way to tell somebody what is
// required and no way at all to guarantee it: the API is public, the sign-in
// consent endpoint is unauthenticated by construction, and a required consent
// that only the UI enforces is a required consent that a curl command does not
// have. Every surface that can create a Customer Session or complete an Online
// Sale checks it here.
//
// 400 rather than 403: nothing about the caller is unauthorized — the request
// is simply not one the platform may act on, and restating it with the box
// ticked is exactly what fixes it.
func ErrPolicyAcceptanceRequired() apperror.DomainError {
	return apperror.New(
		"POLICY_ACCEPTANCE_REQUIRED",
		"The Privacy Policy must be accepted to continue.",
		nil,
	)
}

// ErrNoCurrentPolicyVersion is returned when no Policy Version is in effect.
//
// It should be unreachable: migration 060 seeds one, and a version is never
// deleted. It exists because the alternative — serving the embedded text with
// no version label beside it — would publish a notice nobody can be recorded as
// having accepted, which is the one failure this feature must not have quietly.
func ErrNoCurrentPolicyVersion() apperror.DomainError {
	return apperror.New(
		"NO_CURRENT_POLICY_VERSION",
		"No Privacy Policy version is in effect.",
		nil,
	)
}

// ErrConsentGrantNotPermitted is returned when a capture on a withdraw-only
// channel tries to grant something (#271, parent #265).
//
// THE REFUSAL IS THE API'S, NOT THE FORM'S. The Operator surface offers no way
// to grant a consent, and that on its own would be a statement about a page
// rather than a guarantee about the platform: a page has no say over what a
// curl command sends. An Operator who could grant could manufacture the very
// consent they exist to honour the withdrawal of, so the refusal lives in the
// one consent-write path and every caller of it meets the same wall.
//
// 400 rather than 403: nothing about the caller is unauthorized — an Operator is
// entitled to be here and entitled to withdraw — the request simply asks for
// something no channel of this kind may ever do.
func ErrConsentGrantNotPermitted() apperror.DomainError {
	return apperror.New(
		"CONSENT_GRANT_NOT_PERMITTED",
		"Consent cannot be granted on this surface. It can only be withdrawn.",
		nil,
	)
}

// ErrTermsLocaleNotPublished is returned when the Terms are asked for in a
// language this platform does not publish them in.
//
// 404, and deliberately not a fallback, ErrPolicyLocaleNotPublished's rule
// applied to the contract. The Terms are published in both Locales the
// Storefront serves — Spanish, which prevails (§37), and an English courtesy
// translation that says so in its own first line — so reaching this means
// somebody asked for a third language, and answering it with either of the two
// would put a document the reader did not ask for under their own language's
// address.
func ErrTermsLocaleNotPublished() apperror.DomainError {
	return apperror.New(
		"TERMS_LOCALE_NOT_PUBLISHED",
		"The Terms are not published in that language.",
		nil,
	)
}

// ErrTermsAcceptanceRequired is returned when a submission that owes the Terms
// box arrives without it ticked (#536, ADR 0066).
//
// The refusal is the API's, exactly as ErrPolicyAcceptanceRequired's is: the
// disabled submit button is a courtesy, this is the guarantee. 400 rather than
// 403 for the same reason — nothing about the caller is unauthorized, and
// restating the request with the box ticked is exactly what fixes it.
func ErrTermsAcceptanceRequired() apperror.DomainError {
	return apperror.New(
		"TERMS_ACCEPTANCE_REQUIRED",
		"The Términos y Condiciones must be accepted to continue.",
		nil,
	)
}

// ErrAdulthoodDeclarationRequired is returned when a submission that owes the
// Adulthood Declaration box arrives without it ticked (#586, ADR 0069).
//
// IT LIVES HERE, BESIDE THE TWO ACCEPTANCE REFUSALS, because all four capture
// points share it: the sign-in consent step (#586), the staff interstitial and
// staff sign-in gate (#587) and the online checkout (#588) each refuse the same
// untick with the same code, and a second spelling of it on the staff side
// would be a second thing to keep true about one rule.
//
// 400 rather than 403, exactly as ErrTermsAcceptanceRequired is: nothing about
// the caller is unauthorized — the platform has no idea how old anybody is and
// is not claiming to — and restating the request with the box ticked is
// precisely what fixes it. A 403 would say "you may not", which is a verdict
// about a person this platform is in no position to reach.
//
// IT IS RETURNED BEFORE ANY Capture, and that ordering is the feature. Nothing
// whatever is written: no Consent Record, no current state, no Staff Terms
// Acceptance, no held answer on a Payment, no session. The platform keeps NO
// RECORD OF ANYONE WHO SAYS THEY ARE A MINOR — such a row would be a permanent,
// unverified assertion that a named individual is a child, on a table that is
// never edited and never deleted, about the one population the Privacy Policy
// promises not to knowingly process, and it would go stale in the worst
// direction as that person turned eighteen with no edit path to say so.
//
// THE MESSAGE STATES THE RULE AND NOT A FIELD VALIDATION. "The box is required"
// would be a sentence about a form; what the contract says is that only persons
// who have reached eighteen years of age may buy through Multiticketing, and
// that is what a person who unticks it is being told.
func ErrAdulthoodDeclarationRequired() apperror.DomainError {
	return apperror.New(
		"ADULTHOOD_DECLARATION_REQUIRED",
		"You must be eighteen or older to use Multiticketing.",
		nil,
	)
}

// ErrNoCurrentTermsVersion is returned when no Terms Version is in effect.
//
// Unreachable for the reason ErrNoCurrentPolicyVersion is: migration 105 seeds
// edition 1 and a version is never deleted. It exists because serving the
// embedded contract with no version label beside it would publish a text
// nobody can be recorded as having accepted.
func ErrNoCurrentTermsVersion() apperror.DomainError {
	return apperror.New(
		"NO_CURRENT_TERMS_VERSION",
		"No Terms version is in effect.",
		nil,
	)
}
