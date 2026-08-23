// Package otp implements the one-time passcode primitive: code generation,
// hashing, rate limiting by email and by IP, verify-attempt capping, expiry, and
// invalidation.
//
// It is deliberately ignorant of what a successful verification means. It never
// creates a Staff Session, a Customer Session, or any other credential — a
// caller that verifies a passcode decides what to mint. Nothing here may depend
// on a domain module.
//
// Every challenge carries a Purpose. Issuing records it; verifying requires an
// exact match, so a passcode minted for one surface is worthless on another.
// See ADR 0010.
package otp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

const (
	codeLength         = 6
	expiry             = 10 * time.Minute
	rateWindow         = 15 * time.Minute
	maxPerEmail        = 3
	maxPerIP           = 10
	maxVerifyAttempts  = 5
	codeUpperExclusive = 1000000

	// defaultGlobalCeiling is the platform-wide cap on passcode emails sent per
	// rateWindow, across every purpose. See Service.globalCeiling.
	defaultGlobalCeiling = 50
)

// Purpose scopes a challenge to the surface it was minted for. Verification
// requires an exact match: this is a privilege boundary between the staff and
// customer identity domains, not a label.
type Purpose string

const (
	// PurposeStaff scopes a challenge to staff sign-in (a Staff Session).
	PurposeStaff Purpose = "staff"
	// PurposeCustomer scopes a challenge to Customer sign-in (a Customer Session).
	PurposeCustomer Purpose = "customer"
)

// Valid reports whether the purpose is one this package recognises.
func (p Purpose) Valid() bool {
	return p == PurposeStaff || p == PurposeCustomer
}

// Service issues and verifies one-time passcodes.
type Service struct {
	repo   *Repository
	email  platform.EmailSender
	logger platform.Logger
	now    func() time.Time

	// globalCeiling caps passcode emails sent platform-wide per rateWindow,
	// across every purpose. It is deliberately blind to who is asking.
	//
	// The per-email and per-IP limits above assume the requester can be
	// identified. On a public sign-in form that assumption is weak: a per-email
	// cap does nothing against an attacker enumerating thousands of different
	// victims' addresses, and per-IP attribution is only as good as the
	// infrastructure supplying it. This ceiling is the one control that holds
	// regardless of what an attacker forges, and what it protects is the
	// reputation of the single verified sending subdomain shared by staff
	// passcodes, Sale Confirmations, and void notices (ADR 0009).
	//
	// It spans both purposes on purpose: its job is protecting one shared
	// sending domain, not fairness between the staff and Storefront surfaces.
	//
	// ACCEPTED TRADE-OFF, BY DESIGN — do not "fix" this. This is a shared-fate
	// control: a determined attacker can trip it and deny staff and Customers
	// alike their sign-in emails. That is the intended failure mode. A temporary
	// sign-in outage is recoverable; a burned sending domain is not. Scoping
	// the ceiling per-key to spare innocent traffic would give back exactly the
	// property that makes it worth having. See PRD decision 12.
	globalCeiling int
}

// New returns an OTP service.
func New(repo *Repository, email platform.EmailSender, logger platform.Logger) *Service {
	return &Service{
		repo:          repo,
		email:         email,
		logger:        logger,
		now:           time.Now,
		globalCeiling: defaultGlobalCeiling,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithGlobalCeiling overrides the platform-wide cap on passcode emails per
// window. A value of zero or less keeps the default, so an unset configuration
// can never accidentally mean "send nothing".
func (s *Service) WithGlobalCeiling(ceiling int) *Service {
	if ceiling > 0 {
		s.globalCeiling = ceiling
	}
	return s
}

// GlobalCeiling reports the platform-wide cap currently in force.
func (s *Service) GlobalCeiling() int {
	return s.globalCeiling
}

// Issue generates a passcode for the purpose, stores its hash, and delivers it
// in the named language.
//
// Rate-limit counters are scoped per purpose, so traffic on one surface cannot
// exhaust another surface's allowance. Issuing invalidates any earlier active
// challenge for the same email and purpose only.
//
// locale words THIS EMAIL AND NOTHING ELSE (ADR 0033). It is not stored on the
// challenge, not remembered against the address, and never written to a
// Customer's Mail Locale: only a completed sign-in does that, and asking for a
// passcode is not proof you own the address. This package holds no opinion about
// which language a caller names — it does not know what a Customer is.
func (s *Service) Issue(ctx context.Context, purpose Purpose, email, clientIP string, locale platform.Locale) error {
	if !purpose.Valid() {
		return fmt.Errorf("otp: unknown purpose %q", purpose)
	}
	email = platform.NormalizeEmail(email)
	now := s.now()

	since := now.Add(-rateWindow)
	emailCount, err := s.repo.CountRequestsByEmail(ctx, purpose, email, since)
	if err != nil {
		return err
	}
	if emailCount >= maxPerEmail {
		return ErrRateLimited()
	}

	ipCount, err := s.repo.CountRequestsByIP(ctx, purpose, clientIP, since)
	if err != nil {
		return err
	}
	if ipCount >= maxPerIP {
		return ErrRateLimited()
	}

	// Checked last, and only for a request that would otherwise send: a caller
	// already throttled on its own key should hear the ordinary rate-limit
	// error, so an operator can tell "this user is being throttled" apart from
	// "the platform is under attack".
	globalCount, err := s.repo.CountRequestsSince(ctx, since)
	if err != nil {
		return err
	}
	if globalCount >= s.globalCeiling {
		// Loud on purpose: this is the alerting signal. Reaching it means the
		// platform stopped sending passcodes to everyone, so we want to hear it
		// from our own monitoring rather than from a suspended email provider.
		s.logger.Error("otp global ceiling reached: passcode sending halted platform-wide",
			"alert", "otp_global_ceiling",
			"purpose", string(purpose),
			"sent_in_window", globalCount,
			"ceiling", s.globalCeiling,
			"window_seconds", int(rateWindow.Seconds()),
		)
		return ErrGlobalCeilingReached()
	}

	code, err := generateCode()
	if err != nil {
		return err
	}

	challengeID, err := newUUID()
	if err != nil {
		return err
	}

	if err := s.repo.InvalidateForEmail(ctx, purpose, email); err != nil {
		return err
	}

	challenge := Challenge{
		ID:          challengeID,
		Purpose:     purpose,
		Email:       email,
		CodeHash:    hashCode(challengeID, code),
		RequestIP:   clientIP,
		ExpiresAt:   now.Add(expiry),
		Attempts:    0,
		Invalidated: false,
		CreatedAt:   now,
	}
	if err := s.repo.Create(ctx, challenge); err != nil {
		return err
	}

	if err := s.email.SendOTP(ctx, email, code, locale); err != nil {
		// The address stays out of the log for the sender's reason (#377): the
		// challenge row already records who asked, and the sign-in fails loudly
		// on its own.
		s.logger.Error("send otp failed", "purpose", string(purpose), "error", err)
		return fmt.Errorf("send otp: %w", err)
	}

	return nil
}

// Verify checks a passcode against the active challenge for the email and
// purpose, and invalidates the challenge on success.
//
// A challenge issued under a different purpose is not merely rejected — it is
// invisible here, so a wrong-purpose attempt cannot consume the real
// challenge's attempt allowance either.
func (s *Service) Verify(ctx context.Context, purpose Purpose, email, code string) error {
	if !purpose.Valid() {
		return fmt.Errorf("otp: unknown purpose %q", purpose)
	}
	email = platform.NormalizeEmail(email)
	now := s.now()

	challenge, err := s.repo.LatestChallenge(ctx, purpose, email)
	if err != nil {
		return err
	}
	if challenge == nil {
		return ErrInvalid(0)
	}
	if challenge.Invalidated {
		return ErrExpired()
	}
	if now.After(challenge.ExpiresAt) {
		_ = s.repo.Invalidate(ctx, challenge.ID)
		return ErrExpired()
	}
	if challenge.Attempts >= maxVerifyAttempts {
		_ = s.repo.Invalidate(ctx, challenge.ID)
		return ErrAttemptsExceeded()
	}

	expected := hashCode(challenge.ID, code)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(challenge.CodeHash)) != 1 {
		attempts, incErr := s.repo.IncrementAttempts(ctx, challenge.ID)
		if incErr != nil {
			return incErr
		}
		if attempts >= maxVerifyAttempts {
			_ = s.repo.Invalidate(ctx, challenge.ID)
			return ErrAttemptsExceeded()
		}
		return ErrInvalid(maxVerifyAttempts - attempts)
	}

	return s.repo.Invalidate(ctx, challenge.ID)
}

func generateCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(codeUpperExclusive))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", codeLength, n.Int64()), nil
}

func hashCode(challengeID, code string) string {
	sum := sha256.Sum256([]byte(challengeID + ":" + code))
	return hex.EncodeToString(sum[:])
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
