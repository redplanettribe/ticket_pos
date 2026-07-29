package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
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

// onlinePaymentMethod is the Payment Method recorded on an Online Sale that
// collected money: the name of the Payment Provider that collected it at launch
// (the stub stands in for it in development, driving the same legs).
//
// It is the provider's own declared name rather than a second spelling of it,
// because the reversal rule reads this value back and asks the provider whether
// it settled the sale (ADR 0018). Two spellings drifting apart would mean an
// Undo silently withheld from every paid sale.
const onlinePaymentMethod = platform.PayPhoneProviderName

// freePaymentMethod is the Payment Method recorded on an Online Sale of Free
// Ticket Types, where there was nothing to collect and so no Payment Provider
// was ever asked (ADR 0017). It is also what the Payment names as its provider,
// because "which provider handled this" has an honest answer here — none did.
//
// The value itself belongs beside the Payment Provider boundary, because the
// question "can this sale's Payment be reversed?" is answered from the same
// vocabulary (ADR 0018) and must not be answered from a second spelling of it.
const freePaymentMethod = platform.FreePaymentMethod

// beginCheckoutStatus values report how the checkout was left. They are the
// Payment's own status, not a third vocabulary: a checkout with money to collect
// is left pending on the Payment Provider, and one with nothing to collect is
// already approved by the time the response is written (ADR 0017).
const (
	beginCheckoutPending  = "pending"
	beginCheckoutApproved = "approved"
)

// CheckoutLineInput is one requested Ticket Type and quantity in a checkout.
type CheckoutLineInput struct {
	TicketTypeID string
	Quantity     int
}

// BeginCheckoutInput is a guest's request to start paying for tickets: the
// Event named by public slugs, the requested lines, and who is buying.
type BeginCheckoutInput struct {
	OrganizationSlug string
	EventSlug        string
	// Customer is the buyer as the checkout form and the request itself describe
	// them, already validated and normalised by the handler: a Tax ID required on
	// this native Sales Channel (ADR 0016), a phone only when they typed one
	// (#106), and SelfAsserted set from the Customer Session the request carried.
	//
	// The whole of it is snapshotted onto the Payment, because confirm arrives on
	// the provider's return redirect and carries nothing of this form back — and
	// in the case of the session, can no longer establish it at all.
	Customer platform.SaleCustomer
	Lines    []CheckoutLineInput
	// AffiliateCodes are the Affiliate Link codes the buyer's recent clicks on
	// this Event left behind, NEWEST FIRST, forwarded by the Storefront from the
	// cookie it kept for the Attribution Window. Optional and untrusted: the
	// first that resolves to a live link is credited, and a history of unknown,
	// mistyped or deactivated codes simply records the sale unattributed. It
	// never refuses a checkout, and the buyer is never told which of the two
	// happened (#146).
	//
	// The order carries last-click attribution; liveness decides which click the
	// buyer's last LIVE click was, and only this side can answer that (ADR 0022).
	AffiliateCodes []string
}

// BeginCheckoutResult is what the Storefront needs to finish the checkout: our
// id for the attempt, how it was left, and where to send the Customer next.
//
// The two settlements are mutually exclusive and the empty field says which
// happened. A checkout with money to collect is left `pending` and carries the
// provider's RedirectURL; one with nothing to collect is already `approved` and
// carries the ConfirmationRef of the Ticket Sale it recorded, with no redirect
// because there is nowhere to send anybody (ADR 0017).
type BeginCheckoutResult struct {
	ClientTransactionID string `json:"client_transaction_id"`
	// Status is how the checkout was left: "pending" on the Payment Provider, or
	// "approved" when there was nothing to collect and it settled here.
	Status      string `json:"status" enums:"pending,approved"`
	RedirectURL string `json:"redirect_url,omitempty"`
	// ConfirmationRef is the Sale Confirmation reference, set only on an approved
	// checkout — the buyer has their tickets already and this is what they quote.
	ConfirmationRef string `json:"confirmation_ref,omitempty"`
	AmountCents     int    `json:"amount_cents"`
	Currency        string `json:"currency"`
}

// BeginCheckout starts an online checkout: it validates the Event is published
// and the requested Ticket Types exist with capacity to spare, snapshots the
// current unit prices into a pending Payment, asks the Payment Provider to
// initiate, and returns the provider's redirect URL.
func (s *Service) BeginCheckout(ctx context.Context, in BeginCheckoutInput) (*BeginCheckoutResult, error) {
	// The handler has already rejected a missing or invalid Tax ID with
	// field-level errors; this is the online channel's service-layer statement
	// of the ADR 0016 requirement, so no caller can begin a checkout without one.
	if err := sales.RequireTaxID("online", in.Customer.TaxID); err != nil {
		return nil, err
	}
	orgSlug := strings.ToLower(strings.TrimSpace(in.OrganizationSlug))
	eventSlug := strings.ToLower(strings.TrimSpace(in.EventSlug))

	// Trimmed once, on a copy, and used for both the Payment snapshot and the
	// provider prefill below — so the two can never disagree about who is buying,
	// and a buyer fact added to SaleCustomer later reaches both without being
	// listed again here.
	customer := in.Customer
	customer.Email = strings.TrimSpace(customer.Email)
	customer.FirstName = strings.TrimSpace(customer.FirstName)
	customer.LastName = strings.TrimSpace(customer.LastName)

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

	types, err := s.repo.ListEventTicketTypes(ctx, event.OrganizationID, event.ID)
	if err != nil {
		return nil, err
	}
	held, err := s.repo.LiveCapacityHolds(ctx, event.ID, cutoff)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]repository.EventTicketType, len(types))
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
		// What this Ticket Type costs right now: the Promotional Price while its
		// Promotion is live, the List Price otherwise (ADR 0021). The clock is
		// read once for the whole checkout, so a window closing mid-loop cannot
		// price two lines of one cart on different sides of it.
		//
		// This is the only place a Promotion is consulted in the whole sales
		// domain. From here on there is a base price and nothing else: the fee
		// arithmetic, the Payment's lines, the sale copied from them at confirm,
		// Net Proceeds and any later reversal all work off the snapshot below and
		// have no idea a Promotion was ever involved.
		baseCents := catalog.EffectiveBasePriceCents(tt.PriceCents, tt.Promotion, now)
		// Per unit, then multiplied: the Customer's total is exactly the price
		// they were quoted times the quantity, never a percentage of a total.
		fee := s.fees.SnapshotUnit(handling, baseCents)
		amountCents += requested[id] * fee.BuyerUnitPriceCents
		paymentLines = append(paymentLines, repository.PaymentLine{
			TicketTypeID: id,
			Quantity:     requested[id],
			Fee:          fee,
		})
	}

	// Which settlement this checkout gets is decided by the total and nothing
	// else. A cart of Free Ticket Types totals zero and is settled here, by the
	// platform itself; one paid ticket anywhere in it makes the whole checkout an
	// ordinary provider checkout (ADR 0017). The Payment records who handled it
	// either way, and for a free checkout the honest answer is that no Payment
	// Provider did.
	free := amountCents == 0
	provider := s.provider.Name()
	if free {
		provider = freePaymentMethod
	}

	// Affiliate Attribution is decided here, once, and snapshotted onto the
	// Payment: the sale-commit chokepoint copies it onto the Ticket Sale, so both
	// settlements below carry it without either of them knowing it exists. What
	// the code resolved to now is what the sale records, whatever happens to the
	// link afterwards — the same reasoning that freezes the prices above.
	affiliateLinkID := s.resolveAffiliateLink(ctx, event.ID, in.AffiliateCodes)

	clientTransactionID := uuid.NewString()
	if _, err := s.repo.CreatePayment(ctx, repository.CreatePaymentInput{
		AffiliateLinkID:     affiliateLinkID,
		EventID:             event.ID,
		OrganizationID:      event.OrganizationID,
		Provider:            provider,
		ClientTransactionID: clientTransactionID,
		AmountCents:         amountCents,
		Customer:            customer,
		Lines:               paymentLines,
		Now:                 now,
	}); err != nil {
		return nil, err
	}

	if free {
		return s.settleFreeCheckout(ctx, event, clientTransactionID, now)
	}

	initiation, err := s.provider.Initiate(ctx, platform.PaymentInitiateInput{
		AmountCents:         amountCents,
		Currency:            event.Currency,
		ClientTransactionID: clientTransactionID,
		Reference:           event.Name,
		ResponseURL:         s.storefrontBaseURL + checkoutReturnPath,
		// What the buyer has already typed on our screen, handed to the provider
		// so its payment page does not ask them for it a second time (#103). The
		// pair goes across whole: which Tax ID Types a provider's own
		// identification field can take is that provider's business, not this
		// service's (ADR 0012).
		//
		// The phone rides along only when the buyer typed one. Nothing here
		// substitutes a default for a blank: the field is optional, and a provider
		// asked to prefill a value nobody entered is exactly the fabricated
		// cardholder data PayPhone's rules prohibit (#103, #106).
		//
		// This is a narrowing of the buyer, not a copy of them: the prefill is
		// only what a hosted payment page could ask for, so the name and the
		// provenance of the buyer's own assertions stay on our side (#111).
		Customer: platform.PaymentCustomer{
			Email: customer.Email,
			Phone: customer.Phone,
			TaxID: customer.TaxID,
		},
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
		Status:              beginCheckoutPending,
		RedirectURL:         initiation.RedirectURL,
		AmountCents:         amountCents,
		Currency:            event.Currency,
	}, nil
}

// resolveAffiliateLink turns the click history a checkout arrived with into the
// id of the Affiliate Link to credit, or "" for none.
//
// The codes come newest click first and the FIRST that still names a live link
// wins: that is last-click attribution once liveness — the one part of the rule
// the Storefront cannot evaluate at click time (ADR 0022) — is applied. A code
// clicked after it but since deactivated resolves to nobody and falls through,
// so a dead click never costs a live link its credit (#142, #148).
//
// Nothing here can fail a checkout. No resolver wired, no codes, no live link,
// or a database that would not answer all end the same way: the sale is
// unattributed. A display-only statistic is not worth refusing a purchase over,
// so the error is logged and the checkout goes on (#146). The list is already
// bounded by the handler, so this is a handful of indexed lookups at most.
func (s *Service) resolveAffiliateLink(ctx context.Context, eventID string, codes []string) string {
	if s.affiliates == nil {
		return ""
	}
	for _, code := range codes {
		if strings.TrimSpace(code) == "" {
			continue
		}
		linkID, err := s.affiliates.ResolveLiveCode(ctx, eventID, code)
		if err != nil {
			s.logger.Warn("affiliate attribution: could not resolve a code; the sale is recorded unattributed",
				"event_id", eventID, "error", err)
			return ""
		}
		if linkID != "" {
			return linkID
		}
	}
	return ""
}

// settleFreeCheckout finishes a checkout that has nothing to collect: it
// approves the Payment just created and commits its Ticket Sale in the one
// place every Online Sale is recorded, so a free claim reaches the Sales list,
// the Customer Area and the Customer's own record by exactly the path a paid
// one does (ADR 0017).
//
// It runs in the begin-checkout request because there is nothing to wait for.
// No Payment Provider is asked, no redirect is handed out, and no confirm leg
// ever arrives — the buyer has their tickets by the time the response is
// written.
func (s *Service) settleFreeCheckout(ctx context.Context, event *repository.CheckoutEvent, clientTransactionID string, now time.Time) (*BeginCheckoutResult, error) {
	ref, err := generateConfirmationRef()
	if err != nil {
		return nil, err
	}

	approved, err := s.repo.ApprovePaymentAndCommitSale(ctx, repository.ApprovePaymentInput{
		ClientTransactionID: clientTransactionID,
		// No provider transaction id and no instrument: no provider was asked, and
		// nothing was charged to anything.
		PaymentMethod:   freePaymentMethod,
		ConfirmationRef: ref,
		Now:             now,
		UpsertCustomer:  s.customers.UpsertForSale,
	})
	if err != nil {
		// Nothing was collected, so there is no incident here — only a checkout
		// that did not happen, most often because capacity ran out under the
		// commit's own locks. What must not happen is the Payment lingering
		// `pending`: under ADR 0013 that would hold the very tickets nobody can
		// now claim, for a Payment that can never be settled. Marking it is
		// best-effort — the hold-window cutoff is still the backstop — and the
		// caller gets the commit's own error, so a capacity failure still reaches
		// the buyer as CAPACITY_EXCEEDED rather than as something generic.
		if _, markErr := s.repo.MarkPaymentFailed(ctx, clientTransactionID, "", "", now); markErr != nil {
			s.logger.Warn("free checkout: could not mark the unsettled Payment failed; its Capacity Hold lapses with the window",
				"client_transaction_id", clientTransactionID,
				"commit_error", err,
				"mark_error", markErr,
			)
		}
		return nil, err
	}
	if approved.Sale == nil {
		// AlreadySettled on a Payment this request created moments ago: nothing
		// can legitimately have settled it, so there is no recorded outcome worth
		// reloading and no sale to report. Logged loudly for the same reason
		// PAYMENT_APPROVED_WITHOUT_SALE is — an impossible state reached anyway is
		// worth more than the generic 500 the caller gets.
		s.logger.Error("FREE_CHECKOUT_NOT_SETTLED: a free Payment created in this request reported itself already settled; no Ticket Sale was recorded",
			"client_transaction_id", clientTransactionID,
		)
		return nil, fmt.Errorf("sales: free checkout %s was settled by nobody", clientTransactionID)
	}

	s.sendSaleConfirmation(ctx, event.OrganizationID, event.ID, approved.Sale)

	return &BeginCheckoutResult{
		ClientTransactionID: clientTransactionID,
		Status:              beginCheckoutApproved,
		ConfirmationRef:     approved.Sale.ConfirmationRef,
		AmountCents:         approved.Sale.AmountCents,
		Currency:            event.Currency,
	}, nil
}

// sendSaleConfirmation emails the receipt for a Ticket Sale that has just been
// committed, and is deliberately the only place either settlement does it.
//
// It goes out only after the transaction has committed, exactly like the import
// channel: a Confirmation Link names a Ticket Sale by its database id, and until
// commit there is no sale to name. A failure to send is swallowed — the sale is
// recorded and the buyer's tickets do not depend on the email arriving.
func (s *Service) sendSaleConfirmation(ctx context.Context, organizationID, eventID string, sale *repository.RecordedSale) {
	event, ok, err := s.repo.GetEventImportContext(ctx, organizationID, eventID)
	if err != nil || !ok {
		return
	}
	_ = s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
		To:               sale.CustomerEmail,
		CustomerName:     displayName(sale.CustomerFirstName, sale.CustomerLastName),
		EventName:        event.Name,
		Reference:        sale.ConfirmationRef,
		AmountCents:      sale.AmountCents,
		Currency:         event.Currency,
		ConfirmationLink: s.confirmationLink(sale.ID, event.End()),
		TaxID:            sale.CustomerTaxID,
	})
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

	s.sendSaleConfirmation(ctx, payment.OrganizationID, payment.EventID, approved.Sale)

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
