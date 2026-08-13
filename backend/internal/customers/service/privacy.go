package service

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
)

// The Customer's Privacy page (#268, parent #265): what they have authorized,
// and the controls that move each optional consent in either direction.
//
// TWO ACTS AND A LINE BETWEEN THEM. Privacy reads and writes nothing;
// SetOptionalConsent writes exactly one Consent Record. Opening the page must
// leave the platform as it was — a settings surface that recorded a refusal
// because somebody read it would convert "never asked" into "denied" for every
// person who looked and did nothing — so the read below builds no Capture and
// has no path to one.
//
// IT IS A SETTINGS SURFACE AND NOT A CAPTURE SURFACE, which is what makes
// showing current state legitimate here. The never-pre-tick rule governs the
// moments where the platform ASKS — sign-in, checkout — and it is untouched:
// this page reports what is true, and it must never be the surface that first
// asks a Customer a question. The Boxes a capture surface renders still come
// from Outstanding and still come up unticked.
//
// BOTH DIRECTIONS, AND ONLY THAT MAKES THE PAGE HONEST. Moving a control off is
// a Consent Withdrawal; moving it on is an affirmative grant behind a proven
// session, exactly what the Digest toggle already is (ADR 0034). A one-way page
// would trap a Customer who withdrew Networking Consent by mistake, because
// every surface that can grant it shows the box only while the state is
// unanswered — after a withdrawal they would never be shown it again and would
// have no route back at all.
//
// ONE CONTROL PER ACT, structurally. SetOptionalConsent names ONE purpose and
// answers ONE box; the other is nil — NOT SHOWN — so moving Marketing here
// leaves a standing Networking Consent exactly as it was. Withdraw All is a
// different act with its own disclosure and its own single row (#269), and it
// is deliberately not reachable from this method: a surface that could answer
// both at once would let the page perform it without ever telling anybody what
// it means. WithdrawAll below is that act, and it is a THIRD method rather than
// a parameter on this one for exactly that reason.
//
// NO NEW WRITE PATH. Everything below goes through the consent module's
// Capture, which writes the immutable Consent Record, the state it makes true
// and `digest_enabled` in lockstep, in one transaction (ADR 0038). Withdrawing
// Marketing Consent therefore switches the weekly Follow Digest off with it,
// without a line here knowing that it does — and the `/following` toggle and
// this control are one switch rendered twice rather than two switches, because
// they are the same column written by the same statement.

// ConsentPurpose names WHICH optional consent one control on the Privacy page
// moves.
//
// A closed vocabulary of two, and Policy Acceptance is not in it. Acceptance is
// not withdrawable — it is absent from counsel's form, it gates the platform on
// a basis other than consent, and clearing it would re-gate the person rather
// than free them (ADR 0038) — so the type that names what this surface can move
// simply has no word for it.
type ConsentPurpose string

const (
	// PurposeMarketing is Marketing Consent, which is also the weekly Follow
	// Digest: one switch, two columns, never written apart (ADR 0034).
	PurposeMarketing ConsentPurpose = "marketing"
	// PurposeNetworking is Networking Consent: whether this Customer's profile is
	// visible to other attendees of their Events.
	PurposeNetworking ConsentPurpose = "networking"
)

// Valid reports whether the purpose is one this surface recognises.
func (p ConsentPurpose) Valid() bool {
	return p == PurposeMarketing || p == PurposeNetworking
}

// consentStateOnTheWire renders one optional consent's state for a client.
//
// THE FOUR STATES STAY FOUR. `granted`, `denied` and `pending_confirmation` are
// the stored vocabulary verbatim; the empty string — never answered — becomes
// the word "unanswered", which exists ON THE WIRE AND NOWHERE ELSE. It is not a
// stored state and must never become one: the database holds NULL, because
// unanswered is the absence of an answer rather than a fourth kind of answer,
// and a constant in the domain would invite it into the CHECK constraint and
// into rows (see consent.State).
//
// A client is given the word rather than a null because the four are read by a
// page that must draw each of them differently, and "the field was missing" is
// the one thing a page reliably fails to notice. A Pending Confirmation in
// particular must be shown as the unresolved thing it is and never as an
// answer, which a client can only do if it can see it.
func consentStateOnTheWire(state consent.State) string {
	if state == "" {
		return "unanswered"
	}
	return string(state)
}

// OptionalConsentsView is the two optional consents as they stand, and is the
// one spelling of that pair: the Privacy read embeds it and every write answers
// with it, so a page and the act it just performed cannot describe the same
// two facts in two shapes.
type OptionalConsentsView struct {
	// MarketingConsent is "granted", "denied", "pending_confirmation" or
	// "unanswered".
	MarketingConsent string `json:"marketing_consent"`
	// NetworkingConsent is the same four.
	NetworkingConsent string `json:"networking_consent"`
}

// PrivacyView is the Privacy page's read: the accepted Policy Version and the
// state of each optional consent.
//
// The Policy Version pair is READ-ONLY on this surface and has no control
// beside it, because acceptance is not withdrawable. It is here so a Customer
// can see what they agreed to and when, which is the first of the parent
// spec's user stories.
type PrivacyView struct {
	// PolicyVersion is the label of the edition THIS CUSTOMER ACCEPTED, which is
	// not necessarily the one in effect: a Customer who accepted a superseded
	// edition is told what they actually agreed to, and a page naming the current
	// one would claim they had seen a text nobody ever showed them.
	//
	// Null where no acceptance is recorded, which a session minted before consent
	// capture existed can still reach.
	PolicyVersion *string `json:"policy_version"`
	// PolicyAcceptedAt is when that acceptance was recorded, RFC 3339, null
	// alongside a null version.
	PolicyAcceptedAt *time.Time `json:"policy_accepted_at"`
	// Consents is the pair the controls move.
	Consents OptionalConsentsView `json:"consents"`
}

// Privacy reports what the signed-in Customer has authorized. IT WRITES
// NOTHING.
//
// Behind a FULL Customer Session, the same line the profile writes and the
// Follows listing draw: a Confirmation Link session is a forwarded receipt, and
// possession of a forwarded email is not authority to read — or to change —
// somebody's standing privacy settings. This page names every consent a person
// has given, so it discloses more about them than the Area's other reads, not
// less.
func (s *Service) Privacy(ctx context.Context, sessionToken string) (*PrivacyView, error) {
	customer, err := s.fullSessionCustomer(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	state, err := s.consent.PrivacyState(ctx, customer.ID)
	if err != nil {
		return nil, err
	}

	view := &PrivacyView{
		Consents: OptionalConsentsView{
			MarketingConsent:  consentStateOnTheWire(state.MarketingConsent),
			NetworkingConsent: consentStateOnTheWire(state.NetworkingConsent),
		},
	}
	if state.PolicyVersionLabel != "" {
		label := state.PolicyVersionLabel
		view.PolicyVersion = &label
	}
	if !state.PolicyAcceptedAt.IsZero() {
		acceptedAt := state.PolicyAcceptedAt.UTC()
		view.PolicyAcceptedAt = &acceptedAt
	}
	return view, nil
}

// SetOptionalConsent moves ONE optional consent, in whichever direction the
// Customer asked for, and answers with both as they now stand.
//
// A STATE AND NOT A FLIP, exactly as the Digest toggle takes one: "withdraw
// this" arriving twice means what it meant the first time, where a flip
// arriving twice would grant back what somebody just took away.
//
// EmailProven is true and is the whole reason this surface can grant at all.
// The session was established by Proof of Email Ownership, so an answer here is
// the owner's own: it supersedes anything a guest left behind, it is the only
// way a state can become `granted`, and it is how a Customer settles a Pending
// Confirmation somebody else's tick left standing against their address —
// either way, from this page, by answering for themselves.
//
// WITHDRAWING IS CONFIRMED BY EMAIL AND GRANTING IS NOT, and that is not a
// special case written here. confirmWithdrawal is #267's mechanism, reused
// unchanged and asked nothing new: it sends only when the receipt says the act
// took something away, computed inside the transaction that observed the state
// it replaced. A grant took nothing away, and neither did switching off a
// switch that was already off — so both are silent, and this method does not
// have to know which it just performed.
//
// The whole act's evidence is the ordinary technical proof, with the session
// this Customer is signed in under, so a withdrawal made here ties to a session
// and a device exactly as a sign-in's own record does.
func (s *Service) SetOptionalConsent(ctx context.Context, sessionToken string, purpose ConsentPurpose, granted bool, evidence consent.Evidence) (*OptionalConsentsView, error) {
	// An unknown purpose is refused before a session is even resolved: it is a
	// bug in a caller rather than anything about this Customer, and the handler
	// has already turned every purpose a client can name into a validation error.
	if !purpose.Valid() {
		return nil, fmt.Errorf("set optional consent: unknown purpose %q", purpose)
	}

	session, customer, err := s.fullSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	// ONE BOX, AND ONLY ONE. The other purpose and Policy Acceptance are nil —
	// NOT SHOWN on this act — so nothing standing is churned and the evidence
	// says truthfully which single question was answered. Reading those nils as
	// refusals would turn moving one control into a Withdraw All.
	answer := granted
	answers := consent.Answers{}
	switch purpose {
	case PurposeMarketing:
		answers.MarketingConsent = &answer
	case PurposeNetworking:
		answers.NetworkingConsent = &answer
	}

	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID:  customer.ID,
		Email:       customer.Email,
		Channel:     consent.ChannelAccountSettings,
		EmailProven: true,
		Answers:     answers,
		Evidence: consent.Evidence{
			IP:        evidence.IP,
			UserAgent: evidence.UserAgent,
			// The session the act was made under, so this row and the sign-in's own
			// tie together in the log.
			SessionID: session.ID,
			OriginURL: evidence.OriginURL,
		},
	})
	if err != nil {
		return nil, err
	}

	// #267's mechanism, reused rather than reimplemented, and asked nothing this
	// surface knows: it reads the receipt.
	s.confirmWithdrawal(ctx, customer, receipt)

	// The states the CAPTURE made true, read back by the statement that wrote
	// them under the Customer row's lock — never the answer this request carried.
	// The two differ wherever the platform's rules differ from what was asked,
	// and a surface reporting its own request back would be reporting a hope.
	return &OptionalConsentsView{
		MarketingConsent:  consentStateOnTheWire(receipt.MarketingConsent),
		NetworkingConsent: consentStateOnTheWire(receipt.NetworkingConsent),
	}, nil
}

// WithdrawAllView is what one Withdraw All did.
//
// It reports BOTH the resulting states and what the act TOOK AWAY, which are
// different facts and both worth telling. `denied` is the same value whether
// somebody has just given something up or was declining for the second time, so
// a page that read only the states would tell a Customer whose consents were
// already both denied that a confirmation is on its way to them — and none is,
// because nothing moved.
type WithdrawAllView struct {
	// Consents is the pair as it now stands: the same shape every other act on
	// this surface answers with, so the page cannot describe the same two facts
	// in two ways.
	Consents OptionalConsentsView `json:"consents"`
	// Withdrew is what this act actually took away, decided inside the
	// transaction that observed the prior state (#266) and never recomputed out
	// here. It is the same shape and the same spelling the passcode-only surface
	// publishes (#270), because it is the same question.
	Withdrew ConsentWithdrewView `json:"withdrew"`
}

// WithdrawAll takes back every optional consent at once: ONE act, ONE Consent
// Record, both optional consents denied.
//
// WHY IT IS ITS OWN METHOD AND ITS OWN ROUTE. Two calls to SetOptionalConsent
// would leave the same Customer in the same state, and would be WRONG AS
// EVIDENCE: the log would hold two rows saying somebody moved two controls,
// where what happened was one person asking to be left alone. A compliance
// officer reading the revocation register cannot recover that intent from two
// rows and a shared timestamp, and must not have to guess at it. So the single
// row is the deliverable here, not an optimisation — and it is why the
// per-purpose route deliberately takes one purpose in its path and cannot
// become this by accident (#268).
//
// IT CAN ONLY WITHDRAW, and that is a property of the code rather than a rule
// to remember: the answers are built from deniedAnswer(), which has no variable
// in it to flip, exactly as on the passcode-only surface (#270). Nothing here
// takes a value from a caller, and the request that reaches this method carries
// no body at all.
//
// POLICY ACCEPTANCE IS NOT TOUCHED and is nil — not shown on this act. It is
// not withdrawable: it rests on a basis other than consent, and clearing it
// would re-gate the person rather than free them (ADR 0038). "Everything" here
// means every OPTIONAL consent, which is everything a Customer ever granted.
//
// NO NEW WRITE PATH, again. The single Capture writes the immutable Consent
// Record, both states, and `digest_enabled` in lockstep with Marketing Consent —
// one transaction, one statement (ADR 0034) — so the weekly Follow Digest goes
// off with this act without a line here knowing that it does.
//
// THE DISCLOSURE IS NOT HERE, and there is nothing this method could do about
// it. What Withdraw All means — that marketing and networking stop, that the
// data is retained for the Tickets held and for legal and security obligations,
// that the account, the Tickets and the transactional mail are unaffected, that
// it can all be turned back on, and that IT IS NOT DELETION — is told to the
// Customer by the dialog before they reach this at all. This method must never
// be given a second caller that has not shown it.
func (s *Service) WithdrawAll(ctx context.Context, sessionToken string, evidence consent.Evidence) (*WithdrawAllView, error) {
	session, customer, err := s.fullSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID: customer.ID,
		Email:      customer.Email,
		Channel:    consent.ChannelAccountSettings,
		// The session was established by Proof of Email Ownership, so this is the
		// owner answering for themselves. It is what makes the denial STICK: an
		// unproven No writes no state at all, which here would mean silently
		// ignoring the withdrawal of every consent that had actually been granted.
		EmailProven: true,
		// BOTH BOXES, ANSWERED NO, IN ONE ACT — the whole of the ticket, and the
		// one place in the platform where both optional answers are non-nil and
		// false together. A Pending Confirmation somebody else left standing is
		// settled as No by the same answers, which is the owner resolving it.
		Answers: consent.Answers{
			MarketingConsent:  deniedAnswer(),
			NetworkingConsent: deniedAnswer(),
		},
		Evidence: consent.Evidence{
			IP:        evidence.IP,
			UserAgent: evidence.UserAgent,
			// The session the act was made under, so this row and the sign-in's own
			// tie together in the log.
			SessionID: session.ID,
			OriginURL: evidence.OriginURL,
		},
	})
	if err != nil {
		return nil, err
	}

	// #267's mechanism, reused and asked nothing new. It sends only when the
	// receipt says something moved, so a Customer who had already denied both is
	// not written to — and where only one consent was actually standing, the mail
	// names that one rather than claiming both were taken away.
	s.confirmWithdrawal(ctx, customer, receipt)

	return &WithdrawAllView{
		Consents: OptionalConsentsView{
			MarketingConsent:  consentStateOnTheWire(receipt.MarketingConsent),
			NetworkingConsent: consentStateOnTheWire(receipt.NetworkingConsent),
		},
		Withdrew: ConsentWithdrewView{
			MarketingConsent:  receipt.Withdrawn.MarketingConsent,
			NetworkingConsent: receipt.Withdrawn.NetworkingConsent,
		},
	}, nil
}
