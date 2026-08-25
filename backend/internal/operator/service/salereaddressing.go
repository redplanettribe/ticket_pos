package service

import (
	"context"

	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// ReAddressSaleInput is the Sale Re-addressing an operator is recording: the
// address the buyer meant (already parsed and normalised by the handler), an
// optional note, and the operator's own email — which the handler takes from
// the Staff Session and never from the request body.
type ReAddressSaleInput struct {
	Operator       string
	CorrectedEmail string
	Note           *string
}

// ReAddressSale records a Sale Re-addressing against the Ticket Sale a Sale
// Confirmation reference names, and has the corrected address mailed its
// Re-addressing Link (#420, ADR 0058).
//
// One call into sales and nothing else, on the Operator Reversal's terms: the
// composition this service exists for belongs to the lookup that precedes this
// action, and the action is about one sale. No Payment Provider appears
// anywhere on this path — nothing about a re-addressing moves money — and the
// result carries no token and no link, because the Operator must not be able
// to complete the acceptance themself.
func (s *Service) ReAddressSale(ctx context.Context, confirmationRef string, in ReAddressSaleInput) (*ReAddressing, error) {
	return s.money.ReAddressSaleAsOperator(ctx, confirmationRef, salessvc.ReAddressSaleInput{
		Operator:       in.Operator,
		CorrectedEmail: in.CorrectedEmail,
		Note:           in.Note,
	})
}
