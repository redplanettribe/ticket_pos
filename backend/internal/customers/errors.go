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

// ErrFollowRequiresFullSession is the same refusal once more, for the Follows
// (#217): a Confirmation Link session asking to Follow, Unfollow, or read what
// the Customer Follows.
//
// It shares the code above because it is the same fact — this credential is too
// narrow — and carries its own message because neither "change your details" nor
// "undo this purchase" is what this caller was doing.
//
// The reason it must be refused at all is worth stating, because the ticket's
// own wording ("an active Customer Session, which by definition implies a
// verified email") is true of a FULL Customer Session and not of this one. A
// sale-scoped session is minted by redeeming a Confirmation Link, which is
// possession of an email that was sent to somebody and nothing more — it does
// not mark the Customer verified. A Follow is a standing request to be written
// to (ADR 0030), so accepting one from a forwarded receipt would let a stranger
// subscribe another person's inbox to mail they never asked for, which ADR 0010
// forbids.
func ErrFollowRequiresFullSession() apperror.DomainError {
	return apperror.New(
		"CUSTOMER_SESSION_SCOPE_INSUFFICIENT",
		"Sign in with a passcode to follow organizations.",
		nil,
	)
}

// ErrFollowedOrganizationNotFound is returned when a Follow names a slug no
// Organization owns.
//
// It carries identity's code rather than one of its own, so that following an
// unknown Organization and reading its public profile answer alike — a caller
// learns nothing here about which Organizations exist that the public endpoint
// would not have told them.
func ErrFollowedOrganizationNotFound() apperror.DomainError {
	return apperror.New("ORGANIZATION_NOT_FOUND", "Organization not found.", nil)
}

// ErrFollowsUnavailable is returned when the service was built without the
// resolver that turns an Organization slug into an id. A deployment fault, not a
// caller error, mirroring ErrConfirmationLinkUnavailable's posture: refuse
// plainly rather than silently record a Follow of nothing.
func ErrFollowsUnavailable() apperror.DomainError {
	return apperror.New("FOLLOWS_UNAVAILABLE", "Following is not available.", nil)
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
// ErrUnsubscribeLinkInvalid is returned when an unsubscribe token is malformed,
// unsigned, signed with the wrong key, tampered with, signed for some other
// purpose, or names a Customer who no longer exists (#224, ADR 0030).
//
// Those causes share one code and one message for the reason
// ErrConfirmationLinkInvalid's do, and here the reason is sharper: this endpoint
// takes no credential at all, so an error that distinguished "bad signature"
// from "no such Customer" would be an unauthenticated oracle for whether a
// Customer id exists.
//
// It is 400 rather than the 401 a bad Confirmation Link gets, because nothing
// here is a sign-in: a Confirmation Link mints a session and failing to present
// a valid one is a failure to authenticate, where this token is a malformed
// argument to a request that authenticates nobody. There is no credential to
// re-present and no WWW-Authenticate answer that would help.
func ErrUnsubscribeLinkInvalid() apperror.DomainError {
	return apperror.New("UNSUBSCRIBE_LINK_INVALID", "This unsubscribe link is not valid. You can turn the digest off from your account instead.", nil)
}

// ErrUnsubscribeLinkUnavailable is returned when the service holds no signing
// key and so cannot mint or verify an unsubscribe link. A deployment fault, not
// a caller error, with ErrConfirmationLinkUnavailable's posture: refuse plainly
// rather than emit or accept an unsigned token, which would be an
// unauthenticated way to silence any Customer whose id somebody could guess.
func ErrUnsubscribeLinkUnavailable() apperror.DomainError {
	return apperror.New("UNSUBSCRIBE_LINK_UNAVAILABLE", "Unsubscribe links are not available.", nil)
}

func ErrConfirmationLinkUnavailable() apperror.DomainError {
	return apperror.New("CONFIRMATION_LINK_UNAVAILABLE", "Confirmation links are not available.", nil)
}

// ErrPendingConsentInvalid is returned when a pending-consent token is unknown,
// already spent, or past its short expiry (#251).
//
// One code and one message for all three causes, in the tradition of
// ErrConfirmationLinkInvalid above, and for a sharper reason than either: this
// token stands between Proof of Email Ownership and a Customer Session, so an
// error that distinguished "never existed" from "already used" would let a
// caller probe which halves of a sign-in some address has completed — a thinner
// version of the account-existence oracle the passcode request endpoint refuses
// to be.
//
// 401, like a bad passcode and a bad Confirmation Link: this token is a
// credential, and failing to present a valid one is a failure to prove
// something. The recovery is the same as theirs — start the sign-in again — and
// the message says so rather than describing the token.
func ErrPendingConsentInvalid() apperror.DomainError {
	return apperror.New("PENDING_CONSENT_INVALID", "This sign-in has expired. Please sign in again.", nil)
}
