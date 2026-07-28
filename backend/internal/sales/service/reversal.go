package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// onlineChannel is the one Sales Channel a Customer can reverse on their own.
// An In-Person Sale or an imported one records money the platform never
// touched, so there is nothing here to give back and nobody here to ask.
const onlineChannel = "online"

// activeSaleStatus is the Ticket Sale that still stands. A reversed one has
// already been undone and cannot be undone twice.
const activeSaleStatus = "active"

// SaleReversalResult is what the Customer is told after undoing a purchase: the
// sale they undid, still identified by the Sale Confirmation reference on their
// receipt, and when it happened.
//
// The reference is deliberately still here. A reversed Ticket Sale is never
// deleted — it keeps its reference and stays visible to both the Customer and
// the Organization — so the thing a buyer would quote in a support thread is the
// same thing before and after.
type SaleReversalResult struct {
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is always "reversed"; a reversal that did not happen is an error,
	// never a result carrying some other word.
	Status     string `json:"status"`
	ReversedAt string `json:"reversed_at"`
}

// ReverseOwnSale is a Customer undoing their own Online Sale within the Reversal
// Window (ADR 0018).
//
// customerID is the authorization and the only one there is. It comes from a
// Customer Session — never from a Confirmation Link, which travels by email and
// can be forwarded, and never from anything in the request body — and the sale
// is loaded scoped to it, so a sale belonging to somebody else is not refused so
// much as invisible.
//
// The order of the checks below is not arbitrary. Everything that can refuse
// without touching anything runs first, so a refusal is always a no-op: the
// Ticket Sale, the Ticket Types' capacity and the buyer's inbox are all exactly
// as they were. Only then is the Payment Provider asked, and only on its
// agreement is anything written. The email goes last of all, after the reversal
// has committed, because a buyer must never be told their tickets are gone while
// the transaction that removes them can still roll back.
func (s *Service) ReverseOwnSale(ctx context.Context, customerID, ticketSaleID string) (*SaleReversalResult, error) {
	sale, err := s.repo.GetCustomerTicketSale(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if sale == nil {
		return nil, sales.ErrTicketSaleNotFound()
	}
	if sale.Status != activeSaleStatus {
		return nil, sales.ErrSaleAlreadyReversed()
	}
	if sale.Channel != onlineChannel {
		return nil, sales.ErrSaleNotReversible()
	}

	// Can this sale's Payment actually be undone? A free claim always can —
	// nothing was collected, so voiding the sale is the whole reversal (ADR 0017)
	// — while a paid one can only when the Payment Provider that collected it
	// supports reversal. This is the same question, answered by the same rule,
	// that decided whether the Customer Area offered the button at all.
	reversal := platform.NewPaymentReversal(s.provider)
	if !reversal.Supports(sale.PaymentMethod.String) {
		return nil, sales.ErrSaleNotReversible()
	}

	// The window, enforced here and not merely drawn in a client. An Online Sale
	// always has an Event start — publishing requires one and only a published
	// Event is sellable — so a missing start is a sale with no window rather than
	// a sale with an unbounded one. Inventing a deadline in that case would be
	// inventing permission.
	if !sale.EventStartsAt.Valid {
		return nil, sales.ErrReversalWindowClosed()
	}
	now := s.now()
	// SoldAt is the Payment approval instant on an Online Sale: the sale commits
	// in the transaction that approves the Payment.
	if !platform.NewReversalWindow(sale.SoldAt, sale.EventStartsAt.Time).IsOpenAt(now) {
		return nil, sales.ErrReversalWindowClosed()
	}

	// The provider is called BEFORE anything local is written (ADR 0018): a
	// refusal must leave the sale untouched, and capacity released and resold
	// cannot be reclaimed if a local rollback were attempted afterwards.
	//
	// A free claim skips this because there is no provider to call — no money was
	// collected, by anyone. Nothing else may skip it: the branch is on the
	// Payment Method the sale was settled under, not on convenience.
	if sale.PaymentMethod.String != platform.FreePaymentMethod {
		if err := s.provider.Reverse(ctx, sale.ClientTransactionID); err != nil {
			// The provider's code goes to the log and not to the buyer. PayPhone
			// publishes no "too late" code, so any explanation offered here would
			// be a guess about somebody's money.
			s.logger.Error("sale reversal refused by the payment provider; the Ticket Sale is untouched",
				"ticket_sale_id", sale.ID,
				"confirmation_ref", sale.ConfirmationRef,
				"client_transaction_id", sale.ClientTransactionID,
				"error", err,
			)
			return nil, sales.ErrSaleReversalFailed(sale.ConfirmationRef)
		}
	}

	// The shared reversal primitive: it voids the sale and returns every line's
	// quantity to its Ticket Type's sold_count in one transaction, under the same
	// locks a Sale Import undo takes. Capacity restoration is defined once, in
	// that primitive, and never re-implemented per caller.
	//
	// It skips a sale that is no longer active, which is what makes a double
	// submit safe: two concurrent requests serialize on the row lock, one
	// reverses and the other finds nothing to do and comes back empty. Capacity
	// is therefore restored exactly once however many times the button is
	// pressed.
	reversedSales, err := s.repo.ReverseSales(ctx, repository.ReverseSalesInput{
		EventID:        sale.EventID,
		OrganizationID: sale.OrganizationID,
		SaleIDs:        []string{sale.ID},
		Actor:          sales.ReversalActorCustomer,
		Now:            now,
	})
	if err != nil {
		return nil, err
	}
	if len(reversedSales) == 0 {
		// Somebody else got there first — the buyer's own second press, or a Sale
		// Import undo landing in between. The honest answer is that this request
		// reversed nothing, and it is the same answer a request arriving a minute
		// later would get.
		return nil, sales.ErrSaleAlreadyReversed()
	}

	// Only now, with the reversal committed, is the buyer told. A failure to send
	// is swallowed exactly as every other notice in this module is: the tickets
	// are gone whether or not the email lands, and re-running the reversal to
	// retry an email would be far worse than a missing one.
	reversed := reversedSales[0]
	_ = s.email.SendSaleVoided(ctx, platform.SaleVoided{
		To:           reversed.CustomerEmail,
		CustomerName: displayName(reversed.CustomerFirstName, reversed.CustomerLastName),
		EventName:    sale.EventName,
		Reference:    reversed.ConfirmationRef,
	})

	return &SaleReversalResult{
		TicketSaleID:    sale.ID,
		ConfirmationRef: reversed.ConfirmationRef,
		Status:          "reversed",
		ReversedAt:      now.UTC().Format(time.RFC3339),
	}, nil
}
