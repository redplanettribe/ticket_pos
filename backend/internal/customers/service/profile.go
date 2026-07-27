package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/customers"
	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// The Customer Area's "My info" profile (#102): the one place a person edits
// what the platform holds about them without starting a checkout.
//
// Two things are deliberately absent from everything below. There is no email —
// the address is the Customer's identity (ADR 0010), and this file cannot write
// it under any spelling. And there is no Ticket Sale — a profile edit moves the
// Customer's *current assertion* and nothing else; every sale keeps the name and
// Tax ID it was transacted under, immutably (ADR 0016).

// CustomerProfileView is the "My info" payload: the Customer's own record as
// they may see it. The email is here to be shown, never to be written.
//
// It is a narrower shape than CustomerSessionView on purpose. That one answers
// "who am I signed in as, and how far does this session reach"; this one answers
// "what does the platform hold about me". The Tax ID is unmasked for the same
// reason it is there: this is the person's own Tax ID shown back to the person,
// behind their own session.
type CustomerProfileView struct {
	Email       string  `json:"email"`
	FirstName   string  `json:"first_name"`
	LastName    string  `json:"last_name"`
	TaxIDType   *string `json:"tax_id_type"`
	TaxIDNumber *string `json:"tax_id_number"`
}

// UpdateProfileInput is one edit of the Customer's own assertion about
// themselves.
//
// The names must already be non-blank and the Tax ID already validated and
// normalised by the handler — this input is the accepted edit, not the raw
// request. The blank-name precondition is not a formality: repository.Upsert
// reads "blank name" as "never named", and this is the only write path in the
// system that could falsify that.
//
// TaxIDType and TaxIDNumber are nil together to clear the Tax ID and set
// together to assert one. Half a pair is not representable by the time it gets
// here, which is the point: the two halves are one fact.
type UpdateProfileInput struct {
	FirstName   string
	LastName    string
	TaxIDType   *string
	TaxIDNumber *string
}

// UpdateProfile writes the signed-in Customer's name and Tax ID and returns the
// profile as it now stands.
//
// The Customer written is the one on the session token and can be no other: no
// id, email, or any other identifier reaches this function, so there is nothing
// a caller could supply that would aim the write elsewhere.
//
// A Confirmation Link session is refused. It is minted from a token in a
// forwarded Sale Confirmation rather than from Proof of Email Ownership, and it
// exists to show one Ticket Sale; letting it rewrite the buyer's name or Tax ID
// would hand that power to whoever the confirmation was forwarded on to. This is
// the first Customer route to draw that line, and it draws it here rather than
// in middleware because "full session only" is a property of this write, not of
// the namespace: the Customer Area read below it is legitimately open to a link
// session, narrowed to its one sale.
func (s *Service) UpdateProfile(ctx context.Context, token string, in UpdateProfileInput) (*CustomerProfileView, error) {
	session, customer, err := s.authenticate(ctx, token)
	if err != nil {
		return nil, err
	}
	if session.TicketSaleID.Valid {
		return nil, customers.ErrCustomerSessionScopeInsufficient()
	}

	updated, err := s.repo.UpdateProfile(ctx, repository.UpdateProfileInput{
		CustomerID:  customer.ID,
		FirstName:   in.FirstName,
		LastName:    in.LastName,
		TaxIDType:   in.TaxIDType,
		TaxIDNumber: in.TaxIDNumber,
	})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		// The Customer vanished between authenticating and writing. The session
		// that named them cannot mean anything either.
		return nil, customers.ErrCustomerSessionNotFound()
	}

	return profileView(updated), nil
}

func profileView(customer *repository.Customer) *CustomerProfileView {
	view := &CustomerProfileView{
		Email:     customer.Email,
		FirstName: customer.FirstName,
		LastName:  customer.LastName,
	}
	if customer.TaxIDType.Valid && customer.TaxIDNumber.Valid {
		taxIDType, taxIDNumber := customer.TaxIDType.String, customer.TaxIDNumber.String
		view.TaxIDType, view.TaxIDNumber = &taxIDType, &taxIDNumber
	}
	return view
}
