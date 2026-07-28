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

// ErrCustomerSessionScopeInsufficient is returned when a valid Customer Session
// is too narrow for what it was presented for: a Confirmation Link session
// asking to edit the Customer's profile.
//
// It is 403 rather than 401 because the caller's credential is genuine and
// re-presenting it will never help. What they need is a wider one, and the
// message says so: only Proof of Email Ownership earns the right to rewrite what
// the platform holds about that person, because a Confirmation Link is
// possession of a forwarded email and nothing more (#102, ADR 0010).
func ErrCustomerSessionScopeInsufficient() apperror.DomainError {
	return apperror.New(
		"CUSTOMER_SESSION_SCOPE_INSUFFICIENT",
		"Sign in with a passcode to change your details.",
		nil,
	)
}

// ErrReversalRequiresFullSession is the same refusal for the one action that
// moves money: a Confirmation Link session asking to undo a Ticket Sale.
//
// It shares the code above because it is the same fact — this credential is too
// narrow, and a wider one is what would help — and carries its own message
// because "change your details" is not what this caller was trying to do. The
// stakes are higher here than on the profile edit: a Confirmation Link arrives
// in an inbox and gets forwarded, and reversal is the first mutating,
// money-moving action a Customer can take (ADR 0018).
func ErrReversalRequiresFullSession() apperror.DomainError {
	return apperror.New(
		"CUSTOMER_SESSION_SCOPE_INSUFFICIENT",
		"Sign in with a passcode to undo this purchase.",
		nil,
	)
}

// ErrAvatarUploadUnavailable is returned when the service holds no object
// storage. A deployment fault, not a caller error, mirroring
// ErrConfirmationLinkUnavailable's posture: refuse plainly rather than degrade.
func ErrAvatarUploadUnavailable() apperror.DomainError {
	return apperror.New("AVATAR_UPLOAD_UNAVAILABLE", "Photo uploads are not available.", nil)
}

// ErrInvalidAvatarImageKey is returned when an Avatar write names a content type
// outside the image allowlist or an object key outside the signed-in Customer's
// own prefix. The two share one code: both are "that is not an Avatar you may
// attach", and distinguishing them helps only a caller probing the key scheme.
func ErrInvalidAvatarImageKey() apperror.DomainError {
	return apperror.New("AVATAR_IMAGE_INVALID", "That image can't be used as your photo.", nil)
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
