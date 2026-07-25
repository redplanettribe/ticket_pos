package googleauth

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// ErrSignInFailed is the only error this package returns, and it is one error on
// purpose.
//
// A code Google rejected, an address Google will not vouch for, a token with no
// `email` claim, and a misconfigured deployment all end here with the same code
// and the same words. Every one of those is a failed Proof of Email Ownership,
// and the route that reports it is public and unauthenticated: a caller able to
// tell the causes apart could learn which addresses the platform knows, which is
// the oracle the passcode request endpoint is carefully built to deny. The cause
// is written to the log, where the person answering for the platform can read
// it and a stranger cannot.
func ErrSignInFailed() apperror.DomainError {
	return apperror.New("GOOGLE_SIGN_IN_FAILED", "Sign-in failed. Please try again.", nil)
}
