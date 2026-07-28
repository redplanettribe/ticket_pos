package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

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
//
// The whole attempt is serialised per Ticket Sale, and the serialisation spans
// the provider call rather than merely the local write. See the lock below: a
// double-press that gets past this point twice returns somebody's money twice.
func (s *Service) ReverseOwnSale(ctx context.Context, customerID, ticketSaleID string) (*SaleReversalResult, error) {
	// The ownership read comes FIRST, before the lock. The lock is keyed on a
	// Ticket Sale id, and a caller who does not own that sale must not be able to
	// take it: contending for a stranger's lock would let anyone make a sale they
	// cannot see report itself already reversed to the buyer who can.
	sale, err := s.repo.GetCustomerTicketSale(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if sale == nil {
		return nil, sales.ErrTicketSaleNotFound()
	}

	// One reversal attempt at a time per Ticket Sale, across every server process,
	// held from here until this function returns — the provider call included.
	//
	// Without it, two presses of one button both read an active sale, both pass
	// every check below, and both POST the reversal to the Payment Provider; only
	// afterwards does one lose the row lock on the local write. Capacity would
	// still be correct and the buyer would still see one 200 — and the money would
	// have been returned twice. This is the one provider call the system never
	// retries, for exactly that reason (ADR 0018), so a concurrent second call is
	// no more acceptable than a retried one.
	//
	// A contended lock is not an error to report as one. It means another attempt
	// on this very sale is in flight, so the honest answer is the one a request
	// arriving a moment later gets: this purchase has already been undone. That
	// answer is occasionally premature — the attempt in flight may yet be refused
	// by the provider — and it is still the right one to give, because a buyer who
	// pressed twice has one reversal happening and no second one to make. Inventing
	// a "try again" code would invite exactly the retry that must not happen.
	release, locked, err := s.repo.LockTicketSaleForReversal(ctx, sale.ID)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, sales.ErrSaleAlreadyReversed()
	}
	defer func() {
		if err := release(); err != nil {
			s.logger.Error("could not release the Sale Reversal lock; further reversals of this Ticket Sale may be refused until the connection is recycled",
				"ticket_sale_id", sale.ID,
				"error", err,
			)
		}
	}()

	// Re-read under the lock. The row that decided anything must be the row as it
	// stands now that nobody else can be acting on it: the read above may have
	// happened while another attempt was mid-reversal, and a stale 'active' is
	// precisely how a second reversal reaches the provider. The loser of the race
	// re-reads a reversed sale here and answers correctly.
	sale, err = s.repo.GetCustomerTicketSale(ctx, customerID, ticketSaleID)
	if err != nil {
		return nil, err
	}
	if sale == nil {
		return nil, sales.ErrTicketSaleNotFound()
	}

	// The four checks, asked of the same shared rule the Customer Area asked
	// before it offered the button (platform.PaymentReversal.EligibilityAt). The
	// enforcing side maps each refusal to its own error; the deciding is not
	// re-implemented here, because an endpoint that refuses what the offer
	// promised is the bug that separating them would produce.
	now := s.now()
	_, refusal := platform.NewPaymentReversal(s.provider).EligibilityAt(sale.ReversalFacts(), now)
	if err := reversalRefusalError(refusal); err != nil {
		return nil, err
	}

	// The provider is called BEFORE anything local is written (ADR 0018): a
	// refusal must leave the sale untouched, and capacity released and resold
	// cannot be reclaimed if a local rollback were attempted afterwards.
	//
	// A free claim skips this because there is no provider to call — no money was
	// collected, by anyone. Nothing else may skip it: the branch is on the
	// Payment Method the sale was settled under, not on convenience.
	paid := sale.PaymentMethod.String != platform.FreePaymentMethod
	if paid {
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
	// It skips a sale that is no longer active, and takes a row lock while doing
	// so, which is the second of this function's two guards against a double
	// submit: a request that somehow reached here on an already-reversed sale
	// finds nothing to do and comes back empty. Capacity is therefore restored
	// exactly once however many times the button is pressed — but note that this
	// guard alone never protected the money, since by the time it runs the
	// provider has already been called. The advisory lock above is what does.
	reversedSales, err := s.repo.ReverseSales(ctx, repository.ReverseSalesInput{
		EventID:        sale.EventID,
		OrganizationID: sale.OrganizationID,
		SaleIDs:        []string{sale.ID},
		Actor:          sales.ReversalActorCustomer,
		Now:            now,
	})
	if err != nil {
		// The money is already on its way back and the Ticket Sale still stands:
		// the buyer keeps both their tickets and their payment, and the
		// Organization's dashboard shows revenue that no longer exists. Nothing
		// here can fix it — re-calling the provider risks reversing twice, and the
		// local write is the thing that just failed — so it is logged as an
		// incident an operator resolves by hand, exactly as the checkout's own
		// take-the-money-lose-the-sale case is (ADR 0018).
		//
		// The two ids are the whole point of the line: the Ticket Sale to correct
		// here, and the client transaction id to find the reversal on the
		// provider's dashboard.
		if paid {
			s.logger.Error("SALE_REVERSAL_NOT_COMMITTED: the Payment Provider reversed the payment but the Ticket Sale could not be marked reversed; the buyer keeps both the money and the tickets",
				"ticket_sale_id", sale.ID,
				"confirmation_ref", sale.ConfirmationRef,
				"client_transaction_id", sale.ClientTransactionID,
				"error", err,
			)
		}
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

// reversalRefusalError is how this module answers the shared reversal decision:
// one typed, buyer-facing error per refusal, and nil when there is none.
//
// The mapping is the whole of what this module adds to the decision it shares
// with the Customer Area (platform.PaymentReversal.EligibilityAt). The Area
// collapses every refusal into silence — no offer, no deadline — because a
// surface has nothing useful to say about which; an endpoint being pressed does,
// since the buyer is owed the difference between "this was never yours to undo
// here" and "you are too late".
//
// Two refusals share one code deliberately. A sale that is not an Online Sale
// and one whose Payment Provider cannot give the money back are the same fact to
// the person reading it — this is not yours to undo, and waiting will not change
// that — and the second is temporary in a way no message should promise.
func reversalRefusalError(refusal platform.ReversalRefusal) error {
	switch refusal {
	case platform.ReversalAllowed:
		return nil
	case platform.ReversalAlreadyReversed:
		return sales.ErrSaleAlreadyReversed()
	case platform.ReversalWindowClosed:
		return sales.ErrReversalWindowClosed()
	default:
		return sales.ErrSaleNotReversible()
	}
}
