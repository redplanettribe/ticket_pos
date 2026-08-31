package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	consentrepo "github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/identity/repository"
)

// pendingTermsDuration is how long a held staff sign-in is worth finishing:
// long enough to read the checkbox label and follow the link to the full text,
// short enough that this is not a second credential lying about. The customer
// side's pending consent window, for its reasons (pending_consents,
// migration 063).
const pendingTermsDuration = 15 * time.Minute

// capacityOrganizer is the capacity every acceptance on the Staff platform is
// recorded in today — org_admin, event_owner, event_staff and Platform
// Operators alike accept "en calidad de organizador" (§3, ADR 0066). The
// vocabulary is held open by the CHECK on migration 107, not by anything here.
const capacityOrganizer = "organizer"

// TermsVersionSource is what the sign-in gate needs to know about the Terms:
// which edition is current. It is the narrowest possible slice of the consent
// module — one read, no way to record anything — declared HERE, on the side
// that calls it, the house pattern for a cross-module dependency (ConsentGate,
// OrganizationResolver). identity may depend on consent; consent must never
// depend on identity, and an interface this thin keeps the gate from ever
// asking the consent module to do the gating.
//
// consentrepo.Repository satisfies it as-is.
type TermsVersionSource interface {
	CurrentTermsVersion(ctx context.Context) (consentrepo.TermsVersion, error)
}

// SignInOutcome is what a completed Proof of Email Ownership produces on the
// Staff platform, and it has exactly two shapes: a Staff Session, or a terms
// step to be finished first (#538).
//
// One type with a nil in it rather than two return paths, for the customer
// door's reason (customers/service.SignInOutcome): every caller must handle
// both, and a shape that lets one be forgotten is a shape that lets a session
// be minted past the gate. Session and SessionID are set together, or
// TermsRequired is set and both are empty; there is no outcome with neither
// and none with both.
type SignInOutcome struct {
	Session   *SessionView `json:"session"`
	SessionID string       `json:"session_id"`
	// TermsRequired is non-nil when no session was minted because this email has
	// no Terms Acceptance of the current Terms Version.
	TermsRequired *TermsRequiredView `json:"terms_required"`
}

// TermsRequiredView is the terms-required outcome on the wire: the token that
// can finish this sign-in, when it stops being worth anything, and the one box
// to show.
//
// IT IS ONLY EVER RETURNED AFTER PROOF OF EMAIL OWNERSHIP — the oracle
// discipline the customer consent gate set (ADR 0035): whether an address owes
// a Terms Acceptance is a fact about a known person, and it must not be
// visible until a passcode or a Google token has established who is asking.
//
// The Terms Version's database id is deliberately absent, matching the Policy
// Version rule: the label is how a human names the edition, and which edition
// an acceptance records is the platform's finding at the moment of the write,
// never the client's assertion.
type TermsRequiredView struct {
	// PendingTermsToken is the single-use, short-lived credential that exchanges
	// an acceptance for the session this sign-in did not mint. A server-side row
	// (migration 107), so spending it destroys it.
	PendingTermsToken string `json:"pending_terms_token"`
	// ExpiresAt is when that token stops working, RFC 3339.
	ExpiresAt string `json:"expires_at"`
	// Version is the edition label being accepted ("1").
	Version string `json:"version"`
	// AcceptanceLabel is the mandatory, un-premarked checkbox's label, markdown,
	// verbatim from the embedded artifact (§3). The UI renders it beside a link
	// to the public Storefront terms page and may not reword or pre-tick it —
	// the Staff app hosts no copy of the document.
	AcceptanceLabel string `json:"acceptance_label"`
}

// TermsAcceptanceSubmission is a terms step being finished: the token, the one
// answer, and the circumstances to be recorded as evidence.
type TermsAcceptanceSubmission struct {
	// Token is the pending-terms token from the terms-required outcome.
	Token string
	// TermsAcceptance is the required box. False is refused by the API and not
	// merely by a disabled button.
	TermsAcceptance bool
	// Evidence is the technical proof of this act, derived by the handler from
	// the request itself and never from the body (consent.EvidenceFromRequest —
	// one spelling for every capture surface in every module).
	Evidence consent.Evidence
}

// gateOnTerms decides what a proven staff email earns: a Staff Session, or a
// terms step (#538, ADR 0066).
//
// The predicate is one thing only — no Staff Terms Acceptance of the CURRENT
// Terms Version for this email — and it has no special cases in it. An Org
// Admin of three Organizations, an Event Staff member scanning at a door, and
// a Platform Operator who is a Member of nothing are all gated by the same
// test for the same reason: every human on the Staff platform accepts, once
// per person per edition, and nobody is grandfathered. Inserting a later
// terms_versions row makes every stored reference stop matching, which
// re-gates everyone with no code and no data migration.
//
// A read that fails FAILS THE SIGN-IN rather than waving it through: unlike
// the Staff Locale beside it, this gate is the thing the sign-in owes, and a
// database hiccup must not be the way past a contractual gate. Migration 105
// seeds edition 1, so "no current version" is unreachable in any migrated
// environment and is reported as the plain error it is.
func (s *Service) gateOnTerms(ctx context.Context, email string, now time.Time) (*TermsRequiredView, error) {
	version, err := s.termsVersions.CurrentTermsVersion(ctx)
	if err != nil {
		return nil, err
	}

	accepted, err := s.repo.HasTermsAcceptance(ctx, email, version.ID)
	if err != nil {
		return nil, err
	}
	if accepted {
		return nil, nil
	}

	token, err := newSessionToken()
	if err != nil {
		return nil, err
	}
	pending := repository.PendingStaffTerms{
		ID:        token,
		Email:     email,
		ExpiresAt: now.Add(pendingTermsDuration),
		CreatedAt: now,
	}
	if err := s.repo.CreatePendingStaffTerms(ctx, pending); err != nil {
		return nil, err
	}

	return &TermsRequiredView{
		PendingTermsToken: token,
		ExpiresAt:         pending.ExpiresAt.UTC().Format(time.RFC3339),
		Version:           version.Label,
		AcceptanceLabel:   terms.Current().AcceptanceLabel,
	}, nil
}

// AcceptTerms exchanges a pending-terms token and a ticked box for the Staff
// Session the sign-in withheld, appending exactly one Staff Terms Acceptance
// row on the way (#538).
//
// The token is spent whatever happens next — consumed before the answer is
// judged, so a submission refused for an unticked box cannot be retried
// against the same proof; the person signs in again, which costs a passcode.
// Abandoning instead leaves no session and no row: refusing the Terms costs a
// person nothing they already had, because what they had was being signed out.
//
// NOBODY IS EVER SIGNED IN WITHOUT THE EVIDENCE HAVING COMMITTED. The session
// is minted before the acceptance row is written, because the row names the
// session this act produces — but it is not RETURNED unless the insert
// succeeds, so a failure to record the evidence is a failed sign-in and the
// orphaned session row is never handed to anybody (the customer consent step's
// trade, taken identically).
//
// The edition recorded is the one CURRENT AT THE WRITE, re-read here rather
// than pinned to the token: which edition somebody accepted is the platform's
// finding at the moment of the act, and a token minted just before an edition
// change must not record an acceptance of text nobody was shown by the time
// they ticked.
func (s *Service) AcceptTerms(ctx context.Context, submission TermsAcceptanceSubmission) (*SignInOutcome, error) {
	pending, err := s.repo.ConsumePendingStaffTerms(ctx, submission.Token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// Unknown, already spent, or expired: one refusal, deliberately
	// indistinguishable, exactly as the customer side's token gets.
	if pending == nil || now.After(pending.ExpiresAt) {
		return nil, identity.ErrPendingTermsInvalid()
	}

	// The required box, refused by the API. The Staff app disables its button
	// too, and that is a courtesy; this is the guarantee.
	if !submission.TermsAcceptance {
		return nil, identity.ErrTermsAcceptanceRequired()
	}

	version, err := s.termsVersions.CurrentTermsVersion(ctx)
	if err != nil {
		return nil, err
	}

	session, view, err := s.mintStaffSession(ctx, pending.Email, now)
	if err != nil {
		return nil, err
	}

	if err := s.repo.InsertTermsAcceptance(ctx, repository.StaffTermsAcceptance{
		Email:          pending.Email,
		TermsVersionID: version.ID,
		Capacity:       capacityOrganizer,
		AcceptedAt:     now,
		IP:             submission.Evidence.IP,
		UserAgent:      submission.Evidence.UserAgent,
		SessionID:      session.ID,
		OriginURL:      submission.Evidence.OriginURL,
	}); err != nil {
		return nil, err
	}

	return &SignInOutcome{Session: view, SessionID: session.ID}, nil
}
