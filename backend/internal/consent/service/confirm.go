package service

import (
	"context"
	"errors"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
)

// The double opt-in's resolving half (#255, parent #249, ADR 0035).
//
// A guest ticked an optional box for an address nobody had proven, so it
// pended: recorded faithfully, denied for sending, unanswered for prompting,
// never expiring. This file is where that ends — from the confirmation link
// carried in the Sale Confirmation, whose press from the inbox is itself the
// proof of ownership the tick lacked.
//
// The token, the endpoint and the page are the customers module's (it holds the
// signing key and owns the Customer). What lives here is the only part that is
// a consent rule: what a press MEANS.

// PendingConfirmations reports which optional consents currently sit in Pending
// Confirmation, and it is the question the Sale Confirmation asks before it
// writes a line offering to resolve one.
//
// IT READS STATE AT THE MOMENT IT IS ASKED, not what any particular checkout
// left behind. #253's capture discards its receipt deliberately — the sales
// module states what happened and has no use for what it made true — so this is
// how a receipt learns that its own checkout pended something. The consequence
// is worth naming: what the line offers is everything pending on this Customer
// when the mail is written, which on a second guest checkout for the same
// address may include a box an earlier one pended. That is the honest scope. A
// per-sale answer would offer to confirm a tick and silently leave an identical
// one standing beside it.
//
// It is EMPTY for a Customer with nothing pending, which is the great majority
// — a signed-in buyer's answers are proven and land granted or denied, and
// somebody who ticked nothing has nothing to confirm — and an empty answer is
// what leaves an ordinary receipt exactly as it was.
func (s *Service) PendingConfirmations(ctx context.Context, customerID string) (consent.Pending, error) {
	state, err := s.repo.ConsentState(ctx, customerID)
	if err != nil {
		return consent.Pending{}, err
	}
	return pendingFrom(state), nil
}

// ConfirmPending resolves the Pending Confirmations a signed confirmation link
// names: it flips them to granted, writes a FRESH Consent Record for the act on
// the `email_confirmation` channel, stamps `confirmed_at` on the tick each one
// came from, and keeps `digest_enabled` in lockstep — all in one transaction.
//
// WHY THE PRESS IS RECORDED AS EmailProven, which is the whole argument the
// double opt-in rests on: the link travelled in exactly one place, a Sale
// Confirmation addressed to this Customer's own stored address, and presenting
// it is evidence of access to that inbox. ADR 0035 says so in as many words —
// "clicking from the inbox being itself proof of ownership" — and #256 made the
// same argument for the unsubscribe link. It is what makes the answer `granted`
// rather than another pending, which is the entire point of pressing it.
//
// THE STALENESS RULE, and it is deliberate rather than incidental: A PRESS
// CONFIRMS THE INTERSECTION OF THREE SETS — what the link was minted for, what
// is pending NOW, and nothing else. Two consequences follow, and both are the
// behaviour the ticket asks for:
//
//   - A PROVEN LATER ANSWER OUTRANKS AN OLDER PENDING. If the owner has since
//     declined from their Customer Area, or answered while signed in, or
//     unsubscribed, their state is `granted` or `denied` — not pending — so it
//     is outside the intersection and this press does not touch it. A link is
//     non-expiring precisely because it can be pressed a year late, and a year-
//     late press must not overwrite a decision the person made in the meantime.
//     Their own proven answer is the better evidence and it stands.
//   - A SECOND PRESS IS HARMLESS AND HONEST. The first press granted, so the
//     second finds nothing pending, confirms nothing, and says so — a
//     ConfirmationResult with an empty Confirmed and the states as they stand.
//     It is not an error: the same link is in the same email forever, and
//     pressing it twice is one request arriving twice with the same meaning.
//
// A press that confirms nothing writes NO Consent Record, which is the one
// place this departs from the unsubscribe link's "a repeated act is still an
// act". The difference is what there would be to record. An unsubscribe always
// carries an answer — No, again — and its second record says a person asked for
// quiet a second time. Here there is no answer: no box was shown, nothing was
// ticked, and a row with all three answers NULL asserts a capture act in which
// nothing was captured, which is a shape the schema reserves for "the box was
// not shown". Writing one would put noise in an evidence log to describe a
// no-op. It is logged instead, so a support thread can still see the press.
//
// The read is taken FOR UPDATE inside the transaction that writes, so a press
// racing the owner's own toggle cannot interleave into a state neither asked
// for.
func (s *Service) ConfirmPending(ctx context.Context, confirmation consent.Confirmation) (consent.ConfirmationResult, error) {
	if confirmation.CustomerID == "" {
		return consent.ConfirmationResult{}, errors.New("consent confirm: no customer")
	}

	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return consent.ConfirmationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	state, err := s.repo.ConsentStateForUpdateTx(ctx, tx, confirmation.CustomerID)
	if err != nil {
		return consent.ConfirmationResult{}, err
	}

	confirming := confirmation.Scope.Intersect(pendingFrom(state))
	if !confirming.Any() {
		// Nothing to do, and nothing to record. The states are read back from the
		// row this transaction has already locked, so what is reported is what is
		// true and not what the presser hoped.
		s.logger.Info("consent confirmation link pressed with nothing left to confirm",
			"customer_id", confirmation.CustomerID,
			"marketing_consent", string(state.MarketingConsent),
			"networking_consent", string(state.NetworkingConsent),
		)
		return consent.ConfirmationResult{
			MarketingConsent:  state.MarketingConsent,
			NetworkingConsent: state.NetworkingConsent,
		}, nil
	}

	// Only the boxes being confirmed are answered. The other is nil — NOT SHOWN
	// on this surface — so a press that resolves a marketing pending leaves a
	// standing Networking Consent exactly as it was, rather than reading the
	// absence as a refusal.
	granted := true
	answers := consent.Answers{}
	if confirming.MarketingConsent {
		answers.MarketingConsent = &granted
	}
	if confirming.NetworkingConsent {
		answers.NetworkingConsent = &granted
	}

	// PolicyAcceptance is nil for the same reason. A press confirms an optional
	// opt-in; it is not a re-acceptance of the current Policy Version, and a
	// press that silently accepted one on somebody's behalf would be evidence of
	// something that did not happen.
	receipt, err := s.capture(ctx, tx, consent.Capture{
		CustomerID:  confirmation.CustomerID,
		Email:       confirmation.Email,
		Channel:     consent.ChannelEmailConfirmation,
		EmailProven: true,
		Answers:     answers,
		Evidence:    confirmation.Evidence,
	})
	if err != nil {
		return consent.ConfirmationResult{}, err
	}

	if confirming.MarketingConsent {
		if err := s.repo.StampConfirmedTx(ctx, tx, confirmation.CustomerID, repository.BoxMarketing, receipt.CapturedAt); err != nil {
			return consent.ConfirmationResult{}, err
		}
	}
	if confirming.NetworkingConsent {
		if err := s.repo.StampConfirmedTx(ctx, tx, confirmation.CustomerID, repository.BoxNetworking, receipt.CapturedAt); err != nil {
			return consent.ConfirmationResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return consent.ConfirmationResult{}, err
	}

	return consent.ConfirmationResult{
		Confirmed:         confirming,
		MarketingConsent:  receipt.MarketingConsent,
		NetworkingConsent: receipt.NetworkingConsent,
	}, nil
}

// pendingFrom reads the two optional states as the one question this file asks
// of them: is somebody else's tick standing here, unresolved?
func pendingFrom(state repository.CustomerConsentState) consent.Pending {
	return consent.Pending{
		MarketingConsent:  state.MarketingConsent == consent.StatePendingConfirmation,
		NetworkingConsent: state.NetworkingConsent == consent.StatePendingConfirmation,
	}
}
