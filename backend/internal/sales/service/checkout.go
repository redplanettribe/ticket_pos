package service

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// Online checkout: the guest-facing begin/confirm pair behind the Payment
// Provider boundary (ADR 0012). Begin creates a pending Payment with a price
// snapshot and hands back the provider's redirect URL; Confirm — keyed by our
// client transaction id and idempotent — settles it, committing the Ticket Sale
// through the shared spine in the same transaction that marks the Payment
// approved.
//
// Capacity at begin counts sold_count PLUS live Capacity Holds — the pending
// Payment created here IS the hold on its quantities for the next
// sales.CapacityHoldWindow (ADR 0013) — so a Customer on the provider's payment
// page cannot lose their tickets to a faster buyer. The under-lock check at
// commit remains the one that cannot be raced past.

// checkoutReturnPath is the Storefront route the Payment Provider sends the
// Customer back to when its payment page is done with them. A Storefront URL,
// never an API one: the Customer must land on a page, and no browser may
// address the Go API directly (ADR 0008).
const checkoutReturnPath = "/checkout/return"

// onlinePaymentMethod is the Payment Method recorded on every Online Sale: the
// name of the Payment Provider that collects money at launch (the stub stands
// in for it in development, driving the same legs).
const onlinePaymentMethod = "payphone"

// CheckoutLineInput is one requested Ticket Type and quantity in a checkout.
type CheckoutLineInput struct {
	TicketTypeID string
	Quantity     int
}

// BeginCheckoutInput is a guest's request to start paying for tickets: the
// Event named by public slugs, the requested lines, and the checkout identity.
type BeginCheckoutInput struct {
	OrganizationSlug  string
	EventSlug         string
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	// CustomerTaxID is the Tax ID this purchase is to be declared under,
	// required on this native Sales Channel and already validated and normalised
	// by the handler (ADR 0016). Its SelfAsserted flag says the begin request
	// ran under this buyer's own Customer Session; it is snapshotted onto the
	// Payment because confirm, arriving on the provider's redirect, can no
	// longer establish it.
	CustomerTaxID platform.SaleTaxID
	Lines         []CheckoutLineInput
}

// BeginCheckoutResult is what the Storefront needs to send the Customer to the
// provider's payment page: our id for the attempt and where to redirect.
type BeginCheckoutResult struct {
	ClientTransactionID string `json:"client_transaction_id"`
	RedirectURL         string `json:"redirect_url"`
	AmountCents         int    `json:"amount_cents"`
	Currency            string `json:"currency"`
}

// BeginCheckout starts an online checkout: it validates the Event is published
// and the requested Ticket Types exist with capacity to spare, snapshots the
// current unit prices into a pending Payment, asks the Payment Provider to
// initiate, and returns the provider's redirect URL.
func (s *Service) BeginCheckout(ctx context.Context, in BeginCheckoutInput) (*BeginCheckoutResult, error) {
	orgSlug := strings.ToLower(strings.TrimSpace(in.OrganizationSlug))
	eventSlug := strings.ToLower(strings.TrimSpace(in.EventSlug))

	event, err := s.repo.GetCheckoutEvent(ctx, orgSlug, eventSlug)
	if err != nil {
		return nil, err
	}
	// A draft or cancelled Event is not sellable and, like the public event
	// page, not acknowledged to exist: both report EVENT_NOT_FOUND.
	if event == nil || event.Status != "published" {
		return nil, sales.ErrEventNotFound()
	}

	now := s.now()
	cutoff := sales.HoldCutoff(now)

	// Lazy expiry (ADR 0013): pendings past the hold window get their status
	// flipped opportunistically here. Best-effort bookkeeping only — the
	// created_at cutoff below is what actually releases their holds — so a
	// failure is logged and ignored rather than blocking the checkout.
	if _, err := s.repo.ExpireStalePayments(ctx, event.ID, cutoff, now); err != nil {
		s.logger.Warn("capacity holds: lazy expiry failed; holds are still bounded by the cutoff query", "event_id", event.ID, "error", err)
	}

	types, err := s.repo.ListTicketTypesForImport(ctx, event.OrganizationID, event.ID)
	if err != nil {
		return nil, err
	}
	held, err := s.repo.LiveCapacityHolds(ctx, event.ID, cutoff)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]repository.ImportTicketType, len(types))
	for _, tt := range types {
		byID[tt.ID] = tt
	}

	// Aggregate quantities per Ticket Type (a cart may repeat one) so the
	// capacity check sees the request whole.
	requested := map[string]int{}
	var order []string
	for _, line := range in.Lines {
		tt, ok := byID[line.TicketTypeID]
		if !ok {
			return nil, sales.ErrTicketTypeNotFound(line.TicketTypeID)
		}
		if _, seen := requested[tt.ID]; !seen {
			order = append(order, tt.ID)
		}
		requested[tt.ID] += line.Quantity
	}

	// The Event's Fee Handling is read once, here, and frozen into the lines
	// below: a flip while this Payment is pending must not move what the
	// Customer is already paying (ADR 0014).
	handling := sales.FeeHandlingOrDefault(event.FeeHandling)

	// Capacity check: sold + held + requested may not exceed capacity, so a
	// request cannot claim tickets that live Capacity Holds already speak for.
	// The commit-time check under row locks remains authoritative.
	amountCents := 0
	paymentLines := make([]repository.PaymentLine, 0, len(order))
	for _, id := range order {
		tt := byID[id]
		available := tt.Capacity - tt.SoldCount - held[id]
		if requested[id] > available {
			return nil, sales.ErrCapacityExceeded(id, requested[id], available)
		}
		// Per unit, then multiplied: the Customer's total is exactly the price
		// they were quoted times the quantity, never a percentage of a total.
		fee := s.fees.SnapshotUnit(handling, tt.PriceCents)
		amountCents += requested[id] * fee.BuyerUnitPriceCents
		paymentLines = append(paymentLines, repository.PaymentLine{
			TicketTypeID: id,
			Quantity:     requested[id],
			Fee:          fee,
		})
	}

	clientTransactionID := uuid.NewString()
	if _, err := s.repo.CreatePayment(ctx, repository.CreatePaymentInput{
		EventID:             event.ID,
		OrganizationID:      event.OrganizationID,
		Provider:            s.provider.Name(),
		ClientTransactionID: clientTransactionID,
		AmountCents:         amountCents,
		CustomerEmail:       strings.TrimSpace(in.CustomerEmail),
		CustomerFirstName:   strings.TrimSpace(in.CustomerFirstName),
		CustomerLastName:    strings.TrimSpace(in.CustomerLastName),
		CustomerTaxID:       in.CustomerTaxID,
		Lines:               paymentLines,
		Now:                 now,
	}); err != nil {
		return nil, err
	}

	initiation, err := s.provider.Initiate(ctx, platform.PaymentInitiateInput{
		AmountCents:         amountCents,
		Currency:            event.Currency,
		ClientTransactionID: clientTransactionID,
		Reference:           event.Name,
		ResponseURL:         s.storefrontBaseURL + checkoutReturnPath,
	})
	if err != nil {
		// The pending Payment stays behind with no redirect ever handed out; it
		// lapses to expired like any abandoned checkout (ADR 0013).
		return nil, err
	}

	if err := s.repo.SetPaymentProviderTransactionID(ctx, clientTransactionID, initiation.ProviderPaymentID, now); err != nil {
		return nil, err
	}

	return &BeginCheckoutResult{
		ClientTransactionID: clientTransactionID,
		RedirectURL:         initiation.RedirectURL,
		AmountCents:         amountCents,
		Currency:            event.Currency,
	}, nil
}

// ConfirmCheckoutResult is the settled outcome of a Payment: approved with the
// Sale Confirmation reference, or failed with none.
type ConfirmCheckoutResult struct {
	ClientTransactionID string `json:"client_transaction_id"`
	// Status is "approved" or "failed".
	Status          string `json:"status"`
	ConfirmationRef string `json:"confirmation_ref,omitempty"`
}

// ConfirmCheckout settles a Payment after the Customer returns from the
// provider's payment page. It is idempotent, keyed by our client transaction
// id: an already-settled Payment returns its recorded outcome without asking
// the provider anything or writing anything, so a refresh or back button never
// double-commits.
//
// On approval, the Ticket Sale is committed through the shared spine in the
// SAME transaction that marks the Payment approved, then the Sale Confirmation
// is emailed (outside the transaction, like every other Sales Channel). On
// decline the Payment ends failed and no sale exists. If the provider approves
// but the sale commit fails, the Payment is left approved WITHOUT a sale and
// the incident is logged loudly for the operator (parent spec decision 24).
func (s *Service) ConfirmCheckout(ctx context.Context, clientTransactionID string, providerParams map[string]string) (*ConfirmCheckoutResult, error) {
	payment, err := s.repo.GetPaymentByClientTransactionID(ctx, clientTransactionID)
	if err != nil {
		return nil, err
	}
	if payment == nil {
		return nil, sales.ErrPaymentNotFound()
	}
	// 'expired' is NOT settled: lazy expiry only released the Payment's
	// Capacity Hold, it passed no verdict on the money. When the Customer
	// returns from the provider after the window, the honest outcome is the
	// provider's — approved commits the sale if capacity still allows
	// (flipping expired → approved), declined records the failure. Only
	// 'approved' and 'failed' replay their recorded outcome (ADR 0013).
	if payment.Status != "pending" && payment.Status != "expired" {
		return s.recordedOutcome(payment)
	}

	confirmation, err := s.provider.Confirm(ctx, platform.PaymentConfirmInput{
		ClientTransactionID: clientTransactionID,
		Params:              providerParams,
	})
	if err != nil {
		// The Payment stays pending: the Customer can retry the confirm, and an
		// unconfirmed charge auto-reverses on the provider side (ADR 0012).
		return nil, err
	}

	if !confirmation.Approved {
		settled, err := s.repo.MarkPaymentFailed(ctx, clientTransactionID, confirmation.ProviderTransactionID, confirmation.Instrument, s.now())
		if err != nil {
			return nil, err
		}
		if !settled {
			// A racing confirm settled the Payment first; report what it recorded.
			return s.reloadOutcome(ctx, clientTransactionID)
		}
		return &ConfirmCheckoutResult{ClientTransactionID: clientTransactionID, Status: "failed"}, nil
	}

	ref, err := generateConfirmationRef()
	if err != nil {
		return nil, err
	}
	approved, err := s.repo.ApprovePaymentAndCommitSale(ctx, repository.ApprovePaymentInput{
		ClientTransactionID:   clientTransactionID,
		ProviderTransactionID: confirmation.ProviderTransactionID,
		Instrument:            confirmation.Instrument,
		PaymentMethod:         onlinePaymentMethod,
		ConfirmationRef:       ref,
		Now:                   s.now(),
		UpsertCustomer:        s.customers.UpsertForSale,
	})
	if err != nil {
		// The provider has the money and the sale could not be recorded — the one
		// incident the platform operator resolves by hand. The Payment is left
		// approved-without-sale as the durable marker, and the log line below is
		// the loud path the parent spec demands.
		if _, markErr := s.repo.MarkPaymentApprovedWithoutSale(ctx, clientTransactionID, confirmation.ProviderTransactionID, confirmation.Instrument, s.now()); markErr != nil {
			s.logger.Error("PAYMENT_APPROVED_WITHOUT_SALE: marking the incident failed too; reconcile from provider records",
				"client_transaction_id", clientTransactionID,
				"provider_transaction_id", confirmation.ProviderTransactionID,
				"commit_error", err,
				"mark_error", markErr,
			)
			return nil, sales.ErrPaymentSaleCommitFailed()
		}
		s.logger.Error("PAYMENT_APPROVED_WITHOUT_SALE: provider approved the charge but the Ticket Sale commit failed; resolve by hand",
			"client_transaction_id", clientTransactionID,
			"provider_transaction_id", confirmation.ProviderTransactionID,
			"error", err,
		)
		return nil, sales.ErrPaymentSaleCommitFailed()
	}
	if approved.AlreadySettled {
		return s.reloadOutcome(ctx, clientTransactionID)
	}

	// The Sale Confirmation goes out only after the transaction has committed,
	// exactly like the import channel: a Confirmation Link names a Ticket Sale by
	// its database id, and until commit there is no sale to name.
	event, ok, err := s.repo.GetEventImportContext(ctx, payment.OrganizationID, payment.EventID)
	if err == nil && ok {
		_ = s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
			To:               approved.Sale.CustomerEmail,
			CustomerName:     displayName(approved.Sale.CustomerFirstName, approved.Sale.CustomerLastName),
			EventName:        event.Name,
			Reference:        approved.Sale.ConfirmationRef,
			AmountCents:      approved.Sale.AmountCents,
			Currency:         event.Currency,
			ConfirmationLink: s.confirmationLink(approved.Sale.ID, event.End()),
			TaxID:            approved.Sale.CustomerTaxID,
		})
	}

	return &ConfirmCheckoutResult{
		ClientTransactionID: clientTransactionID,
		Status:              "approved",
		ConfirmationRef:     ref,
	}, nil
}

// recordedOutcome maps a settled Payment to the outcome its confirm recorded —
// the idempotent replay a refreshed or revisited return page receives.
func (s *Service) recordedOutcome(p *repository.Payment) (*ConfirmCheckoutResult, error) {
	switch p.Status {
	case "approved":
		if p.TicketSaleID == "" {
			// The approved-without-sale incident persists across re-confirms: there
			// is still no sale to report, and the Customer still needs support.
			return nil, sales.ErrPaymentSaleCommitFailed()
		}
		return &ConfirmCheckoutResult{
			ClientTransactionID: p.ClientTransactionID,
			Status:              "approved",
			ConfirmationRef:     p.ConfirmationRef,
		}, nil
	default: // failed ('expired' never reaches here: it is confirmable, not settled)
		return &ConfirmCheckoutResult{ClientTransactionID: p.ClientTransactionID, Status: "failed"}, nil
	}
}

// reloadOutcome re-reads a Payment a racing confirm settled first and reports
// the recorded outcome.
func (s *Service) reloadOutcome(ctx context.Context, clientTransactionID string) (*ConfirmCheckoutResult, error) {
	payment, err := s.repo.GetPaymentByClientTransactionID(ctx, clientTransactionID)
	if err != nil {
		return nil, err
	}
	if payment == nil {
		return nil, sales.ErrPaymentNotFound()
	}
	return s.recordedOutcome(payment)
}
