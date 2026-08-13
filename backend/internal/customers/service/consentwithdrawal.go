package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Withdrawing a consent on Proof of Email Ownership alone (#270, parent #265,
// ADR 0039): a passcode, a withdrawal, and no Customer Session at either end.
//
// WHY IT EXISTS. Signing in withholds the session until the consent step, and
// that step demands acceptance of the CURRENT Policy Version. Publishing a new
// edition re-gates the whole customer base, so a person who wants to take a
// consent back is told: to withdraw your consent, first accept this. Marketing
// has an escape hatch in the unsubscribe link at the foot of a Digest, which
// authenticates nobody; Networking Consent has none at all, and until this file
// existed nothing in the platform could move it back to `denied`.
//
// THE CREDENTIAL IS THE ONE THAT ALREADY EXISTS. A `pending_consents` row is
// Proof of Email Ownership held in suspension — minutes long, single use, and
// spendable on exactly one thing, a consent submission (migration 063). This
// surface mints one and spends one; it invents no second credential, signs no
// long-lived link, and adds no column to that table. The token is spent whatever
// the outcome, exactly as the sign-in door spends it.
//
// IT CAN ONLY EVER WITHDRAW, and that is a property of the types rather than of
// a rule anybody has to remember. WithdrawConsent below names which consents to
// take away and cannot name a value: the only answer it is able to build is
// deniedAnswer(), which has no variable in it to flip. So a stolen passcode on
// this route buys the ability to switch somebody's marketing off — which its
// owner can switch back on from their own Customer Area — and nothing else. It
// cannot grant a consent, accept a Policy Version, or open a session.
//
// NO SESSION IS MINTED AT EITHER STEP. Proving an address in order to withdraw
// must not leave somebody signed in on a shared machine (parent story 23), so
// the passcode door here answers with the pending-consent token alone and never
// with a Customer Session — which is the whole difference between it and
// VerifyOTP, and the reason it is a route of its own rather than a flag on that
// one. A flag would have made the sign-in door capable of withholding a session
// on a client's say-so; this way the door that mints sessions is untouched.
//
// WHAT IS DELIBERATELY NOT HERE. No consent state is published to this surface.
// A person who proves an address is offered the two optional consents to take
// away and is told what is true afterwards, but the surface never renders their
// standing state as a starting position: it is not a settings page (that is
// `/privacy`, behind a full session), and a passcode-only route that reported
// "you have Networking Consent granted" would be answering a question about a
// Customer to whoever is holding one passcode.
//
// The word is WITHDRAWAL. `revoke` stays this platform's verb for destroying a
// credential — and this file destroys one, the pending-consent row, which is
// exactly why the two words must not be swapped here of all places (ADR 0038).

// ConsentWithdrawalProofView is what proving an address on the withdrawal
// surface earns: the pending-consent token, and when it stops being worth
// anything.
//
// IT IS THE CONSENT-REQUIRED OUTCOME MINUS THE BOXES, and the omission is the
// point. `boxes` on a sign-in says which questions the Customer has not yet
// answered, so that a capture surface can ask them; this surface asks nothing
// and captures nothing new. Publishing the same field here would tell the holder
// of a passcode which consents its owner has answered, and would invite a client
// to read it as "these are the ones you may withdraw" — which is the opposite of
// what it means.
type ConsentWithdrawalProofView struct {
	// PendingConsentToken is the single-use, short-lived proof of email
	// ownership. It is the same credential the sign-in door mints and is spent at
	// the same endpoint.
	PendingConsentToken string `json:"pending_consent_token"`
	// ExpiresAt is when it stops working, RFC 3339, published so a surface can
	// say "start again" rather than discovering it by being refused.
	ExpiresAt string `json:"expires_at"`
}

// ConsentWithdrawal is one Consent Withdrawal made on Proof of Email Ownership
// alone: which consents to take away, and the circumstances to record as
// evidence.
//
// THE FIELDS SAY WHICH, NEVER WHAT. MarketingConsent true means "withdraw
// Marketing Consent" and false means "this consent was not on the submission at
// all" — there is no value in this struct that could ask for a grant, which is
// what makes "this surface can only withdraw" structural rather than a rule in a
// comment. A future field that carried an answer instead of a selection would
// quietly undo that, and there is no reason to add one: granting a consent takes
// a full Customer Session, and the surfaces that do it already exist.
type ConsentWithdrawal struct {
	// Token is the pending-consent token minted by the withdrawal surface's
	// passcode door — or, indistinguishably, by a held sign-in. Which door minted
	// it is not a security property and is not recorded: both are Proof of Email
	// Ownership, and the act this one buys is strictly weaker than the session the
	// other one does.
	Token string
	// MarketingConsent and NetworkingConsent select the consents this act takes
	// away. At least one must be selected; a submission selecting neither is not a
	// withdrawal and is refused as one (see WithdrawConsent).
	MarketingConsent  bool
	NetworkingConsent bool
	// Evidence is the technical proof of this act, derived by the handler from
	// the request itself and never from the body. There is no SessionID in it and
	// there cannot be: this act mints no session, and null there is recorded as
	// "not collected" rather than as a blank.
	Evidence consent.Evidence
}

// ConsentWithdrawalView is what the withdrawal did, as the surface reports it.
//
// It says what is TRUE NOW and what this act TOOK AWAY, which are different
// facts and both worth telling — the same distinction the confirmation link's
// view makes for the opposite act. `denied` is the same value whether somebody
// has just given something up or was declining for the second time, so a page
// that read only the states would congratulate a person on a withdrawal that
// changed nothing.
type ConsentWithdrawalView struct {
	// MarketingConsent and NetworkingConsent are the states AFTER the act:
	// "granted", "denied", "pending_confirmation", or empty for a consent that
	// has never been answered and was not named by this submission.
	MarketingConsent  string `json:"marketing_consent"`
	NetworkingConsent string `json:"networking_consent"`
	// Withdrew is what this act actually took away — a move out of `granted` or
	// `pending_confirmation` and into `denied`, decided inside the transaction
	// that observed the prior state (#266) and never recomputed out here.
	Withdrew ConsentWithdrewView `json:"withdrew"`
}

// ConsentWithdrewView is which optional consents one act took away.
type ConsentWithdrewView struct {
	MarketingConsent  bool `json:"marketing_consent"`
	NetworkingConsent bool `json:"networking_consent"`
}

// ProveEmailForConsentWithdrawal redeems a passcode for the pending-consent
// token that buys a Consent Withdrawal, and for nothing else.
//
// IT MINTS NO CUSTOMER SESSION, EVER — not for a Customer who owes a Policy
// Acceptance, and not for one who has accepted the current edition and would
// have been signed in straight through by VerifyOTP. That unconditional is the
// reason this exists beside the sign-in door instead of inside it: a person
// exercising a right on a shared machine must be able to prove their address
// without being logged in on it afterwards, and the only way to promise that is
// for the code path to have no branch that mints one.
//
// Everything else a proven passcode does still happens, because a passcode is a
// passcode whichever surface redeems it: the Customer record is created or
// reused, `verified_at` is stamped, and the Mail Locale of the page is
// remembered — which matters here more than anywhere, since the confirmation of
// the withdrawal is written in it (ADR 0033).
//
// A passcode issued for any other purpose is not accepted, and a wrong one fails
// exactly as it fails on the sign-in door: this surface is reachable only past a
// correct code, so it discloses nothing about an address that anybody could not
// already learn by trying to sign in.
func (s *Service) ProveEmailForConsentWithdrawal(ctx context.Context, email, code, locale string) (*ConsentWithdrawalProofView, error) {
	email = platform.NormalizeEmail(email)
	now := s.now()

	if err := s.otp.Verify(ctx, otpPurpose, email, code); err != nil {
		return nil, err
	}

	// The language of the page this happened on, remembered as it is on every
	// completed proof of ownership — a preference and not a credential, so an
	// unserved language is dropped rather than made a reason a person cannot
	// exercise a right.
	mailLocale := ""
	if parsed, ok := platform.ParseLocale(locale); ok {
		mailLocale = string(parsed)
	}
	customer, err := s.repo.VerifyCustomer(ctx, email, now, mailLocale)
	if err != nil {
		return nil, err
	}

	token, expiresAt, err := s.mintPendingConsent(ctx, customer.ID, customer.Email, now)
	if err != nil {
		return nil, err
	}
	return &ConsentWithdrawalProofView{
		PendingConsentToken: token,
		ExpiresAt:           expiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// WithdrawConsent spends a pending-consent token on a Consent Withdrawal, and
// hands back no session.
//
// THE POLICY ACCEPTANCE GATE IS NOT CONSULTED HERE, WHICH IS THE WHOLE OF ADR
// 0039. A submission that grants nothing needs no acceptance: acceptance
// evidences that somebody was informed before the platform began doing something
// on their behalf, and an act that only takes things away begins nothing,
// authorizes nothing and opens nothing. Requiring it would not protect the
// Customer — it would only stand between them and a right, and would do it worst
// to the person the right exists for, whom a freshly published edition has just
// re-gated.
//
// The gate itself is untouched. SubmitConsent still refuses a submission without
// Policy Acceptance, unconditionally, in the same line it always did; what is
// conditional is which of the two acts a submission IS, and that is decided by
// the handler from the submission's contents before either is attempted. So
// there is no path by which a grant reaches this method: it cannot express one.
//
// AT LEAST ONE CONSENT MUST BE NAMED. A submission selecting neither would
// record an act that answered nothing and moved nothing — and, worse, would be
// the shape an empty body takes, which is precisely the submission the gate has
// always refused. It is refused here with the gate's own error, so a client that
// sends nothing gets the same answer it has always got.
//
// The token is spent before the answers are judged, exactly as it is on the
// sign-in door: a refused withdrawal cannot be retried against the same proof,
// and the person starts again with a fresh passcode.
//
// The withdrawal is recorded with the same weight as one made while signed in —
// the same single consent-write path, the same immutable Consent Record, the
// same technical proof, the same confirmation mail — so the easier route is not
// the weaker one (parent story 24).
func (s *Service) WithdrawConsent(ctx context.Context, withdrawal ConsentWithdrawal) (*ConsentWithdrawalView, error) {
	pending, err := s.repo.ConsumePendingConsent(ctx, withdrawal.Token)
	if err != nil {
		return nil, err
	}
	now := s.now()
	// Unknown, already spent, or expired: one refusal, deliberately
	// indistinguishable, exactly as on the sign-in door.
	if pending == nil || now.After(pending.ExpiresAt) {
		return nil, customers.ErrPendingConsentInvalid()
	}

	if !withdrawal.MarketingConsent && !withdrawal.NetworkingConsent {
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

	// Only the consents this submission named are answered. The rest are nil —
	// NOT SHOWN — so this act neither accepts a policy nor touches a consent the
	// person did not ask about, and the Consent Record's null says so.
	//
	// NOTHING IS FILTERED THROUGH Outstanding HERE, and that is the one place
	// this path deliberately reads differently from SubmitConsent. Outstanding
	// answers "which boxes has this Customer not yet answered", which is the
	// right question for a capture surface and exactly the wrong one for a
	// withdrawal: the consents worth withdrawing are the ANSWERED ones, and
	// filtering by it would drop every withdrawal that had anything to take away.
	// Nothing is lost by omitting it, because a denial cannot churn a standing
	// answer into something its owner did not ask for — the worst it can do is
	// record `denied` over `denied`, which changes nothing and mails nobody.
	answers := consent.Answers{}
	if withdrawal.MarketingConsent {
		answers.MarketingConsent = deniedAnswer()
	}
	if withdrawal.NetworkingConsent {
		answers.NetworkingConsent = deniedAnswer()
	}

	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID: customer.ID,
		// The address as this proof established it, from the pending row rather
		// than from the Customer, exactly as the sign-in consent step takes it.
		Email: pending.Email,
		// This surface's own channel (migration 068). It comes through the same
		// door as the sign-in consent step — a passcode redeemed for a
		// pending-consent token — but it is not that surface: it demands no Policy
		// Acceptance and mints no session, and the compliance question `channel`
		// exists to answer is which surface an act came from. Recording it as
		// `signin` and telling the two apart by the absence of an acceptance and a
		// session id was correct and is no longer necessary.
		Channel: consent.ChannelPasscodeWithdrawal,
		// A passcode was redeemed to get the token that reached this line, so the
		// address is proven — passed explicitly rather than inferred from the
		// channel, as everywhere else. It is what makes the denial STICK: an
		// unproven No writes no state at all (consent/service.optionalStateWrite),
		// which on this surface would mean silently ignoring the withdrawal of
		// every consent that had actually been granted.
		EmailProven: true,
		Answers:     answers,
		Evidence:    withdrawal.Evidence,
	})
	if err != nil {
		return nil, err
	}

	// Confirmed by mail, through the mechanism #267 built and unchanged by this
	// surface: it sends only when the act took something away, it does not fail
	// the withdrawal when the provider refuses it, and handing the message over
	// stamps the evidence. A withdrawal made this way is confirmed exactly as one
	// made from an account is.
	s.confirmWithdrawal(ctx, customer, receipt)

	return &ConsentWithdrawalView{
		MarketingConsent:  string(receipt.MarketingConsent),
		NetworkingConsent: string(receipt.NetworkingConsent),
		Withdrew: ConsentWithdrewView{
			MarketingConsent:  receipt.Withdrawn.MarketingConsent,
			NetworkingConsent: receipt.Withdrawn.NetworkingConsent,
		},
	}, nil
}

// deniedAnswer is the only answer this surface can produce.
//
// A function rather than a variable, and a fresh false rather than a shared
// pointer, so that there is nothing here to flip: "this surface can only
// withdraw" is then a fact about what the code CAN express, and widening it
// would take a visible new function rather than an edited literal. Consent
// answers are pointers because nil means "the box was not shown" — see
// consent.Answers — so a denial has to be the address of something.
func deniedAnswer() *bool {
	denied := false
	return &denied
}
