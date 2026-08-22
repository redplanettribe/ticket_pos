package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Customer half of accepting a Ticket Assignment (#325, parent #322,
// ADR 0046).
//
// WHY THIS LIVES HERE AND NOT IN CATALOG. A Customer record is this module's,
// and `verified_at` in particular is the platform's single statement that
// somebody proved they own an address (ADR 0010). Catalog owns Tickets and knows
// when a Holder clicked; it must not learn how to write a Customer, or there
// would be two modules able to assert an identity and one of them would
// eventually assert one nobody proved.
//
// So catalog declares a narrow interface and this file satisfies it — the same
// arrangement OrganizationResolver and ReversalRequestResolver already use in
// the other direction. Catalog never imports this package.
//
// THE CLICK IS THE PROOF, AND THAT IS A DECISION WITH A PRECEDENT. ADR 0035 set
// the standard for the Consent Confirmation Link: "clicking from the inbox being
// itself proof of ownership". ADR 0046 spends it here. Until #325 the only two
// routes to `verified_at` were a completed One-time Passcode and a Google
// Sign-In; this is the third, and it is the only one that does not begin with
// somebody arriving at this platform under their own steam.
//
// WHAT ACCEPTING DOES NOT DO is as load bearing as what it does. It grants NO
// Marketing Consent and no consent of any kind — a Customer minted this way has
// agreed to nothing beyond holding a ticket (ADR 0046) — it mints no session, it
// asks for no password and no passcode, and it never touches a Tax ID or a phone
// number. Nothing in this file writes a consent row, and that absence is the
// feature.

// AcceptHolder mints or matches the Customer a Holder proved themselves to be,
// marks them Verified, and reports the name that record already holds.
//
// THREE BARE STRINGS AND NOT A STRUCT, which is the one ugly thing here and is
// deliberate. Catalog declares the interface this satisfies and must be able to
// name its types; a struct would have to live in a package both modules import,
// which would mean inventing a shared home for one value or letting catalog
// import this package — and catalog importing customers is the coupling this
// arrangement exists to avoid. Three strings need no shared home.
//
// The two name halves are the ONLY disclosure this route makes about an existing
// Customer, and the caller makes it ONLY AFTER THE CLICK: the accept page
// prefills them so a person is not retyping what the platform already knows,
// and a page reachable without the click that showed them would be an oracle for
// whether an address is registered (ADR 0035). Both are empty for a Customer
// nobody has ever named — an inert record minted by somebody else's checkout —
// and empty is exactly what the form should start at.
//
// MATCH ON THE NORMALISED ADDRESS, CREATE IF ABSENT, exactly as a passcode
// sign-in does, and through the very same repository statement — VerifyCustomer.
// One statement rather than two means "signing in and accepting a ticket reach
// the same person" is a property of there being one write, not of two writes
// agreeing. The address arrives already normalised by catalog.ParseHolderEmail
// at the moment the buyer typed it (migration 080), so the Customer somebody
// already has is the Customer they get.
//
// verified_at IS STAMPED ONLY THE FIRST TIME, by that statement's COALESCE, so
// accepting a second ticket years later does not rewrite the moment a person
// claimed their record.
//
// NO MAIL LOCALE IS NAMED, and the empty string is deliberate rather than
// unfinished. The two sign-in routes pass the Locale of the Storefront page the
// person was reading; there is no such page here — the Holder is somewhere in a
// mail client, and the language of the link they clicked was chosen by the
// middleware from their browser, not by them. Naming it would overwrite the Mail
// Locale of somebody who has been reading this platform in Spanish for a year
// because their phone asked for English once. The stored value stands.
func (s *Service) AcceptHolder(
	ctx context.Context,
	email string,
	now time.Time,
) (customerID, firstName, lastName string, err error) {
	customer, err := s.repo.VerifyCustomer(ctx, email, now, "")
	if err != nil {
		return "", "", "", err
	}
	return customer.ID, customer.FirstName, customer.LastName, nil
}

// NameHolder writes the name a Holder gave as the Customer's current asserted
// name.
//
// THE HOLDER'S OWN WORD OUTRANKS EVERYTHING, and this write carries no
// verification guard because it does not need one. repository.Upsert guards a
// Verified Customer's name against a checkout typing over it, since a guest
// checkout is somebody typing about a person who may not be them. Here the
// person editing is the person: they clicked a link that only ever travelled to
// their own address, which is the same proof a Customer Session rests on, and
// UpdateProfile — reached through such a session — carries no guard either.
//
// IT WRITES THE NAME AND NOTHING ELSE. Not the Tax ID, which is a fact about the
// sale's buyer and is never asked of a Holder; not the phone; not a consent.
// See repository.UpdateHolderName, which physically cannot write the others.
//
// Both halves must already be non-blank, trimmed and bounded by
// catalog.ParseHolderName. That precondition is not a formality: repository.
// Upsert reads a blank name as "never named", so a blank written here would make
// a later checkout free to overwrite a Verified Customer's own assertion.
func (s *Service) NameHolder(ctx context.Context, customerID, firstName, lastName string) error {
	return s.repo.UpdateHolderName(ctx, repository.HolderNameInput{
		CustomerID: customerID,
		FirstName:  firstName,
		LastName:   lastName,
	})
}

// HolderMailLocale reports the Mail Locale remembered for an address, or the
// empty string when no Customer holds it.
//
// IT IS FOR ONE CALLER AND ONE MESSAGE: the language the Assignment mail is
// written in (ADR 0033). Most addresses a buyer types belong to nobody here, and
// "" is the ordinary answer.
//
// IT IS NOT AN ORACLE, AND THE DIFFERENCE FROM ONE IS THE CALLER. Nothing about
// this answer reaches a response body: the buyer who typed the address is told
// only that their assignment was recorded, and the only thing that changes with
// the value is which language a message sent TO THAT ADDRESS is written in. A
// route that returned it, or that behaved observably differently for a known
// address, would be the disclosure ADR 0035 refuses — which is why this is a
// service method with one caller rather than anything a handler can reach.
func (s *Service) HolderMailLocale(ctx context.Context, email string) (string, error) {
	customer, err := s.repo.GetCustomerByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if customer == nil {
		return "", nil
	}
	return customer.MailLocale, nil
}

// Compile-time assurance that platform's Locale parsing is what the caller
// applies to the value above: this file hands back the RAW stored string, never
// a Locale, so that a malformed or unserved value is dropped by
// platform.ResolveMailLocale rather than smuggled into a message.
var _ = platform.ResolveMailLocale
