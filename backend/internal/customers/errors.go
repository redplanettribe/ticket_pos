// Package customers owns Customer identity: the platform-global Customer record,
// Customer Sessions, and the Customer Area read (ADR 0010).
//
// Its error codes are its own. A Customer Session failing is never reported with
// a staff SESSION_* code, because the two are unrelated records on unrelated
// surfaces and a Storefront client must not have to disambiguate them.
package customers

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrCustomerSessionNotFound is returned when no Customer Session exists for the
// token — including after sign-out, which destroys the record.
func ErrCustomerSessionNotFound() apperror.DomainError {
	return apperror.New("CUSTOMER_SESSION_NOT_FOUND", "Session not found.", nil)
}

// ErrCustomerSessionExpired is returned when the Customer Session's sliding
// window has run out.
func ErrCustomerSessionExpired() apperror.DomainError {
	return apperror.New("CUSTOMER_SESSION_EXPIRED", "Session expired. Please sign in again.", nil)
}

// ErrConfirmationLinkInvalid is returned when a Confirmation Link token is
// malformed, unsigned, signed with the wrong key, tampered with, or names a
// Ticket Sale that no longer exists.
//
// Those causes deliberately share one code and one message. Distinguishing them
// would tell whoever is holding the token which part of their guess was right,
// and the holder of a bad link has no legitimate use for that.
func ErrConfirmationLinkInvalid() apperror.DomainError {
	return apperror.New("CONFIRMATION_LINK_INVALID", "This link is not valid. Sign in with a passcode to see your tickets.", nil)
}

// ErrConfirmationLinkExpired is returned when a genuine Confirmation Link has
// outlived its Event plus the grace window. Separate from invalid because the
// recovery differs: this person really did buy a ticket and should be pointed at
// passcode sign-in, not told their link was forged.
func ErrConfirmationLinkExpired() apperror.DomainError {
	return apperror.New("CONFIRMATION_LINK_EXPIRED", "This link has expired. Sign in with a passcode to see your tickets.", nil)
}

// ErrConfirmationLinkUnavailable is returned when the service holds no signing
// key. It is a deployment fault, not a caller error, and it refuses rather than
// falling back to an unsigned or default-keyed link.
func ErrConfirmationLinkUnavailable() apperror.DomainError {
	return apperror.New("CONFIRMATION_LINK_UNAVAILABLE", "Confirmation links are not available.", nil)
}
