package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/consent"
)

// CustomerConsent reports one Customer's consent state as it stands now (#271,
// parent #265).
//
// IT IS A DIFFERENT QUESTION FROM Outstanding, which is why it is a different
// method rather than more fields on that one. Outstanding answers "must this
// person be SHOWN this box?", collapsing Pending Confirmation and never-answered
// into one yes because a capture surface treats them identically. This answers
// "what does the platform currently believe?", and the two states it collapses
// are precisely the ones an Operator holding a withdrawal form has to tell apart:
// a tick nobody proved is something standing against the address, and never
// having been asked is nothing at all.
//
// READING IT WRITES NOTHING, and no caller may make it write. It is four columns
// off the Customer row, the same read every gate performs, and it is allowed to
// be a moment stale — a decision made from it is made by Capture, which takes
// the row's lock and observes the state again under it (repository.AppendTx).
// Nothing here is the basis of a write.
//
// An unknown Customer is repository.ErrCustomerNotFound, passed through so the
// caller can decide what to say about an address it was handed. This module has
// no opinion on that: whether the absence of a Customer may be disclosed is a
// question about who is asking, and the surfaces know who is asking.
func (s *Service) CustomerConsent(ctx context.Context, customerID string) (consent.CustomerConsent, error) {
	state, err := s.repo.ConsentState(ctx, customerID)
	if err != nil {
		return consent.CustomerConsent{}, err
	}
	return consent.CustomerConsent{
		MarketingConsent:  state.MarketingConsent,
		NetworkingConsent: state.NetworkingConsent,
		// The zero time where nothing was ever accepted, which is the same
		// "nothing to say" the null column carries and needs no second spelling.
		PolicyAcceptedAt: state.PolicyAcceptedAt.Time,
	}, nil
}
