package platform

import (
	"context"
	"errors"
	"fmt"
)

// ErrDigestSenderUnconfigured is returned by every Digest send on a deployment
// that has no Digest sending identity.
//
// It is a named error rather than a generic one so a caller can tell a
// MISCONFIGURATION from a provider outage. The two look identical from the
// drain's point of view — a Digest that did not go out — but one is fixed by
// waiting and the other never is.
var ErrDigestSenderUnconfigured = errors.New("follow digest sending identity is not configured")

// DigestEmailSender delivers the weekly Follow Digest, and nothing else.
//
// It is a separate, one-method interface from EmailSender because the Digest is
// the one message on a DIFFERENT sending domain (#225, ADR 0030). Splitting the
// interface is what lets the two identities be held by two different objects at
// once, which is the whole mechanism: there is no field to set wrong and no
// branch inside a sender deciding which From to use.
type DigestEmailSender interface {
	SendFollowDigest(ctx context.Context, digest FollowDigest) error
}

// SplitEmailSender is the EmailSender a production deployment runs: every
// transactional message goes to one sender, the Follow Digest to another.
//
// The transactional sender is EMBEDDED, so the eight transactional methods are
// literally the transactional sender's own and cannot drift onto the Digest
// identity by an oversight here. Only SendFollowDigest is overridden.
type SplitEmailSender struct {
	// EmailSender is the TRANSACTIONAL sender: One-time Passcodes, Sale
	// Confirmations, void and refused-reversal notices, and the five Payout
	// Request notices. It is untouched by anything the Digest does.
	EmailSender
	// Digest is the marketing sender, on its own subdomain and its own provider
	// credentials — or the refusing sender below, when this deployment has none.
	Digest DigestEmailSender
}

// NewSplitEmailSender routes Digests to digest and everything else to
// transactional.
func NewSplitEmailSender(transactional EmailSender, digest DigestEmailSender) *SplitEmailSender {
	return &SplitEmailSender{EmailSender: transactional, Digest: digest}
}

// SendFollowDigest sends on the Digest identity, never on the transactional one.
func (s *SplitEmailSender) SendFollowDigest(ctx context.Context, digest FollowDigest) error {
	return s.Digest.SendFollowDigest(ctx, digest)
}

// UnconfiguredDigestSender is what a deployment gets when it has a transactional
// sender and no Digest identity. It sends nothing and says so.
//
// This is the SAFE DEGRADATION, and the choice it encodes is deliberate: the
// alternative — falling back to the transactional sender — would put marketing
// mail on the domain the One-time Passcodes depend on, which is precisely the
// coupling ADR 0030 exists to remove. A deployment that has not finished
// configuring the Digest gets no Digests, not Digests from the wrong domain.
//
// It refuses LOUDLY. Every refusal is logged at error level with the reason,
// because a silently dropped Digest is indistinguishable from a feature nobody
// uses, and the operator would have no way to tell the difference for weeks.
type UnconfiguredDigestSender struct {
	logger Logger
	// reason is the operator-facing explanation from
	// DigestEmailConfig.UnconfiguredReason — which half is missing, or that the
	// configured From is on the transactional domain.
	reason string
}

// NewUnconfiguredDigestSender builds the refusing sender. reason is carried into
// both the log line and the returned error, so neither leaves an operator
// guessing which setting to go and fix.
func NewUnconfiguredDigestSender(logger Logger, reason string) *UnconfiguredDigestSender {
	return &UnconfiguredDigestSender{logger: logger, reason: reason}
}

// SendFollowDigest refuses, loudly, and never sends.
//
// It returns an error rather than nil, which matters more than it looks: the
// caller records a Digest as sent on a nil error, so a quiet success here would
// write a ledger row saying a Customer had seen Events they were never shown —
// and those Events would then never appear in a later Digest.
func (s *UnconfiguredDigestSender) SendFollowDigest(_ context.Context, d FollowDigest) error {
	s.logger.Error(
		"follow digest not sent: no digest sending identity is configured",
		"email", d.To,
		"events", len(d.Events),
		"reason", s.reason,
	)
	return fmt.Errorf("%w: %s", ErrDigestSenderUnconfigured, s.reason)
}
