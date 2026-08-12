package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/consent"
	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// Public checkout endpoints: guest checkout means no session requirement here.
// These are called by the Storefront BFF (nothing external calls the Go API
// directly, ADR 0008) on behalf of an anonymous Customer.

// maxCheckoutLines bounds a single checkout's line count. One Event per cart
// (ADR 0002) and a handful of Ticket Types per Event make anything beyond this
// a malformed request rather than a big basket.
const maxCheckoutLines = 50

// maxAffiliateCodes bounds how many remembered clicks a checkout is read for.
// The Storefront's cookie keeps three per Event (ADR 0022); five leaves room to
// widen that without an API change, and stops a hand-crafted request turning a
// display-only statistic into a list of lookups.
const maxAffiliateCodes = 5

// affiliateCodes normalizes the remembered click history: whitespace off, blanks
// out, and nothing past the accepted length read at all.
//
// Truncating rather than refusing is the rule the whole feature is built on —
// no ref, however absurd, may cost a buyer their purchase (#146).
func affiliateCodes(raw []string) []string {
	codes := make([]string, 0, len(raw))
	for _, code := range raw {
		if trimmed := strings.TrimSpace(code); trimmed != "" {
			codes = append(codes, trimmed)
		}
		if len(codes) == maxAffiliateCodes {
			break
		}
	}
	return codes
}

type checkoutLineBody struct {
	TicketTypeID string `json:"ticket_type_id"`
	Quantity     int    `json:"quantity"`
}

type beginCheckoutBody struct {
	CustomerEmail     string `json:"customer_email"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	// The buyer's Tax ID: a Tax ID Type ('cedula' | 'ruc' | 'passport') and its
	// number. Both are required — an Online Sale is a native Sales Channel and
	// must be declarable (ADR 0016).
	CustomerTaxIDType   string `json:"customer_tax_id_type"`
	CustomerTaxIDNumber string `json:"customer_tax_id_number"`
	// The buyer's phone number, OPTIONAL and never fabricated (#106). It exists
	// to be handed to the Payment Provider so its hosted card form arrives with
	// nothing left to type but the card; a buyer who omits it checks out exactly
	// as they did before the field existed. Absent, empty, or whitespace all mean
	// the same thing — no phone — and are not validation failures.
	CustomerPhone string `json:"customer_phone"`
	// AffiliateCodes are the Affiliate Link codes the Storefront remembered from
	// the buyer's recent clicks on this Event, NEWEST FIRST, OPTIONAL and never
	// validated as a field: the service credits the first that resolves to a live
	// link and records the sale unattributed when none does. A checkout is never
	// refused over a ref (#146).
	//
	// A list rather than one code because liveness is unknowable at click time
	// (ADR 0022): a click on a since-deactivated code must not erase the live
	// click before it. Anything past maxAffiliateCodes is dropped unread — a
	// browser sends at most three.
	AffiliateCodes []string           `json:"affiliate_codes"`
	Lines          []checkoutLineBody `json:"lines"`
	// Locale is the language of the Storefront page this checkout was completed
	// on, recorded as the sale's Sale Locale and read back whenever mail about
	// this sale is written (ADR 0033).
	//
	// It is a fact the page reports about itself — its language is in its own
	// address — and not a preference being negotiated: no read path takes an
	// Accept-Language and none is added here (ADR 0027).
	//
	// OPTIONAL AND NEVER A REASON TO REFUSE. A caller with no page to name one
	// omits it, and a language this platform does not serve is dropped. Either
	// way the sale records no language and the receipt falls back to what the
	// Customer's record remembers, then to English. A locale must never fail a
	// purchase — the money is the point of this request and the words are not.
	Locale string `json:"locale"`
	// The three consent boxes on the checkout dialog (#253, parent #249), named
	// identically to the sign-in consent submission's: one vocabulary for one set
	// of answers, so a client that learned the shape on one surface knows it on
	// the other.
	//
	// POINTERS, AND THE NIL IS LOAD-BEARING. Absent means THE BOX WAS NOT SHOWN,
	// which is a different fact from `false`; false means it was shown and left
	// unticked, which is an explicit No and is recorded as `denied` (ADR 0034).
	// A guest checkout always shows all three and therefore always sends all
	// three; a signed-in Customer is shown only what they have not answered
	// (#254), and reading their absent boxes as refusals would turn their
	// purchase into a marketing opt-out.
	//
	// PolicyAcceptance is REQUIRED to be present and true of everybody who is
	// still owed it — every guest, and every signed-in Customer without an
	// acceptance of the current Policy Version. It is not a field error but a
	// domain refusal — POLICY_ACCEPTANCE_REQUIRED from the service — because what
	// is wrong is not the shape of the request but that the platform may not act
	// on it (ADR 0035, consent.ErrPolicyAcceptanceRequired).
	//
	// WHICH BOXES WERE OWED IS THE SERVICE'S FINDING, not this body's assertion:
	// an answer for a box the buyer was not owed is dropped rather than applied
	// (service.owedConsentAnswers).
	PolicyAcceptance  *bool `json:"policy_acceptance"`
	MarketingConsent  *bool `json:"marketing_consent"`
	NetworkingConsent *bool `json:"networking_consent"`
}

type confirmCheckoutBody struct {
	// ProviderParams are the query params the provider's return redirect
	// carried, relayed verbatim by the Storefront return handler (for the stub:
	// {"outcome": "approved"|"declined"}).
	ProviderParams map[string]string `json:"provider_params"`
}

// BeginCheckout starts an online checkout for a published Event: it validates
// the request, snapshots prices into a pending Payment, and returns the Payment
// Provider's redirect URL.
//
// @Summary      Begin an online checkout
// @Description  Starts a guest checkout on a published event: validates ticket types, quantities, remaining capacity (check-only, no hold) and each Ticket Type's Purchase Limit, snapshots current unit prices into a Payment, and returns our client transaction id with how the checkout was left. A checkout with money to collect comes back status "pending" with the Payment Provider's redirect_url, exactly as before. A checkout whose cart totals zero — Free Ticket Types only — is settled here and now by the platform itself: no Payment Provider is contacted, the Ticket Sale is recorded and its Sale Confirmation sent before the response is written, and the result comes back status "approved" with confirmation_ref and no redirect_url (ADR 0017). One paid ticket anywhere in the cart makes the whole checkout a provider checkout. Refused with 409 PURCHASE_LIMIT_EXCEEDED when a requested Ticket Type carries a Purchase Limit and this buyer would end up holding more than it allows — details carry ticket_type_id, limit, already_held and requested. The allowance counts that Customer's active Ticket Sales plus their live Capacity Holds, so an abandoned checkout releases it and a Sale Reversal returns it; it is keyed on the Customer, and checked here only and never again when the sale commits, so a Payment the provider approved is never refused over it (ADR 0025). A cart breaching both its Purchase Limit and remaining capacity reports PURCHASE_LIMIT_EXCEEDED, because that refusal is terminal for this buyer while CAPACITY_EXCEEDED would invite a smaller retry the limit refuses just the same. Guest checkout: no authentication is required, only an email, a name, and a valid Tax ID — which is required for a free claim exactly as it is for a paid one. customer_phone is optional: supplied, it is recorded in canonical E.164 form and offered to the Payment Provider so its hosted payment page arrives prefilled; omitted, the checkout proceeds identically and nothing is sent in its place. affiliate_codes is optional and carries the Affiliate Link codes the buyer's recent clicks on this Event left behind, newest first: the first that matches one of this Event's live links credits the Ticket Sale, and a history of unknown, mistyped or deactivated codes simply records the sale unattributed — it never refuses a checkout. At most 5 codes are read; anything beyond is ignored. locale is optional and names the language of the Storefront page the checkout was completed on: it is recorded on the Ticket Sale and decides the language of the Sale Confirmation and of every later mail about that sale (ADR 0033). A checkout naming no locale, or one the platform does not serve, records none and still completes — the receipt then falls back to the Customer's remembered language, and to English. A Customer Session presented in Authorization is optional and changes nothing about the sale — it marks the buyer's details as their own assertion, which is what lets them replace the Tax ID and phone already stored on that Customer. Consent is captured here and refused here: policy_acceptance must be present and true from everybody who is still owed it — every guest, and every signed-in Customer with no acceptance of the current Policy Version — or the checkout is refused with 400 POLICY_ACCEPTANCE_REQUIRED and no Payment is created; the disabled button on the dialog is a courtesy, this is the guarantee. WHICH BOXES A BUYER WAS OWED IS RECOMPUTED HERE and never taken from the body: a guest is owed all three, and a checkout carrying the buyer's own Customer Session for the very address being bought under is owed only what that Customer has not answered (the same set the session read publishes as consent_boxes). An answer for a box that was not owed is DROPPED — so a signed-in Customer who has accepted the current edition and answered both optional boxes checks out with no consent fields at all and writes no Consent Record, and no crafted body can churn a standing Marketing or Networking Consent. It is enforced at BEGIN and never at confirm, so nobody is ever handed to a Payment Provider under a Privacy Policy they have not accepted, and no Payment the provider approved is ever refused over a checkbox. marketing_consent and networking_consent are optional and never blocking: sent true they are a grant, sent false they are an explicit No (which switches the weekly Follow Digest off, ADR 0034), and OMITTED means the box was not shown — which is not a No, and leaves any standing answer untouched. The answers are held on the Payment across the Payment Provider redirect, exactly as the Tax ID, phone and locale are, and the immutable Consent Record plus the consent state are written only when the sale commits: an abandoned, declined or expired Payment records no consent at all, just as it records no Customer. A guest has not proven the address they typed, so an optional tick from one enters Pending Confirmation — recorded as evidence, denied for sending, and never overwriting an answer given under a proven Customer Session (ADR 0035). The technical proof stored with the record (IP, user agent, origin URL) is taken from the request, never from this body. The Policy Version accepted is resolved server-side and is never accepted from a client.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        slug       path      string             true  "Organization slug"
// @Param        eventSlug  path      string             true  "Event slug"
// @Param        body       body      beginCheckoutBody  true  "Checkout lines, customer identity, Tax ID and optional phone"
// @Success      201        {object}  openapi.EnvelopeBeginCheckout
// @Failure      400        {object}  platform.Envelope
// @Failure      404        {object}  platform.Envelope
// @Failure      409        {object}  platform.Envelope
// @Router       /api/v1/public/organizations/{slug}/events/{eventSlug}/checkout [post]
func (h *Handler) BeginCheckout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	orgSlug := strings.TrimSpace(r.PathValue("slug"))
	eventSlug := strings.TrimSpace(r.PathValue("eventSlug"))
	if orgSlug == "" || eventSlug == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "slug", Code: platform.CodeRequired, Message: "organization and event slug are required"},
		})
		return
	}

	var body beginCheckoutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	fields, input := validateBeginCheckout(orgSlug, eventSlug, body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Set after validation rather than inside it: these are the facts about the
	// REQUEST rather than about the form, so they need the *http.Request that
	// validateBeginCheckout deliberately does not see.
	//
	// Both come out of ONE predicate, evaluated once: whether this checkout is the
	// buyer speaking about themselves. It decides what may be written back onto
	// their profile and which consent boxes they were owed, and the two must never
	// be able to disagree — a dialog that hid a box on the strength of a session
	// the write side then treats as a stranger's would collect nothing and refuse
	// the sale.
	input.SessionCustomerID, input.Customer.SelfAsserted = selfAssertedCheckout(r, input.Customer.Email)
	// The technical proof of the consent act, derived here and never taken from the
	// body — a body a client composes could claim any address and any browser, and
	// evidence of circumstances is worth keeping only in the sense that the
	// platform observed it. The IP comes from the platform's one agreed
	// derivation rather than from this handler reading the forwarding chain
	// itself; the user agent and origin are the browser's own headers as the
	// Storefront BFF relayed them (ADR 0008).
	//
	// No session id: this is the guest checkout, and the act it evidences is
	// finished by a redirect back from a Payment Provider that no session
	// outlives. What ties the evidence to what happened is the Ticket Sale.
	input.ConsentEvidence = consent.EvidenceFromRequest(r)

	result, err := h.svc.BeginCheckout(r.Context(), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, result)
}

// validateBeginCheckout applies handler-layer validation (shape, formats,
// obvious nonsense) so the service only ever sees a well-formed request.
// Business rules — event published, ticket types on the event, capacity — stay
// in the service.
func validateBeginCheckout(orgSlug, eventSlug string, body beginCheckoutBody) ([]platform.FieldError, service.BeginCheckoutInput) {
	var fields []platform.FieldError

	if strings.TrimSpace(body.CustomerEmail) == "" {
		fields = append(fields, platform.FieldError{Field: "customer_email", Code: platform.CodeRequired, Message: "is required"})
	} else if _, err := mail.ParseAddress(body.CustomerEmail); err != nil {
		fields = append(fields, platform.FieldError{Field: "customer_email", Code: platform.CodeInvalidEmail, Message: "must be a valid email"})
	}
	if strings.TrimSpace(body.CustomerFirstName) == "" {
		fields = append(fields, platform.FieldError{Field: "customer_first_name", Code: platform.CodeRequired, Message: "is required"})
	}
	if strings.TrimSpace(body.CustomerLastName) == "" {
		fields = append(fields, platform.FieldError{Field: "customer_last_name", Code: platform.CodeRequired, Message: "is required"})
	}

	// The Tax ID: required here because an Online Sale is a native Sales Channel
	// and the Organization must be able to declare it (ADR 0016). Validation is
	// platform.ValidateTaxID and nothing else — the clients mirror those rules
	// for instant feedback, this is the verdict — and its two sentinel errors
	// are mapped onto the halves of the pair they belong to, so the form can
	// mark the failing field rather than the whole group.
	taxIDType := strings.TrimSpace(body.CustomerTaxIDType)
	taxIDNumber := strings.TrimSpace(body.CustomerTaxIDNumber)
	var taxID platform.SaleTaxID
	switch {
	case taxIDType == "":
		fields = append(fields, platform.FieldError{Field: "customer_tax_id_type", Code: platform.CodeRequired, Message: "is required"})
		if taxIDNumber == "" {
			fields = append(fields, platform.FieldError{Field: "customer_tax_id_number", Code: platform.CodeRequired, Message: "is required"})
		}
	case taxIDNumber == "":
		fields = append(fields, platform.FieldError{Field: "customer_tax_id_number", Code: platform.CodeRequired, Message: "is required"})
	default:
		normalized, taxIDFields := platform.TaxIDFieldErrors(
			"customer_tax_id_type", "customer_tax_id_number", taxIDType, taxIDNumber)
		if len(taxIDFields) > 0 {
			fields = append(fields, taxIDFields...)
		} else {
			taxID = platform.SaleTaxID{Type: taxIDType, Number: normalized}
		}
	}

	// The phone number: OPTIONAL, and that is a product decision the validation
	// has to state carefully (#103, #106). A buyer who abandons at our dialog is
	// worth nothing while a buyer who types their number on PayPhone's own form
	// still completes the purchase, so an absent phone is simply no phone — never
	// an error, never a default, never a placeholder. A phone that IS supplied is
	// held to platform.ValidatePhone and nothing else, exactly as the Tax ID is
	// above: the clients mirror those rules for instant feedback, this is the
	// verdict, and what reaches the Payment is the canonical E.164 form it
	// returns.
	var phone string
	if supplied := strings.TrimSpace(body.CustomerPhone); supplied != "" {
		normalized, phoneFields := platform.PhoneFieldErrors("customer_phone", supplied)
		if len(phoneFields) > 0 {
			fields = append(fields, phoneFields...)
		} else {
			phone = normalized
		}
	}

	if len(body.Lines) == 0 {
		fields = append(fields, platform.FieldError{Field: "lines", Code: platform.CodeEmptyCollection, Message: "must contain at least one line"})
	}
	if len(body.Lines) > maxCheckoutLines {
		fields = append(fields, platform.FieldError{Field: "lines", Code: platform.CodeTooManyItems, Message: fmt.Sprintf("must contain at most %d lines", maxCheckoutLines)})
	}

	lines := make([]service.CheckoutLineInput, 0, len(body.Lines))
	for i, line := range body.Lines {
		prefix := fmt.Sprintf("lines[%d].", i)
		ticketTypeID := strings.TrimSpace(line.TicketTypeID)
		if ticketTypeID == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Code: platform.CodeRequired, Message: "is required"})
		} else if _, err := uuid.Parse(ticketTypeID); err != nil {
			// The column is UUID-typed; a malformed id would otherwise surface as a
			// database error rather than a VALIDATION_FAILED 400.
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Code: platform.CodeInvalidID, Message: "must be a valid id"})
		}
		if line.Quantity <= 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "quantity", Code: platform.CodeInvalidPositiveInt, Message: "must be greater than zero"})
		}
		lines = append(lines, service.CheckoutLineInput{TicketTypeID: ticketTypeID, Quantity: line.Quantity})
	}

	return fields, service.BeginCheckoutInput{
		OrganizationSlug: orgSlug,
		EventSlug:        eventSlug,
		// SelfAsserted is deliberately left unset here: it is a fact about the
		// request, not about the body, and BeginCheckout sets it above.
		Customer: platform.SaleCustomer{
			Email:     strings.TrimSpace(body.CustomerEmail),
			FirstName: strings.TrimSpace(body.CustomerFirstName),
			LastName:  strings.TrimSpace(body.CustomerLastName),
			TaxID:     taxID,
			Phone:     phone,
		},
		Lines: lines,
		// Passed through as typed, with no field error possible: what they resolve
		// to — a live Affiliate Link or nobody — is a business rule, and either
		// outcome is a successful checkout.
		AffiliateCodes: affiliateCodes(body.AffiliateCodes),
		// Passed through raw for the same reason, and never validated into a
		// field error: the service drops a language it cannot write in. Nothing
		// here restates platform.ParseLocale — one reader of a language token is
		// what keeps the checkout and the sign-in doors agreeing about what "es-EC"
		// means.
		Locale: body.Locale,
		// Relayed exactly as they arrived, nils and all: what a missing box means
		// is the consent module's rule, and the service refuses a checkout whose
		// required box is not a present true. Nothing here rewrites an absent
		// optional box into a false, which would be this handler answering on the
		// buyer's behalf.
		Consent: consent.Answers{
			PolicyAcceptance:  body.PolicyAcceptance,
			MarketingConsent:  body.MarketingConsent,
			NetworkingConsent: body.NetworkingConsent,
		},
		// ConsentEvidence is deliberately left unset here, exactly as SelfAsserted
		// is: it is a fact about the request, and BeginCheckout sets it above.
	}
}

// selfAssertedCheckout reports whether this checkout is the buyer speaking about
// themselves: the request carries a full Customer Session and it belongs to the
// very email being bought under.
//
// Both halves matter. The session is what proves ownership of the address, so
// without one an anonymous visitor typing a known email could rewrite a
// stranger's profile; and the email must match, because a signed-in Customer
// buying tickets for a friend is supplying the friend's details, not their own
// (ADR 0016).
//
// The verdict is about the checkout, not about any one field of it, which is why
// it is recorded on the buyer (platform.SaleCustomer.SelfAsserted) and guards
// every value the upsert may write back — the Tax ID and the phone today (#107),
// whatever the form collects on the buyer's own behalf next (#111).
//
// A Confirmation Link session is deliberately not enough. It is minted from a
// token in a forwarded email rather than from Proof of Email Ownership, and it
// exists to show one Ticket Sale; letting it rewrite the profile behind it would
// hand that power to whoever the confirmation was forwarded to.
// It returns the Customer as well as the verdict, because the two are one fact
// and a second reading of the session could disagree with the first. The id is
// empty exactly when the verdict is false, and it is what lets the service ask
// the consent module which boxes this buyer was owed (#254).
func selfAssertedCheckout(r *http.Request, checkoutEmail string) (string, bool) {
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok || session.TicketSaleID != "" {
		return "", false
	}
	if session.Email != platform.NormalizeEmail(checkoutEmail) {
		return "", false
	}
	return session.CustomerID, true
}

// ConfirmCheckout settles a Payment after the Customer returns from the
// provider's payment page, idempotently: re-confirming returns the recorded
// outcome without double-committing.
//
// @Summary      Confirm an online checkout
// @Description  Settles the Payment named by our client transaction id, relaying the provider's return-redirect params. Idempotent: an already-settled Payment returns its recorded outcome (approved with the Sale Confirmation reference, or failed) without asking the provider or writing anything, so a refreshed return page never double-commits. On first approval the Ticket Sale is committed in the same transaction that marks the Payment approved, and the Sale Confirmation email is sent.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        clientTransactionId  path      string               true   "Client transaction id from begin-checkout"
// @Param        body                 body      confirmCheckoutBody  false  "Provider return-redirect params"
// @Success      200                  {object}  openapi.EnvelopeConfirmCheckout
// @Failure      400                  {object}  platform.Envelope
// @Failure      404                  {object}  platform.Envelope
// @Failure      500                  {object}  platform.Envelope
// @Router       /api/v1/public/checkout/{clientTransactionId}/confirm [post]
func (h *Handler) ConfirmCheckout(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	clientTransactionID := strings.TrimSpace(r.PathValue("clientTransactionId"))
	if clientTransactionID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{
			{Field: "clientTransactionId", Code: platform.CodeRequired, Message: "is required"},
		})
		return
	}

	// The body is optional in shape but must be valid JSON when present; missing
	// provider params settle as a decline, never an approval (fail closed).
	var body confirmCheckoutBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	result, err := h.svc.ConfirmCheckout(r.Context(), clientTransactionID, body.ProviderParams)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
