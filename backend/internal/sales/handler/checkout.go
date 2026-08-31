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

// The online checkout's shared machinery, and the Payment Provider's return leg.
//
// THERE IS NO GUEST BEGIN-CHECKOUT (ADR 0054, #386). What used to live here — a
// public POST that would address a Ticket Sale to whatever `customer_email` was
// typed into it — is gone, and the one begin-checkout left is the session-gated
// route in checkout_signed_in.go. The deletion rather than a refusal is the whole
// point: while the route and the field exist, addressing a Sale to an inbox
// nobody has proven is EXPRESSIBLE, and sooner or later something expresses it.
//
// What stays here is the request shape and the validation both halves always
// shared, plus ConfirmCheckout. Confirm is deliberately still public and still
// idempotent: it is the Payment Provider's return leg, driven by a browser that
// may have come back having lost its cookies, its session and its tab, and a
// gate on it would strand a buyer who has already paid.
//
// Everything here is called by the Storefront BFF; nothing external calls the Go
// API directly (ADR 0008).

// maxCheckoutLines bounds a single checkout's line count. One Event per cart
// (ADR 0002) and a handful of Ticket Types per Event make anything beyond this
// a malformed request rather than a big basket.
const maxCheckoutLines = 50

// maxCheckoutAnswers bounds how many Answers one checkout body is read for.
//
// It is a bound on WORK and not a rule about answering, which is why it
// truncates instead of refusing (affiliateCodes has the same shape for the same
// reason). The largest honest body is every ticket in a cart times every
// question its Ticket Type asks; 500 is far past any real cart and still small
// enough that a hand-crafted request cannot turn one checkout into thousands of
// INSERTs. A buyer who somehow exceeds it loses the tail of their answers and
// keeps their tickets, which is the trade this whole feature makes.
const maxCheckoutAnswers = 500

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

// checkoutAnswers turns the body's answer section into the service's vocabulary,
// trimming ids and reading no further than maxCheckoutAnswers.
//
// IT VALIDATES NOTHING AND REFUSES NOTHING. Whether an id names a real question,
// whether the index names a ticket in this cart, and whether the reply fits the
// question's kind are all things only the service can know — it is the party
// that resolved the cart — and all of them are DROPS rather than errors when the
// answer is no (catalog.HoldableCheckoutAnswers). A second copy of those rules
// here would be a second place for them to be enforced differently, and the more
// dangerous half of that is that this one sits where a field error is easy to
// return.
func checkoutAnswers(body []checkoutAnswerBody) []service.CheckoutAnswerInput {
	if len(body) == 0 {
		return nil
	}
	if len(body) > maxCheckoutAnswers {
		body = body[:maxCheckoutAnswers]
	}
	answers := make([]service.CheckoutAnswerInput, 0, len(body))
	for _, entry := range body {
		answers = append(answers, service.CheckoutAnswerInput{
			TicketTypeID:     strings.TrimSpace(entry.TicketTypeID),
			TicketIndex:      entry.TicketIndex,
			TicketQuestionID: strings.TrimSpace(entry.TicketQuestionID),
			Text:             entry.Text,
			Number:           entry.Number,
			Date:             entry.Date,
			Checked:          entry.Checked,
			OptionIDs:        entry.OptionIDs,
		})
	}
	return answers
}

type checkoutLineBody struct {
	TicketTypeID string `json:"ticket_type_id"`
	Quantity     int    `json:"quantity"`
}

// beginCheckoutBody is an online checkout as the server understands it, once the
// buyer's address has been supplied.
//
// IT IS NOT A WIRE FORMAT ANY MORE. Nothing decodes a request into this type; the
// only begin-checkout is beginCustomerCheckoutBody, which carries no address and
// converges here through addressedTo (checkout_signed_in.go). It survives as the
// one description of "an online checkout" that the validator and the service
// input are written against, so that shape, formats and cart rules are stated
// once.
//
// The doc comments on each field are the contract the session-gated body points
// at, which is why they stay here rather than being duplicated there.
type beginCheckoutBody struct {
	// CustomerEmail is the address the Ticket Sale is written to, and it is
	// EXPLICITLY NOT A FIELD ON THE WIRE — `json:"-"`, so no client can name it and
	// no generated client offers it. It is populated in exactly one place, from the
	// Customer Session (addressedTo), which is what makes "an online Sale is
	// addressed to a proven address" a property of the code rather than a
	// convention of a form (ADR 0054).
	CustomerEmail     string `json:"-"`
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
	//
	// ALL THREE ABSENT IS NOW THE ORDINARY CASE. Every buyer is a signed-in
	// Customer, shown only what they have not answered (#254), and a first-time
	// buyer answered everything at the sign-in that let them reach the dialog at
	// all — so the usual checkout sends no consent field whatsoever. Reading those
	// absences as refusals would turn a purchase into a marketing opt-out.
	//
	// PolicyAcceptance is REQUIRED to be present and true of everybody who is
	// still owed it, which since ADR 0054 means one person: a Customer holding a
	// live session when a new Policy Version is published under their feet. It is
	// not a field error but a
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
	// TermsAcceptance is the Términos y Condiciones box (#537, ADR 0066): the
	// checkout's OTHER required box, owed independently of the policy's and
	// only to a Customer whose session spans the current edition — ordinarily
	// nobody, because a sign-in since #536 settles it. Same pointer discipline:
	// absent means the box was not drawn, and an unowed answer is dropped.
	TermsAcceptance *bool `json:"terms_acceptance"`
	// Answers are what the buyer filled in on the checkout's answer section
	// (#311, ADR 0044).
	//
	// THE ONLY FIELD ON THIS BODY THAT CANNOT PRODUCE A FIELD ERROR, and the
	// asymmetry with everything above it is the point. A malformed email is a
	// form the buyer must fix; a malformed answer is a t-shirt size, and refusing
	// a purchase over one is the thing ADR 0044 exists to forbid. Anything
	// unusable here is DROPPED — quietly, by the service — and the Ticket carries
	// an Outstanding Answer instead.
	//
	// OPTIONAL AND USUALLY ABSENT. A cart whose Ticket Types ask nothing sends no
	// such key, and neither does a buyer who skipped the whole section, which is
	// explicitly a supported way to check out.
	Answers []checkoutAnswerBody `json:"answers"`
}

// checkoutAnswerBody is one Answer as the checkout form states it: which Ticket
// Type, which of that Ticket Type's tickets, which question, and the reply.
//
// FIVE OPTIONAL REPLY SLOTS AND EXACTLY ONE IS FILLED, decided by the question's
// KIND and not by the caller (catalog.SubmittedAnswer). They are pointers, and
// the nil is load-bearing on two of them in particular: `checked: false` is
// somebody who read the box and left it unticked, which is an answer, while an
// absent `checked` is somebody who was never asked; and an empty `option_ids`
// array is somebody clearing their choices, while an absent one says nothing
// about choices at all.
//
// `number` is a STRING and not a JSON number, deliberately. It lands in a
// NUMERIC column, and round-tripping it through a float64 is how `0.1` becomes
// `0.100000001` and how `3.50` loses the trailing zero a price or a measurement
// meant.
type checkoutAnswerBody struct {
	TicketTypeID string `json:"ticket_type_id"`
	// TicketIndex is which of that Ticket Type's tickets this Answer is about,
	// ONE-BASED: 1..quantity, counted across the whole cart's holding of that
	// Ticket Type. It becomes the minted Ticket's `ordinal` when the sale
	// commits, which is what makes "in order" mean anything (ADR 0043).
	TicketIndex      int      `json:"ticket_index"`
	TicketQuestionID string   `json:"ticket_question_id"`
	Text             *string  `json:"text"`
	Number           *string  `json:"number"`
	Date             *string  `json:"date"`
	Checked          *bool    `json:"checked"`
	OptionIDs        []string `json:"option_ids"`
}

type confirmCheckoutBody struct {
	// ProviderParams are the query params the provider's return redirect
	// carried, relayed verbatim by the Storefront return handler (for the stub:
	// {"outcome": "approved"|"declined"}).
	ProviderParams map[string]string `json:"provider_params"`
}

// validateBeginCheckout applies handler-layer validation (shape, formats,
// obvious nonsense) so the service only ever sees a well-formed request.
// Business rules — event published, ticket types on the event, capacity — stay
// in the service.
func validateBeginCheckout(orgSlug, eventSlug string, body beginCheckoutBody) ([]platform.FieldError, service.BeginCheckoutInput) {
	var fields []platform.FieldError

	// The address, which no caller supplied: it comes from the Customer Session,
	// so this branch is now a FAIL-CLOSED ASSERTION rather than form validation.
	// It cannot fire on a request that reached here through the session gate — a
	// session's email is an address this platform itself stored and proved — and
	// it stays because the alternative to an impossible refusal is selling
	// somebody tickets under an empty address if the wiring ever changes.
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
		// Relayed as typed, and — alone on this body — incapable of producing a
		// field error. The service reads them against the cart it resolved and
		// drops what does not fit; see checkoutAnswers.
		Answers: checkoutAnswers(body.Answers),
		// Relayed exactly as they arrived, nils and all: what a missing box means
		// is the consent module's rule, and the service refuses a checkout whose
		// required box is not a present true. Nothing here rewrites an absent
		// optional box into a false, which would be this handler answering on the
		// buyer's behalf.
		Consent: consent.Answers{
			PolicyAcceptance:  body.PolicyAcceptance,
			MarketingConsent:  body.MarketingConsent,
			NetworkingConsent: body.NetworkingConsent,
			TermsAcceptance:   body.TermsAcceptance,
		},
		// ConsentEvidence is deliberately left unset here, exactly as SelfAsserted
		// is: it is a fact about the request, and BeginCheckout sets it above.
	}
}

// selfAssertedCheckout reports whether this checkout is the buyer speaking about
// themselves: the request carries a full Customer Session and it belongs to the
// very email being bought under.
//
// IT IS CONSTANT TRUE ON THE ONE ROUTE THAT CALLS IT, and it is still asked.
// Since ADR 0054 there is no route on which a checkout's address can differ from
// its session's, so both halves below always hold — but SelfAsserted means "we
// checked", it varies on the staff channels and on every historical row, and a
// hardcoded true here would leave one column with two meanings depending on which
// handler wrote it. The constant is a consequence of the gate, not a shortcut
// past it.
//
// Both halves still matter, and this is why they are written down. The session is
// what proves ownership of the address; and the email must match, because
// supplying somebody else's address was how a signed-in Customer used to buy for
// a friend, and that is the very thing the deleted route made possible (ADR 0016).
//
// The verdict is about the checkout, not about any one field of it, which is why
// it is recorded on the buyer (platform.SaleCustomer.SelfAsserted) and guards
// every value the upsert may write back — the Tax ID and the phone today (#107),
// whatever the form collects on the buyer's own behalf next (#111).
//
// A Confirmation Link session is deliberately not enough. It is minted from a
// token in a forwarded email rather than from Proof of Email Ownership, and it
// exists to show one Ticket Sale; letting it rewrite the profile behind it would
// hand that power to whoever the confirmation was forwarded to. The handler
// refuses such a session outright before ever reaching here, so this is the
// second of two independent statements of the same rule.
//
// It returns the Customer as well as the verdict, because the two are one fact
// and a second reading of the session could disagree with the first. The id is
// empty exactly when the verdict is false, and it is what lets the service ask
// the consent module which boxes this buyer was owed (#254) — which, with no
// guest left to be owed everything, is now the service's ONLY way to answer that
// question.
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
