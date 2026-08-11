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
