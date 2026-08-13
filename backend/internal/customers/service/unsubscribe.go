package service

import (
	"context"
	"crypto/hmac"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Unsubscribing from the Follow Digest (#224, parent #215, ADR 0030).
//
// UNSUBSCRIBING IS A SWITCH, NOT A PURGE. Nothing in this file reaches a Follow,
// and that is the decision the whole feature rests on rather than an omission.
// The Digest is the platform's first non-transactional mail, so it is the first
// that needs an opt-out reachable WITHOUT SIGNING IN — it is read in a mail
// client months after anybody last signed in, and an opt-out gated behind a
// passcode is not an opt-out. The moment such a link exists, mail security
// scanners that prefetch every link in every message can press it. Had
// Unsubscribe meant "Unfollow everything", a corporate scanner would have
// silently wiped its own users' lists and nobody would ever have learned why.
//
// TWO ENTRY POINTS reach the one switch below:
//
//   - The signed link in every Digest footer, which carries its own authority
//     and needs no session. The landing page confirms with a POST, so a bare
//     prefetch reaches no act at all.
//   - The toggle in the Customer Area, which needs a full Customer Session
//     because it is reached from inside the Area and reports what it did.
//
// TRANSACTIONAL MAIL IS NOT ON THIS SWITCH. Nothing that sends a One-time
// Passcode or a Sale Confirmation reads the state this file writes, and nothing
// ever should: a passcode is how a person signs in and a confirmation is the
// receipt for something they just paid for.
//
// BOTH ENTRY POINTS ARE CONSENT ACTS NOW (#256, parent #249, ADR 0034).
// Marketing Consent and the Follow Digest are one switch, so switching the
// Digest is answering the marketing box — and an answer is recorded as one:
// each write below goes through the consent module's Capture, which writes the
// state, the immutable Consent Record with the circumstances of the act, and
// `digest_enabled` in lockstep, in one transaction. Neither entry point writes
// `digest_enabled` itself any more, and nothing in this module does: a state
// change with no evidence beside it is the thing this feature exists to make
// impossible.
//
// THE SCOPE LINE MOVED, and this comment is where a future reader will look, so
// it says where it moved to (#265, #266). It used to read that nothing here was
// the withdrawal feature: no evidence of a withdrawal as such, no confirmation
// to the titular, no rights request. That was true of #256 and is no longer true
// of this file, because #265 crossed the line ON PURPOSE and at this exact
// point.
//
// WHAT CROSSED IT. An Unsubscribe IS a Consent Withdrawal — the Marketing-
// specific, link-driven special case of one (CONTEXT.md) — and the platform now
// says so rather than treating the resemblance as a coincidence. Every act
// below therefore records what it TOOK AWAY as well as what it answered: the
// Capture it performs writes each optional consent's state as it stood
// immediately before, so a press that moved somebody out of `granted` is legible
// as a withdrawal from the single row that performed it (#266).
//
// AND THE CUSTOMER IS TOLD (#267). Both entry points below now confirm by email
// that a withdrawal happened — but only when the act actually took something
// away, which the receipt reports and neither entry point decides for itself.
// That mail is TRANSACTIONAL and is sent even to somebody who has just withdrawn
// Marketing Consent, which is not a contradiction of the paragraph above about
// this switch reaching no transactional mail: the switch still reaches none, and
// this is the confirmation OF the switch being thrown. Handing it to the
// provider stamps the evidence. See confirmWithdrawal.
//
// WHAT STILL HAS NOT. The line has moved, not vanished, and the three things it
// still holds back are worth naming because each is somebody's reasonable next
// idea:
//
//   - NOTHING HERE REACHES A FOLLOW. Unsubscribing is a switch, not a purge, and
//     the whole argument at the top of this file is unaffected by any of the
//     above.
//   - NOTHING HERE TOUCHES NETWORKING CONSENT. One box is shown and one box is
//     answered; the other is nil — NOT SHOWN — and reading that nil as a refusal
//     would turn an unsubscribe into a Withdraw All. Withdrawing everything is a
//     deliberate act a Customer performs on `/privacy`, behind a dialog that
//     tells them what it means.
//   - NOTHING HERE PROPAGATES ANYWHERE. There is no networking application
//     integration to propagate to; whoever builds one reads this platform's
//     state live and caches nothing (ADR 0038).
//
// The word is WITHDRAWAL. `revoke` stays reserved for credentials — sessions,
// in-flight pending consents — and the Spanish copy's "revocatoria" is counsel's
// legal term rendered for a reader, not a second name for the concept.

// unsubscribeLinkPath is the STOREFRONT route the link points at, never an API
// one (ADR 0008), and it is a page rather than an endpoint for the reason the
// whole feature exists: the page confirms with a POST, so a scanner that merely
// opened the address has changed nothing.
const unsubscribeLinkPath = "/unsubscribe"

// unsubscribeTokenPurpose is the domain separator baked into every unsubscribe
// payload, and it is what stops the two signed tokens in this service from being
// one credential.
//
// Both are HMACs under the SAME key, so without it a Confirmation Link naming a
// Ticket Sale and an unsubscribe link naming a Customer would be
// interchangeable strings — and either could be presented where the other was
// expected. The purpose is inside the signed payload rather than beside it, so
// it cannot be edited off a genuine token.
const unsubscribeTokenPurpose = "unsubscribe"

// DigestSubscriptionView is the Customer's Digest switch as the API reports it.
//
// One field, and the name says which way round it is. A view rather than a bare
// boolean because both entry points answer with it and the Follows listing
// embeds the same fact, so all three surfaces can only ever say it one way.
type DigestSubscriptionView struct {
	DigestEnabled bool `json:"digest_enabled"`
}

// UnsubscribeLinkURL returns the signed unsubscribe link for one Customer, to be
// carried in the footer of every Follow Digest sent to them.
//
// IT IS THE SAME LINK EVERY WEEK, deliberately. The token carries no expiry and
// no week: a Digest sits in an inbox for months, and the most likely moment for
// somebody to press unsubscribe is on an old one they have just rediscovered. A
// link that had gone stale by then would send a person who wants quiet to a
// sign-in page, which is exactly the opt-out ADR 0030 refuses to ship. What the
// token can do is bounded instead of dated — it names one Customer and switches
// one reversible flag — so a leaked one costs its holder nothing they cannot
// undo from their own Area.
//
// It is minted here rather than in the digest module because the signing key is
// this service's (ConfirmationLinkConfig) and the Customer is this module's
// subject. The digest module declares the narrow interface and calls it.
func (s *Service) UnsubscribeLinkURL(customerID string) (string, error) {
	if len(s.links.Secret) == 0 {
		// Refusing beats emitting an unsigned or default-signed link, which would
		// be an unauthenticated way to silence any Customer whose id somebody
		// could guess.
		return "", customers.ErrUnsubscribeLinkUnavailable()
	}
	return s.links.StorefrontBaseURL + unsubscribeLinkPath + "?token=" + s.signUnsubscribeLink(customerID), nil
}

// Unsubscribe turns the Follow Digest off for the Customer a signed link names.
//
// NO SESSION, BY CONTRACT. The token is the whole authority, it names one
// Customer, and the only thing it can do is set one flag to false. That is the
// trade ADR 0030 made knowingly: an opt-out that demanded a sign-in first would
// not be an opt-out, so the link's power is cut down until being unauthenticated
// costs nothing worth having.
//
// UNFOLLOWING NOTHING. There is no path from here to a Follow table, and the
// Customer's list is exactly as it was afterwards — which is what makes a mail
// scanner's prefetch survivable even if it were ever to reach this far.
//
// Pressing it twice is not an error: every Digest a person ever received carries
// the same link, and a second press is the same request arriving twice with the
// same meaning. It writes a second Consent Record, and that is right — the log
// says what HAPPENED, and a repeated act is still an act (CONTEXT.md).
//
// WHY THIS ACT IS RECORDED AS EmailProven DESPITE THERE BEING NO SESSION, which
// is the one genuinely debatable decision in #256:
//
//   - The token travelled in exactly one place: a Follow Digest addressed to
//     this Customer's own stored address. Presenting it is evidence of access to
//     that inbox, which is the same argument ADR 0035 makes for the confirmation
//     link — "clicking from the inbox being itself proof of ownership". A signed
//     link sent to a proven address is weaker than a passcode and much stronger
//     than a typed-in claim, and it is the only proof this surface can have,
//     since demanding a session here would be refusing to ship an opt-out at all
//     (ADR 0030).
//   - AND THE ANSWER IS ALWAYS NO. An unproven answer is written only over an
//     unanswered state (consent/service.Capture), so recording this as unproven
//     would leave the Digest running for every Customer who had granted Marketing
//     Consent — the press would be silently ignored by exactly the people it is
//     for. This surface can never grant anything, so treating it as proven can
//     only ever silence mail, never authorize any. The worst a stolen or leaked
//     token achieves is still what ADR 0030 priced in: a weekly email its owner
//     can switch back on from their own Area, which is now also how they grant
//     the consent again.
//
// A prefetching mail scanner is handled where it always was — the link points at
// a Storefront page, this endpoint is POST-only, and a GET is answered 405 — so
// nothing here rests on the proof being unforgeable.
func (s *Service) Unsubscribe(ctx context.Context, token string, evidence consent.Evidence) (*DigestSubscriptionView, error) {
	customerID, err := s.parseUnsubscribeLink(token)
	if err != nil {
		return nil, err
	}

	customer, err := s.repo.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		// A validly signed token for a Customer who no longer exists is spent. It
		// is reported as an invalid link rather than as a missing Customer: the
		// holder is not entitled to learn which of the two it was, and an
		// unauthenticated endpoint that distinguished them would be a way to test
		// whether a Customer id exists.
		return nil, customers.ErrUnsubscribeLinkInvalid()
	}

	declined := false
	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID: customer.ID,
		// The Customer's own stored address, because that is the only address this
		// surface could be about: the link was mailed to it, and nobody typed
		// anything.
		Email:   customer.Email,
		Channel: consent.ChannelUnsubscribeLink,
		// See the doc comment above for why this is true.
		EmailProven: true,
		// One box, and only one. Policy Acceptance and Networking Consent are nil
		// — NOT SHOWN HERE — so this act neither accepts a policy nor touches a
		// standing Networking Consent. Reading those nils as refusals would turn
		// an unsubscribe into a Withdraw All, which is a deliberate act performed
		// somewhere a Customer has been told what it means, and never something a
		// link in a footer does on their behalf.
		Answers: consent.Answers{MarketingConsent: &declined},
		// No SessionID: this route is session-less by contract, and an empty
		// evidence field is recorded as "not collected" rather than as a blank.
		Evidence: evidence,
	})
	if err != nil {
		return nil, err
	}

	// AND THE CUSTOMER IS TOLD, which on this surface matters more than on any
	// other (#267). A press here authenticates nobody and the link is in every
	// Digest the person ever received, so this mail is how the address's real
	// owner finds out that somebody — a colleague forwarded the Digest, a mail
	// client prefetched past a POST it should not have — acted on their behalf.
	// It goes to the Customer's own stored address for exactly that reason.
	s.confirmWithdrawal(ctx, customer, receipt)

	return &DigestSubscriptionView{DigestEnabled: false}, nil
}

// confirmWithdrawal tells the Customer that a Consent Withdrawal took something
// away, and records on the evidence that they were told (#267, parent #265).
//
// IT RETURNS NOTHING, AND THAT IS THE CONTRACT. A failure to send must not fail
// the withdrawal: the withdrawal is the thing that had to happen, it has already
// committed by the time this is called, and refusing the request now would tell
// a person who asked for quiet that their request failed when it did not. Every
// failure is logged instead, at error level, because a confirmation nobody got
// and nobody noticed is the one this feature exists to prevent.
//
// IT SENDS ONLY WHEN SOMETHING MOVED, and it does not decide that for itself.
// The receipt says what the act withdrew, computed inside the transaction that
// observed the state it replaced (consent.Receipt.Withdrawn) — a caller that
// reasoned from its own answer would mail everybody who switched off a switch
// that was already off, which is a change that did not happen.
//
// NOTHING HERE READS A CONSENT STATE OR `digest_enabled`. This is transactional
// mail on the same footing as a Sale Confirmation or a passcode, sent to
// somebody who has just asked to stop receiving marketing precisely because
// suppressing it would make the one act that must be confirmed the one act met
// with silence. A "respect the Customer's preferences" check added here later
// would be the bug.
//
// The stamp is written only where the provider actually took the message, so
// `confirmation_sent_at` says "this platform handed the confirmation over" and
// never "we composed one". Null means not sent, which is a true and useful thing
// for the evidence to say.
func (s *Service) confirmWithdrawal(ctx context.Context, customer *repository.Customer, receipt consent.Receipt) {
	if !receipt.Withdrawn.Any() {
		return
	}
	if s.email == nil {
		// A deployment with no sender still performed the withdrawal, and says so
		// loudly rather than leaving a Customer silently unconfirmed — the same
		// posture UnconfiguredDigestSender takes for the same reason.
		s.logger.Error("consent withdrawal not confirmed: no email sender is configured",
			"customer_id", customer.ID,
			"record_id", receipt.RecordID,
		)
		return
	}

	if err := s.email.SendConsentWithdrawalConfirmation(ctx, platform.ConsentWithdrawalConfirmation{
		// The Customer's own stored address, never one a request named: this mail
		// is about what the platform recorded against that Customer, and the
		// unsubscribe link carries no address at all.
		To: customer.Email,
		// No sale is involved, so the chain is the remembered Mail Locale and then
		// English (ADR 0033). A message about somebody's rights is the last one
		// that may arrive in a language they cannot read.
		Locale: platform.ResolveMailLocale("", customer.MailLocale),
	}); err != nil {
		s.logger.Error("consent withdrawal confirmation could not be sent",
			"customer_id", customer.ID,
			"record_id", receipt.RecordID,
			"error", err,
		)
		return
	}

	if err := s.consent.ConfirmationSent(ctx, receipt.RecordID, s.now()); err != nil {
		// The Customer HAS been told; only the annotation failed. Logged and left,
		// because failing the withdrawal over it would undo an act that happened
		// and a mail that has already gone out.
		s.logger.Error("consent withdrawal confirmation could not be stamped on the evidence",
			"customer_id", customer.ID,
			"record_id", receipt.RecordID,
			"error", err,
		)
	}
}

// SetDigestEnabled is the Customer Area's toggle: the other entry point, and the
// only one that turns the Digest back ON.
//
// RE-ENABLING IS DELIBERATELY NOT REACHABLE FROM A LINK. An unsubscribe link is
// unauthenticated because a person who wants quiet must be able to have it
// without signing in; nothing about that argument applies to switching somebody's
// mail back on, which is a request to be written to and takes the same proof
// pressing Follow does.
//
// It sits behind a FULL Customer Session for the reason ListFollows does: a
// Confirmation Link session is a forwarded receipt, and possession of a
// forwarded email is not authority to subscribe that inbox to weekly mail.
//
// SWITCHING IT ON GRANTS MARKETING CONSENT AND SWITCHING IT OFF DENIES IT (ADR
// 0034), both under the `account_settings` channel and both EmailProven, which
// they are: this is the one surface here that runs behind a session established
// by Proof of Email Ownership. Proven is what makes the answer stick — it
// supersedes anything a guest left behind, and it is the only way an answer can
// become `granted` at all.
//
// The Customer Area is therefore where a Pending Confirmation is resolved by the
// owner simply using their own switch, and where a Customer whom the transition
// clause was still mailing turns their legacy default into an actual answer.
func (s *Service) SetDigestEnabled(ctx context.Context, sessionToken string, enabled bool, evidence consent.Evidence) (*DigestSubscriptionView, error) {
	session, customer, err := s.fullSession(ctx, sessionToken)
	if err != nil {
		return nil, err
	}

	answer := enabled
	receipt, err := s.consent.Capture(ctx, consent.Capture{
		CustomerID:  customer.ID,
		Email:       customer.Email,
		Channel:     consent.ChannelAccountSettings,
		EmailProven: true,
		// Only the marketing box was shown, so only it is answered. A Customer
		// switching their Digest does not re-accept a Policy Version and does not
		// re-answer Networking Consent, and nil is what says so.
		Answers: consent.Answers{MarketingConsent: &answer},
		Evidence: consent.Evidence{
			IP:        evidence.IP,
			UserAgent: evidence.UserAgent,
			// The session the act was made under — the same identifier the sign-in's
			// own record carries, so the two rows in the log tie together.
			SessionID: session.ID,
			OriginURL: evidence.OriginURL,
		},
	})
	if err != nil {
		return nil, err
	}

	// Switching it OFF is a Consent Withdrawal and is confirmed like any other
	// (#267). Switching it on is a grant and sends nothing, which is not a
	// special case written here: the receipt reports what the act withdrew, and a
	// grant withdrew nothing.
	s.confirmWithdrawal(ctx, customer, receipt)

	return &DigestSubscriptionView{DigestEnabled: enabled}, nil
}

// signUnsubscribeLink produces the token: which Customer, for which purpose,
// plus an HMAC over both.
//
// The same two-segment `payload.mac` shape and the same URL-safe encoding as a
// Confirmation Link, because it survives a mail client, a copy-paste and a query
// string for exactly the same reasons — and because one token format in this
// service is one format to get right.
func (s *Service) signUnsubscribeLink(customerID string) string {
	payload := unsubscribeLinkPayload(customerID)
	return encodeSegment([]byte(payload)) + "." + encodeSegment(s.confirmationLinkMAC(payload))
}

// parseUnsubscribeLink validates a token and returns the Customer it names.
//
// THE SIGNATURE IS CHECKED BEFORE ANYTHING IN THE PAYLOAD IS BELIEVED, in
// constant time, so a token that was edited, truncated or simply made up fails
// here and never reaches a write. Without that this endpoint would be an
// unauthenticated way to silence any Customer whose id somebody could guess.
func (s *Service) parseUnsubscribeLink(token string) (string, error) {
	if len(s.links.Secret) == 0 {
		return "", customers.ErrUnsubscribeLinkUnavailable()
	}

	encodedPayload, encodedMAC, found := strings.Cut(strings.TrimSpace(token), ".")
	if !found {
		return "", customers.ErrUnsubscribeLinkInvalid()
	}
	payload, err := decodeSegment(encodedPayload)
	if err != nil {
		return "", customers.ErrUnsubscribeLinkInvalid()
	}
	mac, err := decodeSegment(encodedMAC)
	if err != nil {
		return "", customers.ErrUnsubscribeLinkInvalid()
	}
	if !hmac.Equal(mac, s.confirmationLinkMAC(string(payload))) {
		return "", customers.ErrUnsubscribeLinkInvalid()
	}

	purpose, customerID, found := strings.Cut(string(payload), ":")
	// The purpose is checked as strictly as the signature. A genuinely signed
	// Confirmation Link presented here is a valid HMAC over a payload that means
	// something else, and only this comparison stops it being spent as an
	// unsubscribe.
	if !found || purpose != unsubscribeTokenPurpose || !isUUID(customerID) {
		return "", customers.ErrUnsubscribeLinkInvalid()
	}
	return customerID, nil
}

// unsubscribeLinkPayload is what the token asserts and the signature covers:
// that this is an unsubscribe, and for whom.
//
// No expiry, unlike a Confirmation Link's — see UnsubscribeLinkURL for why a
// dated opt-out would be no opt-out.
func unsubscribeLinkPayload(customerID string) string {
	return unsubscribeTokenPurpose + ":" + customerID
}
