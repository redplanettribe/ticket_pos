// Package googleauth implements the Google half of Google Sign-In: it takes an
// authorization code obtained in a browser, redeems it at Google's token
// endpoint with a surface's client credentials, and returns the email address
// Google says the person owns.
//
// Like the otp package, it is deliberately ignorant of what that proves. It
// mints no Staff Session, no Customer Session, and no record of any kind — a
// caller that obtains a verified email decides what to do with it. Nothing here
// may depend on a domain module.
//
// It is not a provider abstraction. Google is trusted to verify an address as
// rigorously as our own One-time Passcode does (ADR 0011); that argument is
// Google-specific and does not generalise, so nothing here is shaped to accept
// a second provider.
//
// Google proves the email; it is not an identity. The `sub` claim, and every
// other thing the token carries, is read past and thrown away: there is no
// provider table, no linked account, and nothing to unlink.
package googleauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// exchangeTimeout bounds the call to Google. A sign-in that hangs is a sign-in
// that failed, and the person is waiting on a page for it.
const exchangeTimeout = 10 * time.Second

// Credentials are one surface's registration with Google. Each surface has its
// own (ADR 0011): Google binds an authorization code to the client that
// requested it, so a code obtained on the Storefront cannot be redeemed with the
// Staff client's credentials. That isolation is a property of these values, not
// of a check in application code.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// configured reports whether a surface has credentials at all. A deployment
// without them — local development, where the button is hidden rather than
// broken — can still start; the exchange refuses.
func (c Credentials) configured() bool {
	return strings.TrimSpace(c.ClientID) != "" && strings.TrimSpace(c.ClientSecret) != ""
}

// Exchange is the authorization code a frontend relayed, with the PKCE verifier
// and the redirect URI it was obtained under. Google requires all three to match
// what it issued the code against.
//
// The frontend never names an email address; it relays a code only Google can
// turn into one (ADR 0011).
type Exchange struct {
	Code         string
	CodeVerifier string
	RedirectURI  string
}

// Client redeems authorization codes for one surface's Google OAuth client.
type Client struct {
	creds         Credentials
	tokenEndpoint string
	httpClient    *http.Client
	logger        platform.Logger
}

// New returns a client for one surface's credentials. tokenEndpoint is normally
// Google's own (platform.GoogleTokenEndpoint, which is what configuration
// defaults to); the integration suite substitutes a stub.
func New(creds Credentials, tokenEndpoint string, logger platform.Logger) *Client {
	if strings.TrimSpace(tokenEndpoint) == "" {
		tokenEndpoint = platform.GoogleTokenEndpoint
	}
	return &Client{
		creds:         creds,
		tokenEndpoint: tokenEndpoint,
		httpClient:    &http.Client{Timeout: exchangeTimeout},
		logger:        logger,
	}
}

// Identity is what an exchange yields: the email address Google vouches for,
// and — when the surface requested the `profile` scope — the URL of the
// person's Google profile picture.
//
// PictureURL is a convenience, never a fact about identity: it may be empty
// (scope not requested, or no photo set), it is not verified beyond arriving in
// the same token as the email, and no caller may resolve a person by it. The
// Storefront uses it to seed a Customer Avatar into an empty slot; the staff
// surface ignores it entirely.
type Identity struct {
	Email      string
	PictureURL string
}

// VerifiedEmail redeems the authorization code and returns the email address
// Google vouches for.
//
// Every failure — unconfigured credentials, a code Google rejects, a malformed
// token, a missing `email` claim, `email_verified` false — returns the same
// ErrSignInFailed. The reason is written to the log and never to the caller: a
// response that distinguished "Google refused this code" from "that address is
// unverified" would be an oracle on a public, unauthenticated route, which is
// exactly what the passcode request endpoint spends effort denying.
//
// The returned address is not normalised here. Normalisation is
// platform.NormalizeEmail's job and belongs to the caller that resolves a
// Customer or a Member by it, so there is one rule and one place it is applied.
func (c *Client) VerifiedEmail(ctx context.Context, in Exchange) (string, error) {
	identity, err := c.VerifiedIdentity(ctx, in)
	if err != nil {
		return "", err
	}
	return identity.Email, nil
}

// VerifiedIdentity is VerifiedEmail plus the profile picture URL, for the one
// surface that wants it. Same exchange, same failure posture, same single
// generic error.
func (c *Client) VerifiedIdentity(ctx context.Context, in Exchange) (Identity, error) {
	if !c.creds.configured() {
		// A deployment fault, not a caller error, so it is loud in the log and
		// silent on the wire.
		c.logger.Error("google sign-in: no client credentials configured; the exchange cannot be attempted")
		return Identity{}, ErrSignInFailed()
	}

	idToken, err := c.exchange(ctx, in)
	if err != nil {
		c.logger.Warn("google sign-in refused", "reason", err.Error())
		return Identity{}, ErrSignInFailed()
	}

	claims, err := parseIDTokenClaims(idToken)
	if err != nil {
		c.logger.Warn("google sign-in refused", "reason", err.Error())
		return Identity{}, ErrSignInFailed()
	}

	// The claim is the entire value being consumed, so there is no degraded
	// mode: an address Google will not vouch for proves nothing at all.
	if strings.TrimSpace(claims.Email) == "" {
		c.logger.Warn("google sign-in refused", "reason", "id token carries no email claim")
		return Identity{}, ErrSignInFailed()
	}
	if !claims.EmailVerified {
		c.logger.Warn("google sign-in refused", "reason", "id token reports the email as unverified")
		return Identity{}, ErrSignInFailed()
	}

	return Identity{Email: claims.Email, PictureURL: strings.TrimSpace(claims.Picture)}, nil
}

// exchange POSTs the authorization code to the token endpoint and returns the
// raw ID token. Its errors are diagnostic text for the log; none of them reaches
// the caller.
func (c *Client) exchange(ctx context.Context, in Exchange) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {in.Code},
		"code_verifier": {in.CodeVerifier},
		"redirect_uri":  {in.RedirectURI},
		"client_id":     {c.creds.ClientID},
		"client_secret": {c.creds.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call token endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Google answers a spent, forged, or cross-client code with 400
		// invalid_grant. The status is enough to diagnose; the body may echo the
		// code, so it is not logged.
		return "", fmt.Errorf("token endpoint returned status %d", resp.StatusCode)
	}

	var payload struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if payload.IDToken == "" {
		return "", fmt.Errorf("token response carries no id_token")
	}
	return payload.IDToken, nil
}

// idTokenClaims is everything this package reads from an ID token. Google sends
// more — `sub` above all — and none of it is stored anywhere (ADR 0011).
//
// `picture` arrives only from a surface that requested the `profile` scope (the
// Storefront does, since Customer Avatars; the staff surface stays on
// `openid email`). The names that scope also offers are deliberately NOT read:
// customer names have a precedence rule of their own (PRD decisions 42-44) that
// a third source would complicate, so `given_name` and `family_name` are read
// past exactly as `sub` is.
type idTokenClaims struct {
	Email         string       `json:"email"`
	EmailVerified flexibleBool `json:"email_verified"`
	Picture       string       `json:"picture"`
}

// parseIDTokenClaims reads the claims out of an ID token WITHOUT verifying its
// signature, and that is correct rather than an omission.
//
// The token was not relayed by a browser: it was just read off a direct TLS
// connection to Google's own token endpoint, whose certificate the exchange
// already validated. OIDC Core §3.1.3.7 says a client MAY skip signature
// validation in exactly that case, so there is no JWKS fetch, no key cache, and
// no key rotation to get wrong here. Do not add one — it would be a second,
// weaker check on a fact TLS already established.
func parseIDTokenClaims(idToken string) (idTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return idTokenClaims{}, fmt.Errorf("id token is not a three-part JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idTokenClaims{}, fmt.Errorf("decode id token payload: %w", err)
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return idTokenClaims{}, fmt.Errorf("parse id token claims: %w", err)
	}
	return claims, nil
}

// flexibleBool reads `email_verified` whether it arrives as a JSON boolean or as
// the string "true". Google's ID tokens use a boolean, but the same claim is a
// string elsewhere in Google's own APIs, and reading a genuine "true" as false
// would refuse a sign-in that should have worked. Anything else is false, which
// refuses — the safe direction.
type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", `"true"`:
		*b = true
	default:
		*b = false
	}
	return nil
}
