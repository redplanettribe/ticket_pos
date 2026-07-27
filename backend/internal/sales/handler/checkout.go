package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strings"

	"github.com/google/uuid"

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
	CustomerTaxIDType   string             `json:"customer_tax_id_type"`
	CustomerTaxIDNumber string             `json:"customer_tax_id_number"`
	Lines               []checkoutLineBody `json:"lines"`
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
// @Description  Starts a guest checkout on a published event: validates ticket types, quantities, and remaining capacity (check-only, no hold), snapshots current unit prices into a pending Payment, asks the Payment Provider to initiate, and returns our client transaction id with the provider's redirect URL. Guest checkout: no authentication is required, only an email, a name, and a valid Tax ID. A Customer Session presented in Authorization is optional and changes nothing about the sale — it only marks the Tax ID as the buyer's own assertion, which lets it replace their stored one.
// @Tags         public
// @Accept       json
// @Produce      json
// @Param        slug       path      string             true  "Organization slug"
// @Param        eventSlug  path      string             true  "Event slug"
// @Param        body       body      beginCheckoutBody  true  "Checkout lines, customer identity and Tax ID"
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
			{Field: "slug", Message: "organization and event slug are required"},
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
	input.CustomerTaxID.SelfAsserted = taxIDSelfAsserted(r, input.CustomerEmail)

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
		fields = append(fields, platform.FieldError{Field: "customer_email", Message: "is required"})
	} else if _, err := mail.ParseAddress(body.CustomerEmail); err != nil {
		fields = append(fields, platform.FieldError{Field: "customer_email", Message: "must be a valid email"})
	}
	if strings.TrimSpace(body.CustomerFirstName) == "" {
		fields = append(fields, platform.FieldError{Field: "customer_first_name", Message: "is required"})
	}
	if strings.TrimSpace(body.CustomerLastName) == "" {
		fields = append(fields, platform.FieldError{Field: "customer_last_name", Message: "is required"})
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
		fields = append(fields, platform.FieldError{Field: "customer_tax_id_type", Message: "is required"})
		if taxIDNumber == "" {
			fields = append(fields, platform.FieldError{Field: "customer_tax_id_number", Message: "is required"})
		}
	case taxIDNumber == "":
		fields = append(fields, platform.FieldError{Field: "customer_tax_id_number", Message: "is required"})
	default:
		normalized, taxIDFields := platform.TaxIDFieldErrors(
			"customer_tax_id_type", "customer_tax_id_number", taxIDType, taxIDNumber)
		if len(taxIDFields) > 0 {
			fields = append(fields, taxIDFields...)
		} else {
			taxID = platform.SaleTaxID{Type: taxIDType, Number: normalized}
		}
	}

	if len(body.Lines) == 0 {
		fields = append(fields, platform.FieldError{Field: "lines", Message: "must contain at least one line"})
	}
	if len(body.Lines) > maxCheckoutLines {
		fields = append(fields, platform.FieldError{Field: "lines", Message: fmt.Sprintf("must contain at most %d lines", maxCheckoutLines)})
	}

	lines := make([]service.CheckoutLineInput, 0, len(body.Lines))
	for i, line := range body.Lines {
		prefix := fmt.Sprintf("lines[%d].", i)
		ticketTypeID := strings.TrimSpace(line.TicketTypeID)
		if ticketTypeID == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Message: "is required"})
		} else if _, err := uuid.Parse(ticketTypeID); err != nil {
			// The column is UUID-typed; a malformed id would otherwise surface as a
			// database error rather than a VALIDATION_FAILED 400.
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Message: "must be a valid id"})
		}
		if line.Quantity <= 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "quantity", Message: "must be greater than zero"})
		}
		lines = append(lines, service.CheckoutLineInput{TicketTypeID: ticketTypeID, Quantity: line.Quantity})
	}

	return fields, service.BeginCheckoutInput{
		OrganizationSlug:  orgSlug,
		EventSlug:         eventSlug,
		CustomerEmail:     strings.TrimSpace(body.CustomerEmail),
		CustomerFirstName: strings.TrimSpace(body.CustomerFirstName),
		CustomerLastName:  strings.TrimSpace(body.CustomerLastName),
		CustomerTaxID:     taxID,
		Lines:             lines,
	}
}

// taxIDSelfAsserted reports whether this checkout is the buyer restating their
// own Tax ID: the request carries a full Customer Session and it belongs to the
// very email being bought under.
//
// Both halves matter. The session is what proves ownership of the address, so
// without one an anonymous visitor typing a known email could rewrite a
// stranger's profile; and the email must match, because a signed-in Customer
// buying tickets for a friend is supplying the friend's Tax ID, not their own
// (ADR 0016).
//
// A Confirmation Link session is deliberately not enough. It is minted from a
// token in a forwarded email rather than from Proof of Email Ownership, and it
// exists to show one Ticket Sale; letting it rewrite the profile behind it would
// hand that power to whoever the confirmation was forwarded to.
func taxIDSelfAsserted(r *http.Request, checkoutEmail string) bool {
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok || session.TicketSaleID != "" {
		return false
	}
	return session.Email == platform.NormalizeEmail(checkoutEmail)
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
			{Field: "clientTransactionId", Message: "is required"},
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
