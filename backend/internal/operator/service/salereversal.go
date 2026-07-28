package service

import (
	"context"

	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// OperatorReversalInput is the assertion an operator is recording: what the
// buyer actually got back, whether the platform kept its Platform Fee and Fee
// IVA, an optional note, and the operator's own email — which the handler takes
// from the Staff Session and never from the request body.
type OperatorReversalInput struct {
	Operator            string
	RefundedAmountCents *int
	PlatformFeeKept     *bool
	Note                *string
}

// ReverseSale records an Operator Reversal against the Ticket Sale a Sale
// Confirmation reference names: the operator refunded the buyer off-platform,
// and this is the platform learning it happened (#125, #123).
//
// It is one call into sales and nothing else. The composition this service
// exists for — the sale beside its Organization — belongs to the lookup that
// precedes this action; the action is about one sale, and the operator who got
// here came through that door and has already seen whose sale it is.
//
// No Payment Provider appears anywhere on this path, here or below it. The money
// left our account before the request was made, so the only thing calling a
// provider could achieve is refunding somebody twice. This is the same trust
// shape as recording a Payout: a record of money that already moved (ADR 0015).
func (s *Service) ReverseSale(ctx context.Context, confirmationRef string, in OperatorReversalInput) (*SaleReversal, error) {
	return s.money.ReverseSaleAsOperator(ctx, confirmationRef, salessvc.OperatorReversalInput{
		Operator:            in.Operator,
		RefundedAmountCents: in.RefundedAmountCents,
		PlatformFeeKept:     in.PlatformFeeKept,
		Note:                in.Note,
	})
}
