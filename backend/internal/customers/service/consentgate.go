package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// pendingConsentDuration is how long a held sign-in is worth finishing.
//
// Fifteen minutes: long enough to read a Short Notice, follow the link to the
// full Privacy Policy, come back and tick a box; short enough that this is not
// a second credential lying about. It is Proof of Email Ownership in
// suspension, and a passcode's own window is ten minutes for the same reason.
const pendingConsentDuration = 15 * time.Minute

// ConsentGate is the consent module's half of a sign-in: what a Customer still
// has to be asked, and the recording of what they answered.
//
// It is declared HERE, on the side that calls it, and implemented by the
// consent service — the house pattern for a cross-module dependency
// (ReversalRequestResolver, OrganizationResolver). What it buys here is the
// dependency direction the whole feature rests on: `customers` may depend on
// `consent`, and `consent` must never depend on `customers`. The module that
// decides whether a Customer Session may be minted cannot be the module that
// owns Customer Sessions, or the two point at each other and the gate ends up
// being defined by the thing it gates.
//
// The interface stays as narrow as the acts on this side need. It cannot read
// the Consent Record log, cannot write state without evidence — Capture and
// ConfirmPending each do both or neither — and cannot name a Policy Version,
// because which edition somebody accepted is the consent module's finding and
// not this one's assertion.
//
// The second pair arrived with the confirmation link (#255). That link is this
// module's because it holds the signing key and the Customer is its subject,
// while what a press MEANS stays on the far side. Note what is deliberately NOT
// here: no way to ask for a state to be SET. The caller says "this link was
// pressed, here is the scope it was minted for" and learns what that did; it
// cannot ask for `granted`, which is what stops the staleness rule from being
// restated over here.
type ConsentGate interface {
	// Outstanding reports which boxes this Customer must still be shown.
	Outstanding(ctx context.Context, customerID string) (consent.Outstanding, error)
	// Capture records one act — the immutable Consent Record, the state it makes
	// true, and the Follow Digest flag in lockstep — in one transaction.
	Capture(ctx context.Context, capture consent.Capture) (consent.Receipt, error)
	// PendingConfirmations reports which optional consents sit in Pending
	// Confirmation, which is what decides whether a Sale Confirmation carries a
	// confirmation line at all.
	PendingConfirmations(ctx context.Context, customerID string) (consent.Pending, error)
	// ConfirmPending resolves whatever of a pressed link's scope is still
	// pending, and reports what that press actually did.
	ConfirmPending(ctx context.Context, confirmation consent.Confirmation) (consent.ConfirmationResult, error)
	// ConfirmationSent records that the Customer was told about the Consent
	// Withdrawal one Consent Record performed (#267).
	//
	// The third arrival on this interface, and the narrowest: one record, one
	// timestamp. It names a record this module was handed by Capture moments
	// earlier and can say nothing else about it — no way to unsay it, and no way
	// to reach any other row. The evidence log stays the consent module's, and
	// this side may only annotate the act it just performed.
	ConfirmationSent(ctx context.Context, recordID string, at time.Time) error
	// PrivacyState reports what a Customer has authorized — the Policy Version
	// they accepted and when, and each optional consent's current state.
	//
	// THE FOURTH ARRIVAL, AND THE ONLY ONE THAT IS PURELY A READ. It exists
	// beside Outstanding rather than inside it because the two ask opposite
	// questions: Outstanding asks which boxes a person must be SHOWN, which is
	// what a capture surface renders from, and this asks what is TRUE about
	// them, which is what a settings surface reports. A caller cannot reach a
	// write through it, so the page it serves cannot record anything by being
	// looked at.
	//
	// IT SERVES BOTH SURFACES THAT REPORT RATHER THAN ASK — the Customer's own
	// Privacy page (#268) and the Operator's view of what a withdrawal would
	// change before it changes it (#271). The two arrived independently and were
	// nearly the same read; they are one method because the question is one
	// question, and a second spelling of it would be a second thing to keep true.
	// What differs between them is who may call and what is drawn, neither of
	// which is this interface's business.
	PrivacyState(ctx context.Context, customerID string) (consent.PrivacyState, error)
}

// SignInOutcome is what a completed Proof of Email Ownership produces, and it
// has exactly two shapes: a Customer Session, or a consent step to be finished
// first.
//
// One type with a nil in it rather than two return paths, because every caller
// must handle both and a shape that lets one be forgotten is a shape that lets
// a session be minted past the gate. Session and SessionID are set together, or
// ConsentRequired is set and both are empty; there is no outcome with neither
// and none with both.
type SignInOutcome struct {
	Session   *CustomerSessionView `json:"session"`
	SessionID string               `json:"session_id"`
	// ConsentRequired is non-nil when no session was minted because the Customer
	// has no Policy Acceptance of the current Policy Version.
	ConsentRequired *ConsentRequiredView `json:"consent_required"`
}

// ConsentRequiredView is the consent-required outcome on the wire: the token
// that can finish this sign-in, when it stops being worth anything, and which
// boxes to show.
//
// IT IS ONLY EVER RETURNED AFTER PROOF OF EMAIL OWNERSHIP. That is the oracle
// discipline this feature inherits (ADR 0035): the passcode request endpoint
// answers identically for an address the platform knows and one it has never
// seen, and it must stay that way — so "this person has consent outstanding",
// which is a fact about a known Customer, cannot be visible until a passcode or
// a Google token has already established who is asking.
type ConsentRequiredView struct {
	// PendingConsentToken is the single-use, short-lived credential that
	// exchanges answers for the session this sign-in did not mint. It is a
	// server-side row (migration 063), so spending it destroys it.
	PendingConsentToken string `json:"pending_consent_token"`
	// ExpiresAt is when that token stops working, RFC 3339. Published so a client
	// can say "start again" rather than discovering it by being refused.
	ExpiresAt string `json:"expires_at"`
	// Boxes is what to show.
	Boxes ConsentBoxesView `json:"boxes"`
}

// ConsentBoxesView is which checkboxes a capture surface must render.
//
// PolicyAcceptance is always true here: this outcome exists precisely because
// the current Policy Version is unaccepted. The optional two are true only
// where the Customer's state is unanswered — with Pending Confirmation counting
// as unanswered, because somebody else's tick is not the owner's answer — so a
// Customer re-prompted by a version bump is shown the required box ALONE and
// their standing optional answers are never churned.
//
// A box shown is a box shown UNTICKED, always. Nothing in this payload says
// what to pre-fill, and nothing ever should: consent is affirmative, so stored
// state decides whether to ASK and never what to show as already agreed.
type ConsentBoxesView struct {
	PolicyAcceptance  bool `json:"policy_acceptance"`
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
}

// ConsentSubmission is a consent step being finished: the token, what was
// ticked, and the circumstances to be recorded as evidence.
type ConsentSubmission struct {
	// Token is the pending-consent token from the consent-required outcome.
	Token string
	// PolicyAcceptance is the required box. False is refused by the API and not
	// merely by a disabled button — see consent.ErrPolicyAcceptanceRequired.
	PolicyAcceptance bool
	// MarketingConsent and NetworkingConsent are the optional boxes as they were
	// submitted. False means SHOWN AND LEFT UNTICKED, which is an explicit No,
	// and is recorded as `denied` rather than as silence (ADR 0034).
	//
	// They are read only for boxes this Customer was actually owed, which the
	// service recomputes rather than trusting the submission about. A client
	// that sends an answer for a box the person was not shown changes nothing:
	// standing optional answers are the owner's and are not re-writable by
	// whoever is holding a sign-in token.
	MarketingConsent  bool
	NetworkingConsent bool
	// Evidence is the technical proof of this act, derived by the handler from the
	// request itself and never from the body.
	Evidence consent.Evidence
}

// SubmitConsent exchanges a pending-consent token and a set of answers for the
// Customer Session the sign-in withheld.
//
// NOBODY IS EVER SIGNED IN WITHOUT THE EVIDENCE HAVING COMMITTED. The session
// row is created before the capture, because the Consent Record has to name the
// session this act produced — but it is not RETURNED unless the capture
// succeeds, so a failure to record the evidence is a failed sign-in and the
// orphaned row is never handed to anybody. The person is signed in only on the
// far side of a committed Consent Record, which is the property that matters;
// the leftover row expiring unused is the cheap side of the trade.
//
// The token is spent whatever happens next. It is consumed before the answers
// are judged, so a submission refused for missing Policy Acceptance cannot be
// retried against the same proof: the person starts the sign-in again, which
// costs them a passcode and costs an attacker holding a stolen token every
// chance of a second try.
//
// Abandoning instead — closing the tab, refusing the policy — leaves the
// Customer record verified, the token unspent until it expires, and NO SESSION.
// Refusing consent costs a person nothing they already had, because what they
// had was being signed out.
func (s *Service) SubmitConsent(ctx context.Context, submission ConsentSubmission) (*SignInOutcome, error) {
	pending, err := s.repo.ConsumePendingConsent(ctx, submission.Token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// Unknown, already spent, or expired: one refusal, deliberately
	// indistinguishable. An expired row was consumed by the read above and is
	// gone, so a token cannot be probed twice for a different answer.
	if pending == nil || now.After(pending.ExpiresAt) {
		return nil, customers.ErrPendingConsentInvalid()
	}

	// The required box, refused by the API. The Storefront disables its submit
	// button too, and that is a courtesy; this is the guarantee.
	if !submission.PolicyAcceptance {
		return nil, consent.ErrPolicyAcceptanceRequired()
	}

	customer, err := s.repo.GetCustomerByID(ctx, pending.CustomerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		// The Customer went away between proving their address and answering. The
		// token names nothing, which is what an invalid token means.
		return nil, customers.ErrPendingConsentInvalid()
	}

	// Recomputed, not trusted. The submission arrives from a browser that was
	// told which boxes to show, and this is the server asking the same question
	// again at the moment of the write: an answer for a box the Customer had
	// already answered is dropped rather than applied, so a standing Marketing
	// or Networking Consent cannot be flipped by a crafted body.
	outstanding, err := s.consent.Outstanding(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	accepted := true
	answers := consent.Answers{PolicyAcceptance: &accepted}
	if outstanding.MarketingConsent {
		marketing := submission.MarketingConsent
		answers.MarketingConsent = &marketing
	}
	if outstanding.NetworkingConsent {
		networking := submission.NetworkingConsent
		answers.NetworkingConsent = &networking
	}

	// The session is minted before the capture so that the Consent Record can
	// carry its identifier: the evidence's `session_id` is what ties this act to
	// what the person did next, and a record pointing at a session that does not
	// exist yet would tie it to nothing.
	//
	// It is created but NOT RETURNED until the capture commits. If recording the
	// evidence fails, this call fails and the token is already spent, so the
	// session token is never handed to anybody — the row is an orphan that
	// expires on its own schedule, which is the cheap side of this trade. The
	// expensive side would have been a signed-in Customer with no evidence.
	//
	// Nothing is outstanding on the far side of this submission, which is why the
	// minted view says so: every box that WAS outstanding has just been answered
	// above — the required one by the gate, the optional ones by the answers this
	// submission carried — and a box that was not outstanding was already
	// answered. So the session this call hands back is honestly one that owes
	// nothing, without a second read of a Customer this request is mid-write on.
	session, view, err := s.mintSession(ctx, customer, now, consent.Outstanding{})
	if err != nil {
		return nil, err
	}

	if _, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID: customer.ID,
		// The address as this sign-in proved it, from the pending row rather than
		// from the Customer — it is what was asserted at capture.
		Email:   pending.Email,
		Channel: consent.ChannelSignIn,
		// Always true on this surface, and passed explicitly rather than inferred
		// from the channel: a passcode or a Google token has already verified this
		// address, which is what makes a tick here `granted` rather than a Pending
		// Confirmation.
		EmailProven: true,
		Answers:     answers,
		Evidence: consent.Evidence{
			IP:        submission.Evidence.IP,
			UserAgent: submission.Evidence.UserAgent,
			SessionID: session.ID,
			OriginURL: submission.Evidence.OriginURL,
		},
	}); err != nil {
		return nil, err
	}

	return &SignInOutcome{Session: view, SessionID: session.ID}, nil
}

// gateOnConsent decides what a proven email earns: a Customer Session, or a
// consent step.
//
// The predicate is one thing and one thing only — no Policy Acceptance of the
// CURRENT Policy Version — and it has no special cases in it. A Customer
// created by a box-office sale, one from a Sale Import, one who has been
// signing in since before consent existed, and one who accepted an edition that
// has since been superseded are all gated by the same test for the same reason.
// Nobody is grandfathered: an acceptance nobody recorded cannot be produced
// later, and the platform does not get to assume one.
//
// An unanswered OPTIONAL consent is not a reason to withhold a session. It is a
// reason to show the box when the person is stopped for something else — which
// is why the boxes below are computed from the same Outstanding — and the
// surfaces that ask about optional consents in their own right are the checkout
// and the Customer Area, not this door.
//
// The outstanding set is the CALLER's, read once at the door and passed in, so
// that the boxes named below and the session minted just after cannot come from
// two different readings of the same Customer.
func (s *Service) gateOnConsent(ctx context.Context, customer *repository.Customer, outstanding consent.Outstanding, now time.Time) (*ConsentRequiredView, error) {
	if !outstanding.PolicyAcceptance {
		return nil, nil
	}

	token, expiresAt, err := s.mintPendingConsent(ctx, customer.ID, customer.Email, now)
	if err != nil {
		return nil, err
	}

	return &ConsentRequiredView{
		PendingConsentToken: token,
		ExpiresAt:           expiresAt.UTC().Format(time.RFC3339),
		Boxes: ConsentBoxesView{
			// Always: this outcome exists because it is outstanding.
			PolicyAcceptance:  true,
			MarketingConsent:  outstanding.MarketingConsent,
			NetworkingConsent: outstanding.NetworkingConsent,
		},
	}, nil
}

// mintPendingConsent holds one Proof of Email Ownership in suspension: a
// short-lived, single-use row whose only purchase is a consent submission.
//
// It is the minting half of gateOnConsent, extracted so the Consent Withdrawal
// surface can mint the SAME credential rather than invent a second one (#270,
// ADR 0039). What it cannot do is decide WHETHER to mint: the sign-in door mints
// one only when a Policy Acceptance is outstanding, and the withdrawal door
// mints one unconditionally because it has no session to withhold. Folding that
// decision in here would put two callers' policies in one function and make the
// unconditional one look like a bypass of the other.
//
// The token is a session token's worth of entropy, from the same generator, and
// it is the row's primary key — so spending it is a delete and there is nothing
// left to replay (migration 063).
func (s *Service) mintPendingConsent(ctx context.Context, customerID, email string, now time.Time) (string, time.Time, error) {
	token, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, err
	}
	pending := repository.PendingConsent{
		ID:         token,
		CustomerID: customerID,
		Email:      email,
		ExpiresAt:  now.Add(pendingConsentDuration),
		CreatedAt:  now,
	}
	if err := s.repo.CreatePendingConsent(ctx, pending); err != nil {
		return "", time.Time{}, err
	}
	return token, pending.ExpiresAt, nil
}
