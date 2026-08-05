package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/googleauth"
)

// VerifyGoogleSignIn completes a Google Sign-In on the Storefront and issues a
// Customer Session.
//
// The work it does itself stops at establishing an email address: the
// authorization code the Storefront relayed is redeemed at Google's token
// endpoint with the Storefront client's own secret, and Google's `email` claim
// is the answer. From there this is the passcode path, exactly — the same
// create-or-reuse-and-stamp step, the same 180-day sliding session, the same
// view on the wire. A Google Sign-In is what makes someone a Verified Customer
// by the same rule a passcode does, and an address no Ticket Sale has ever
// reached gets the record its first sale would have reused.
//
// Google proves the email; it does not become the identity. The `sub` claim is
// not stored, because nothing in this system resolves a person by anything but
// their email (ADR 0011).
//
// The email is normalised through platform.NormalizeEmail and by nothing else:
// Google's canonical address is an ordinary address, and no Gmail dot or alias
// folding happens here. Where it differs from what a box office recorded, the
// person is a different Customer — deliberately, since the alternative rewrites
// the identity key of every existing row.
// locale is the Locale of the Storefront the sign-in started on, remembered as
// the Customer's Digest Locale exactly as the passcode path remembers it.
func (s *Service) VerifyGoogleSignIn(ctx context.Context, code, codeVerifier, redirectURI, locale string) (*CustomerSessionView, string, error) {
	identity, err := s.google.VerifiedIdentity(ctx, googleauth.Exchange{
		Code:         code,
		CodeVerifier: codeVerifier,
		RedirectURI:  redirectURI,
	})
	if err != nil {
		// One generic error, whatever went wrong, and nothing written: a refused
		// exchange leaves no Customer and no Customer Session behind.
		return nil, "", err
	}

	// The picture URL rides along to seed a Customer Avatar into an empty slot
	// and for nothing else — it proves nothing, names nobody, and an upload the
	// person made is never overwritten by it (see seedAvatarFromGoogle).
	return s.signInProvenEmail(ctx, platform.NormalizeEmail(identity.Email), s.now(), identity.PictureURL, locale)
}
