package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/consent"
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
//
// The Purchase Limit is counted the same two ways — the buyer's active Ticket
// Sales plus their live Capacity Holds — but only at begin, and never again at
// commit (ADR 0025). See refusePurchaseLimitBreach.

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

// CheckoutAnswerInput is one Answer the buyer gave on the checkout's answer
// section: which Ticket Type, which of that Ticket Type's tickets, which
// question, and the reply (#311).
//
// It is this module's own input shape rather than catalog.SubmittedCheckoutAnswer
// passed through from the handler, for CheckoutLineInput's reason: the handler
// speaks to the service in the service's vocabulary, and the domain rule's shape
// is a detail of how the service satisfies the request. `submittedAnswer` below
// is the one place the two are joined.
//
// EVERY REPLY SLOT IS A POINTER AND ABSENCE IS MEANINGFUL. `false` sent to a
// checkbox is an answer — somebody read the question and said no — while a nil
// Checked is the absence of one; a bool alone could not tell those apart. Number
// is a string so the digits reach a NUMERIC column exactly as they were typed.
type CheckoutAnswerInput struct {
	TicketTypeID string
	// TicketIndex is one-based, 1..quantity, counted across the cart's whole
	// holding of that Ticket Type — and it becomes the minted Ticket's ordinal.
	TicketIndex      int
	TicketQuestionID string
	Text             *string
	Number           *string
	Date             *string
	Checked          *bool
	OptionIDs        []string
}

// submittedAnswers translates the checkout's input shape into the domain's.
//
// A translation and nothing more: it drops nothing, checks nothing and cannot
// fail. Every judgement about these answers is made once, in
// catalog.HoldableCheckoutAnswers, against a cart only that call knows.
func submittedAnswers(in []CheckoutAnswerInput) []catalog.SubmittedCheckoutAnswer {
	if len(in) == 0 {
		return nil
	}
	submitted := make([]catalog.SubmittedCheckoutAnswer, 0, len(in))
	for _, answer := range in {
		submitted = append(submitted, catalog.SubmittedCheckoutAnswer{
			TicketTypeID:     answer.TicketTypeID,
			TicketIndex:      answer.TicketIndex,
			TicketQuestionID: answer.TicketQuestionID,
			Answer: catalog.SubmittedAnswer{
				Text:      answer.Text,
				Number:    answer.Number,
				Date:      answer.Date,
				Checked:   answer.Checked,
				OptionIDs: answer.OptionIDs,
			},
		})
	}
	return submitted
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
	// Locale is the language token of the Storefront page this checkout was
	// completed on, exactly as the request body carried it, and it becomes the
	// sale's Sale Locale (ADR 0033).
	//
	// Raw and untrusted, like the affiliate codes above and for the same reason:
	// it is read through platform.ParseLocale and DROPPED when it names a
	// language this platform does not write, leaving the sale with none. Nothing
	// about it can refuse a checkout. A receipt in the wrong language is a
	// disappointment; a purchase refused over one is a lost sale and a Customer
	// who cannot get into an Event.
	//
	// Empty from every caller with no page to name one, which is the ordinary
	// state of the other two Sales Channels: a box office sale and an import
	// record no language at all, and that is a different thing from recording
	// English (see migration 059).
	Locale string
	// Consent is what the buyer did with the three consent boxes on the checkout
	// dialog (#253, parent #249).
	//
	// PolicyAcceptance must be present and true or this checkout is refused below
	// — the required box is not a courtesy of the form — UNLESS this checkout is
	// a signed-in Customer's own and they have already accepted the current
	// Policy Version, in which case no such box was drawn and none is owed. The
	// optional two are recorded as given: ticked, unticked (an explicit No), or
	// nil for a box this buyer was not shown (#254).
	//
	// Every one of them is read only for a box the buyer was actually OWED, which
	// BeginCheckout recomputes rather than trusting the body about.
	Consent consent.Answers
	// SessionCustomerID is the Customer whose own Customer Session this checkout
	// is running under, and it is set ONLY where Customer.SelfAsserted is: a full
	// session, presented for the very address being bought under.
	//
	// The two travel together because they are one fact — "the person at the
	// keyboard is provably this Customer" — and the same fact decides both what a
	// buyer may overwrite on their own profile and which consent boxes they were
	// owed. A signed-in Customer buying for a friend supplies the friend's
	// details and the friend's consent: nothing is self-asserted, no id is set,
	// and the checkout is captured exactly as a guest's is.
	//
	// Empty on every guest checkout, on a Confirmation Link session (which proves
	// nothing about who holds it), and on the box office and import channels,
	// which never build one of these.
	SessionCustomerID string
	// Answers are what the buyer typed into the checkout's answer section: one
	// entry per (Ticket Type, ticket index, Ticket Question) they filled in
	// (#311, ADR 0044).
	//
	// SKIPPABLE IN EVERY SENSE. Empty is the ordinary case and says nothing is
	// wrong: the Tickets are minted with their questions outstanding, and the
	// holder answers by Answer Link afterwards. A required question left blank is
	// an Outstanding Answer and never a refusal.
	//
	// UNTRUSTED IN EVERY FIELD, and read only against what the server itself
	// knows the cart to be. An entry naming a Ticket Type not in the cart, a
	// question that Ticket Type does not ask, an index past its quantity, or a
	// reply of the wrong shape is DROPPED — see catalog.HoldableCheckoutAnswers,
	// which is why nothing here returns an error.
	//
	// IGNORED ENTIRELY WHILE THE FEATURE FLAG IS CLOSED. Nothing can be captured
	// before a Policy Version describes the collection (ADR 0045), and a body
	// carrying answers to a deployment that asks none is simply a body this
	// deployment has no questions for.
	Answers []CheckoutAnswerInput
	// ConsentEvidence is the technical proof of that act: the client IP as
	// platform.ClientIP derived it, the user agent, and the page it happened on.
	//
	// It comes from the REQUEST and never from the body, and the handler is what
	// enforces that — this service takes what it is given, exactly as it takes
	// Customer.SelfAsserted.
	ConsentEvidence consent.Evidence
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

// owedConsentAnswers narrows a checkout body's three answers to the boxes this
// buyer was actually OWED, and refuses the checkout when the one that gates it
// was owed and not given.
//
// WHO IS OWED WHAT. A guest is owed all three: nothing is known about who typed
// that address, so the dialog draws every box and every answer counts as given.
// A signed-in Customer buying under their own address is owed only what the
// consent module says they have not answered — which is what lets a Customer
// who accepted the current Policy Version and answered both optional boxes check
// out with no consent UI at all, exactly as they did before this feature existed
// (#254, parent spec user story 10).
//
// POLICY ACCEPTANCE GATES BEGIN, NOT CONFIRM, and the choice is the whole point
// of putting it here (#253, parent spec user story 8).
//
// Begin is the leg that hands a buyer to a Payment Provider. Refusing at confirm
// would mean the platform sent somebody's email, name, Tax ID and phone across a
// third-party boundary — processing them, in the guidance's sense — under a
// Privacy Policy nobody had accepted, and then declined the purchase after their
// card had been charged. That is the PAYMENT_APPROVED_WITHOUT_SALE incident,
// manufactured deliberately, over a checkbox. Nothing about consent may ever be a
// reason to refuse a Payment the provider has already approved, exactly as the
// Purchase Limit is checked once and never again (ADR 0025).
//
// It is refused in the SERVICE rather than as a handler-level field error,
// because it is a rule about whether the platform may act at all and not about
// whether the form is well-formed: a caller that is not the Storefront gets the
// same refusal, with the same code, from the same place the sign-in door uses
// (consent.ErrPolicyAcceptanceRequired). A free checkout is gated identically —
// it settles inside the same request (ADR 0017), and a ticket that costs nothing
// is still a Customer record created and a receipt emailed.
//
// RECOMPUTED, NEVER TRUSTED, which is the sign-in consent step's rule applied at
// the other capture surface (#251, service.SubmitConsent). The body arrives from
// a browser that was TOLD which boxes to draw, and this is the server asking the
// same question again at the moment of the write. An answer for a box this buyer
// was not owed is DROPPED rather than applied, so no crafted body can churn a
// standing Marketing or Networking Consent, and no client can manufacture
// evidence of a box it never showed. What that leaves, when a fully-answered
// Customer checks out, is three nil answers — and a capture with nothing in it
// writes no Consent Record at all (repository.ApprovePaymentAndCommitSale):
// evidence exists where a capture act happened, and no box was shown here.
func (s *Service) owedConsentAnswers(ctx context.Context, in BeginCheckoutInput) (consent.Answers, error) {
	// A guest owes every box, without asking anything: there is no Customer this
	// request has proven itself to be, and the address in the form is a claim.
	owed := consent.Outstanding{PolicyAcceptance: true, MarketingConsent: true, NetworkingConsent: true}
	if in.SessionCustomerID != "" {
		var err error
		if owed, err = s.consent.Outstanding(ctx, in.SessionCustomerID); err != nil {
			return consent.Answers{}, err
		}
	}

	var answers consent.Answers
	if owed.PolicyAcceptance {
		if in.Consent.PolicyAcceptance == nil || !*in.Consent.PolicyAcceptance {
			return consent.Answers{}, consent.ErrPolicyAcceptanceRequired()
		}
		answers.PolicyAcceptance = in.Consent.PolicyAcceptance
	}
	if owed.MarketingConsent {
		answers.MarketingConsent = in.Consent.MarketingConsent
	}
	if owed.NetworkingConsent {
		answers.NetworkingConsent = in.Consent.NetworkingConsent
	}
	return answers, nil
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

	// Wired at construction; nil would mean a deployment that can capture answers
	// and cannot record them. Refused rather than logged: a sale recorded without
	// its evidence is worse than a sale not made. Checked before the gate below,
	// which now needs it to ask what this buyer was owed.
	if s.consent == nil {
		return nil, fmt.Errorf("sales: no consent capturer wired; refusing to sell without an evidence log")
	}
	answers, err := s.owedConsentAnswers(ctx, in)
	if err != nil {
		return nil, err
	}
	in.Consent = answers
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

	// The Purchase Limit is enforced here, on the aggregated request, and NOWHERE
	// else — see refusePurchaseLimitBreach for why the commit path deliberately
	// does not re-check it.
	//
	// It runs BEFORE the capacity check below, so a request breaching both is
	// told about its own allowance rather than about the Event being full. That
	// order is deliberate: a Purchase Limit refusal is terminal for that buyer,
	// while CAPACITY_EXCEEDED names an `available` figure and invites a smaller
	// retry the limit would refuse just the same. Telling a Customer who already
	// holds their allowance that the Event is sold out is exactly the confusion
	// ADR 0025 gave this refusal its own code to avoid.
	if err := s.refusePurchaseLimitBreach(ctx, event.ID, byID, requested, order, customer.Email, cutoff); err != nil {
		return nil, err
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

	// The page's language, kept only if it is one this platform writes in
	// (ADR 0033). An unserved or malformed token leaves this empty, which records
	// no Sale Locale at all rather than asserting English on the buyer's behalf —
	// and the checkout carries on either way, which is the whole rule.
	saleLocale := ""
	if parsed, ok := platform.ParseLocale(in.Locale); ok {
		saleLocale = string(parsed)
	}

	clientTransactionID := uuid.NewString()
	paymentID, err := s.repo.CreatePayment(ctx, repository.CreatePaymentInput{
		AffiliateLinkID:     affiliateLinkID,
		Locale:              saleLocale,
		EventID:             event.ID,
		OrganizationID:      event.OrganizationID,
		Provider:            provider,
		ClientTransactionID: clientTransactionID,
		AmountCents:         amountCents,
		Customer:            customer,
		Lines:               paymentLines,
		// Held, not recorded: the Consent Record is written when the sale commits,
		// so an abandoned or declined Payment leaves no evidence — the same rule
		// that leaves it no Customer (migration 064, ADR 0035).
		Consent:         in.Consent,
		ConsentEvidence: in.ConsentEvidence,
		Now:             now,
	})
	if err != nil {
		return nil, err
	}

	// The answer section, captured onto the Payment the moment it exists and
	// BEFORE the free settlement below, which commits its Ticket Sale inside this
	// same request: a free checkout must carry its answers through by exactly the
	// path a paid one does, and the only way it can is if they are already held
	// when the commit runs.
	s.holdCheckoutAnswers(ctx, paymentID, requested, in.Answers, now)

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

// refusePurchaseLimitBreach refuses a checkout that would take the buyer past a
// requested Ticket Type's Purchase Limit: what they already hold plus what this
// cart asks for may not exceed it (ADR 0025).
//
// It is handed the request ALREADY AGGREGATED per Ticket Type, for the same
// reason the capacity check is: a cart naming one Ticket Type on three lines is
// one buyer asking for the sum, and judging the lines separately would let three
// requests of one slip past a limit of one.
//
// The buyer is resolved through the customers seam rather than by looking a row
// up here, because who an email is, is that module's rule (ADR 0010). A first
// checkout resolves to no Customer at all — the record is upserted only when a
// sale commits — and that buyer holds zero, which is not a special case but the
// ordinary one.
//
// Both reads are skipped entirely when no requested Ticket Type is rationed,
// which is nearly every checkout on the platform: an unrestricted Ticket Type is
// the state every one of them starts in, and an unconditional pair of queries
// would make the whole Event's checkout pay for a feature it does not use.
//
// It is checked HERE AND NOWHERE ELSE. The commit path re-checks capacity under
// row locks and deliberately does not re-check this: overselling a venue is a
// physical failure, while one Customer holding limit+1 after a pathological
// interleaving is harmless, and a refusal at commit would mean the Payment
// Provider has the buyer's money for a sale the platform then declines — the
// PAYMENT_APPROVED_WITHOUT_SALE incident a Platform Operator resolves by hand.
// The asymmetry is the decision, not an omission (ADR 0025).
func (s *Service) refusePurchaseLimitBreach(
	ctx context.Context,
	eventID string,
	byID map[string]repository.EventTicketType,
	requested map[string]int,
	order []string,
	email string,
	cutoff time.Time,
) error {
	rationed := false
	for _, id := range order {
		if byID[id].MaxPerCustomer != nil {
			rationed = true
			break
		}
	}
	if !rationed {
		return nil
	}

	held, err := s.customerEventHoldings(ctx, eventID, email, cutoff)
	if err != nil {
		return err
	}

	for _, id := range order {
		limit := byID[id].MaxPerCustomer
		if limit == nil {
			continue
		}
		if held[id]+requested[id] > *limit {
			return sales.ErrPurchaseLimitExceeded(id, *limit, held[id], requested[id])
		}
	}
	return nil
}

// CustomerEventHoldings reports how much of each of an Event's Ticket Types the
// person at this email already holds — their active Ticket Sales plus their live
// Capacity Holds, which is exactly what a Purchase Limit is measured against
// (ADR 0025). Ticket Types they hold none of are absent from the map.
//
// It exists for one caller outside this module: the public Event read, which
// tells a SIGNED-IN Customer their own holdings so the Storefront can bound its
// quantity picker before they type anything, and can say "you have yours"
// instead of "sold out" (#168). Catalog asks sales through this seam rather than
// reaching into the sales repository or restating the two-armed count in a
// second query of its own — one definition of "how many does this person hold"
// is what keeps the page and the refusal agreeing.
//
// THE EMAIL MUST BE ONE THE CALLER HAS PROVEN THE REQUESTER OWNS. Nothing here
// checks that and nothing here can: this is a service method, and the privacy
// property rests entirely on no route ever letting a caller name an address they
// have not proven. An endpoint that took an arbitrary email would be an oracle
// revealing whether a given address had bought a given Ticket Type, which
// ADR 0025 refuses outright.
//
// The hold window is read off this service's own clock, because a page read is
// its own moment and has no other cutoff to be consistent with — unlike
// begin-checkout, which shares one cutoff with its capacity check.
func (s *Service) CustomerEventHoldings(ctx context.Context, eventID, email string) (map[string]int, error) {
	return s.customerEventHoldings(ctx, eventID, email, sales.HoldCutoff(s.now()))
}

// customerEventHoldings is the count itself: resolve the email to a Customer
// through the customers seam — never lower-casing it here, since normalisation
// is that module's rule (ADR 0010) — then read both arms in the one query that
// defines them.
func (s *Service) customerEventHoldings(ctx context.Context, eventID, email string, cutoff time.Time) (map[string]int, error) {
	_, holdings, err := s.resolveCustomerEventHoldings(ctx, eventID, email, cutoff)
	return holdings, err
}

// resolveCustomerEventHoldings is the whole of that read, handing back the
// normalised email alongside the holdings.
//
// The Sale Import needs both: the holdings to judge the row, and the normalised
// address to key its running per-file tally on, so two spellings of one Customer
// in the same spreadsheet spend one allowance. Callers that only want the
// figures take customerEventHoldings above; nobody resolves an email and reads
// the two arms themselves, or begin-checkout's refusal and the import's could
// come to disagree about who a buyer is.
func (s *Service) resolveCustomerEventHoldings(
	ctx context.Context,
	eventID, email string,
	cutoff time.Time,
) (string, map[string]int, error) {
	customerID, normalizedEmail, err := s.customers.ResolveByEmail(ctx, email)
	if err != nil {
		return "", nil, err
	}
	holdings, err := s.repo.CustomerEventHoldings(ctx, eventID, repository.BuyerHoldings{
		CustomerID:      customerID,
		NormalizedEmail: normalizedEmail,
	}, cutoff)
	if err != nil {
		return "", nil, err
	}
	return normalizedEmail, holdings, nil
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

// holdCheckoutAnswers puts the buyer's answers onto the Payment they just
// created, keyed by (payment line, index) so they can land on the minted Tickets
// in order when the sale commits (#311, ADR 0044).
//
// IT RETURNS NOTHING, AND THAT IS THE WHOLE DESIGN OF IT. Every failure this
// function can meet — the flag being closed, a question read that errors, an
// INSERT that fails — resolves to the same outcome: the checkout proceeds and
// the Tickets carry Outstanding Answers. There is no error to return because
// there is no caller that could honourably do anything with one. ADR 0044 is
// unconditional: "nothing about them can refuse or delay a checkout", and a
// signature that cannot express a refusal is a stronger guarantee than a
// discipline about ignoring one.
//
// What is lost when this fails is a convenience, not the data: the buyer who
// knew four t-shirt sizes has to give them again through the Answer Link, which
// is the route the other three of four buyers take anyway. What would be lost by
// failing loudly is a sale.
//
// `requested` is the cart AGGREGATED per Ticket Type — the same map the capacity
// check and the Payment Lines were built from — because a Payment Line is one
// per Ticket Type, and the index counts against that line. A cart naming one
// Ticket Type on three lines is one line of the summed quantity, so the second
// ticket of that Ticket Type is index 2 whichever cart row the buyer added it
// from.
func (s *Service) holdCheckoutAnswers(
	ctx context.Context,
	paymentID string,
	requested map[string]int,
	submitted []CheckoutAnswerInput,
	now time.Time,
) {
	// The flag is read here rather than at the handler, so that EVERY route into
	// a checkout passes the same gate. Closed, this function is the only thing
	// this ticket adds to the request path and it does nothing at all — which is
	// what "byte-identical to today" means (ADR 0045).
	if !s.ticketQuestionsEnabled || len(submitted) == 0 {
		return
	}

	ticketTypeIDs := make([]string, 0, len(requested))
	for id := range requested {
		ticketTypeIDs = append(ticketTypeIDs, id)
	}
	asked, err := s.repo.ListCheckoutQuestions(ctx, ticketTypeIDs)
	if err != nil {
		s.logger.Warn("ticket questions: could not read the cart's questions; the checkout proceeds with its Answers outstanding",
			"payment_id", paymentID, "error", err)
		return
	}

	// The rule lives in the domain package and is reused, never restated: which
	// answers are keepable is the same question catalog.ParseAnswer settles for
	// Event Staff and for the Answer Link, differing here only in that a refusal
	// is a drop.
	held := catalog.HoldableCheckoutAnswers(asked, requested, submittedAnswers(submitted))
	if len(held) == 0 {
		return
	}
	if err := s.repo.HoldCheckoutAnswers(ctx, paymentID, held, now); err != nil {
		s.logger.Warn("ticket questions: could not hold the buyer's Answers on the Payment; the checkout proceeds with them outstanding",
			"payment_id", paymentID, "error", err)
	}
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
		CaptureConsent:  s.captureCheckoutConsent,
		SelfHeld:        s.ticketAssignmentEnabled,
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

// captureCheckoutConsent is the sale-commit spine's consent seam, bound to the
// consent service (#253).
//
// It is a method on the service rather than a closure at each call site so that
// both settlements — the provider confirm and the free checkout, which are the
// two places an Online Sale is recorded — reach the same write path with no
// chance of one being wired and the other forgotten. It adds nothing of its own:
// the sales module states what happened and discards the receipt, because what
// the answers MADE TRUE is the consent module's finding and nothing here has a
// use for it. The Sale Confirmation's own line about a Pending Confirmation is
// #255's, read from state at send time.
func (s *Service) captureCheckoutConsent(ctx context.Context, tx *sql.Tx, capture consent.Capture) error {
	_, err := s.consent.CaptureInTx(ctx, tx, capture)
	return err
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
		// Read from STATE, here, after the commit — which is the only place it
		// could be read. #253's capture discards its receipt deliberately (the
		// sales module states what happened and has no use for what it made true),
		// and the transaction that wrote the pending has to have committed before
		// anything outside it can observe one anyway.
		ConsentConfirmationLink: s.consentConfirmationLink(ctx, sale.CustomerID),
		// Read from state after the commit, like the consent line above and for
		// the same reason: the Tickets this asks about are minted by the very
		// transaction that had to commit before anything outside it could count
		// what they owe. A buyer who answered every question at checkout owes
		// nothing by the time this runs, and gets the receipt they always got.
		HasOutstandingAnswers: s.hasOutstandingAnswers(ctx, sale.ID),
		TaxID:                 sale.CustomerTaxID,
		Locale:                s.mailLocale(ctx, sale.ID, sale.Locale, sale.CustomerEmail),
	})
}

// consentConfirmationLink is the link the receipt carries when this buyer's
// address has an optional consent waiting to be confirmed, and "" when it does
// not — which is the great majority of receipts, and every receipt this platform
// sent before #255.
//
// IT IS ASKED ONLY BY THE ONLINE CHECKOUT, which is the only channel that
// captures consent at all. A box office sale and a Sale Import attest nothing on
// anybody's behalf (#249), so their receipts do not carry an offer to confirm
// something their buyer was never asked — even where that person happens to have
// a pending from some other checkout. Being prompted is the Storefront's job,
// and it happens at their next capture moment or on the receipt of the sale that
// actually asked.
//
// It degrades to "" on failure rather than propagating, exactly as
// confirmationLink does and for the same reason: the Ticket Sale is committed by
// the time this runs, and a receipt without a consent line is worth immeasurably
// more to the buyer than no receipt. The consequence is bounded — the Pending
// Confirmation stays pending, which sends nothing and is the safe direction —
// and it is re-offered at the owner's next capture moment (ADR 0035).
//
// A failure IS logged, unlike a Confirmation Link that could not be signed,
// because this one reads the database: an unsigned link means a misconfigured
// deployment that fails loudly elsewhere, while a persistent failure here would
// be a feature that had quietly stopped working.
func (s *Service) consentConfirmationLink(ctx context.Context, customerID string) string {
	if customerID == "" {
		return ""
	}
	link, err := s.customers.ConsentConfirmationLinkURL(ctx, customerID)
	if err != nil {
		s.logger.Warn("could not mint the consent confirmation link for a Sale Confirmation; the receipt goes out without it and the consent stays pending",
			"customer_id", customerID, "error", err)
		return ""
	}
	return link
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
		SelfHeld:              s.ticketAssignmentEnabled,
		CaptureConsent:        s.captureCheckoutConsent,
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
