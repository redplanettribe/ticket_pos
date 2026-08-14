package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
)

// VerifyGoogleSignIn completes a Google Sign-In on the Staff app and issues a
// Staff Session.
//
// The work it does itself stops at establishing an email address: the
// authorization code the Staff app relayed is redeemed at Google's token
// endpoint with the STAFF client's own secret, and Google's `email` claim is the
// answer. A code obtained on the Storefront is bound by Google to the Storefront
// client and fails here with `invalid_grant` — that isolation lives in the
// credentials, not in a check in this file, and must not be reimplemented as one
// (ADR 0011).
//
// From there this is the passcode path, exactly: the same session row, the same
// fourteen-day sliding window, the same auto-selection of a lone membership, and
// therefore the same auth-fork. A Member of one Organization lands on the
// dashboard, a Member of several on the picker, and an address with no
// membership at organization creation — because signInProvenEmail cannot tell
// which door was used.
//
// Nothing about the sign-in method is written anywhere. Somebody who used a
// passcode on Monday and Google on Tuesday holds one session history, on one
// email.
//
// The email is normalised through platform.NormalizeEmail and by nothing else.
// Authority comes from a `members` row matching that normalised address, so an
// address Google returns which differs from the one an invitation was sent to
// signs in fine and belongs to no Organization — deliberately, per PRD decision
// 5, with the create-organization fork's copy as the recovery.
// detectedLocale is the language the login page was rendered in, remembered as
// the Staff Locale on the same terms the passcode door remembers it: only when
// the person has none. Both doors are equal Proof of Email Ownership, and both
// were opened from a page that knew its own language.
func (s *Service) VerifyGoogleSignIn(ctx context.Context, code, codeVerifier, redirectURI, detectedLocale string) (*SessionView, string, error) {
	email, err := s.google.VerifiedEmail(ctx, googleauth.Exchange{
		Code:         code,
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
	})
	if err != nil {
		// One generic error, whatever went wrong, and nothing written: a refused
		// exchange leaves no Staff Session behind.
		return nil, "", err
	}

	return s.signInProvenEmail(ctx, platform.NormalizeEmail(email), detectedLocale, s.now())
}
