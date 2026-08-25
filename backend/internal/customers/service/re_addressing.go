package service

import (
	"context"
	"database/sql"
	"time"
)

// The Customer half of accepting a Sale Re-addressing (#421, parent #419,
// ADR 0058): the click on a Re-addressing Link is Proof of Email Ownership,
// and the person it proves is minted here, by the module whose authority a
// Customer record is (ADR 0010) — exactly as holder.go mints the Holder an
// Assignment Link's click proves. Sales declares the seam; this file
// satisfies it; sales never imports this package.

// AcceptReAddressedSale mints or matches the Verified Customer the corrected
// address proved itself to be, INSIDE THE SALES MODULE'S TRANSACTION, and
// reports the id the Sale moves to.
//
// IN THE CALLER'S TRANSACTION, because the Sale must change hands in the same
// commit that mints the person it changes hands to: a Customer minted and then
// a Sale that failed to move would be a Verified Customer who proved an
// address and got nothing, and the reverse would be a Sale pointing at nobody.
// UpsertForSale takes a transaction on the same terms.
//
// MATCH ON THE NORMALISED ADDRESS, CREATE IF ABSENT, THROUGH THE ONE STATEMENT
// every other proof uses (repository.verifyCustomerSQL). verified_at is stamped
// only the first time. No Mail Locale is named, on the Assignment Link's
// reasoning: the reader is in a mail client, not on a page.
//
// THE GHOST'S FACTS CARRY ONLY INTO A NAMELESS CUSTOMER (ADR 0058). A record
// nobody has named — brand new, or minted inert by somebody else's checkout —
// takes the ghost's name, and its Tax ID and phone where it has none: the
// buyer typed them about themselves, only under the wrong address. An existing
// Customer with a name is not touched: what they said about themselves wins
// over what a stranger's checkout recorded, and the Sale's own snapshot keeps
// what was typed either way.
//
// IT GRANTS NO CONSENT AND MINTS NO SESSION. The session is the caller's next
// step (SignInAcceptedReAddressing), after the commit, and goes through the
// consent gate as every sign-in does.
func (s *Service) AcceptReAddressedSale(ctx context.Context, tx *sql.Tx, correctedEmail, ghostCustomerID string, now time.Time) (string, error) {
	customer, err := s.repo.VerifyCustomerTx(ctx, tx, correctedEmail, now)
	if err != nil {
		return "", err
	}
	if customer.FirstName == "" && customer.LastName == "" {
		if _, err := s.repo.CarryFactsIntoNamelessCustomer(ctx, tx, customer.ID, ghostCustomerID); err != nil {
			return "", err
		}
	}
	return customer.ID, nil
}

// SignInAcceptedReAddressing is the Customer Session an accepted Re-addressing
// Link earns: THE SAME sign-in a passcode produces, through the same
// convergence (signInProvenEmail), so the consent gate applies exactly as it
// does at either door — a Customer with the Policy outstanding gets the
// consent step and no session, and everything else about the two outcomes is
// identical on the wire.
//
// Called AFTER the sales transaction has committed, on the address that
// transaction just proved and moved the Sale to. It re-runs the verify
// statement, which is idempotent (COALESCE keeps the first verified_at), and
// names no Locale and no Avatar: a mail client is not a page.
func (s *Service) SignInAcceptedReAddressing(ctx context.Context, correctedEmail string) (*SignInOutcome, error) {
	return s.signInProvenEmail(ctx, correctedEmail, s.now(), "", "")
}
