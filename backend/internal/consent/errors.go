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
