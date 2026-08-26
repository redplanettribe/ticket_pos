package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Sale Invoice Reissue (#483, ADR 0061): the one action on an
// authorized Sale Invoice. The body is the corrected Recipient and an
// optional note; who reissued comes from the session, and the email from
// the Sale — neither is accepted from the caller. The shape is refused here
// field by field, with the Tax ID under exactly the rules checkout applies,
// before the service is asked anything.

// reissueRecipientBody is the corrected Recipient as entered. No email.
type reissueRecipientBody struct {
	TaxIDType string `json:"tax_id_type"`
	TaxID     string `json:"tax_id"`
	LegalName string `json:"legal_name"`
	Address   string `json:"address"`
}

// reissueInvoiceBody is the reissue form as submitted.
type reissueInvoiceBody struct {
	Recipient reissueRecipientBody `json:"recipient"`
	Note      *string              `json:"note"`
}

// ReissueInvoice reissues an authorized Sale Invoice to a corrected Recipient.
//
// @Summary      Reissue a Sale Invoice to a corrected Recipient
// @Description  Corrects an authorized Sale Invoice whose Recipient is wrong (ADR 0061) by owing, in one transaction, a Credit Note for the full amount against it — the same Recipient as the factura, reason `reissue`, motivo "Corrección de los datos del receptor" — and a corrected Sale Invoice to the Recipient as entered: `recipient.tax_id_type` (`cedula`, `ruc` or `passport`), `recipient.tax_id` validated exactly as a checkout Tax ID with the same field-level errors, `recipient.legal_name` required, `recipient.address` optional. The corrected document's email is the one the Ticket Sale carries at that moment (after any Sale Re-addressing) and is never taken from the body; its lines, totals and IVA rate are the factura's; it names the factura it supersedes and records who reissued (the session's email), when, and the optional `note` (at most 500 characters). The superseded factura stays authorized and on file; the Sale, its buyer, its Tickets and the Customer's stored Tax ID are untouched. The Sale Invoice Drainer is kicked as after a checkout and works the Credit Note first; the corrected factura is signed only once that Credit Note is authorized. Answers 201 with the corrected document's detail, `owed` and unsigned. Refused with INVOICE_MANUAL_NOT_REISSUABLE (409) on a manual Tax Invoice, CREDIT_NOTE_NOT_REISSUABLE (409) on a Credit Note, INVOICE_NOT_AUTHORIZED (409, `details.status`) on a document that is owed, pending, needs_attention, withdrawn or annulled, INVOICE_SALE_REVERSED (409) when the Ticket Sale was reversed, REISSUE_IN_FLIGHT (409) while another reissue on the Sale has not settled, INVOICE_SUPERSEDED (409) on a factura already superseded, INVOICE_ALREADY_CREDITED (409) on one an authorized Credit Note already stands against, INVOICE_NOT_FOUND (404) otherwise. The decision is made under the Sale's lock, so a reissue racing a reversal is refused rather than double-written. Behind SALE_INVOICING_ENABLED: while the flag is closed this answers 404 SALE_INVOICING_UNAVAILABLE. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path  string              true  "Sale Invoice id"
// @Param        body  body  reissueInvoiceBody  true  "The corrected Recipient and an optional note"
// @Success      201  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/reissue [post]
func (h *Handler) ReissueInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	// Who reissued comes from the session and nowhere else: the trail
	// names the operator who acted, never one the caller named.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	var body reissueInvoiceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	input, fields := validateReissueInvoice(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	input.ReissuedBy = session.Email
	invoice, err := h.svc.ReissueSaleInvoice(r.Context(), id, input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, invoice)
}

// validateReissueInvoice checks the whole form at once and names every
// failing field. The Tax ID goes through the one rule every surface applies
// (platform.TaxIDFieldErrors), under the same field names the manual form
// uses; the note is bounded as an Operator Reversal's is.
func validateReissueInvoice(body reissueInvoiceBody) (service.ReissueInput, []platform.FieldError) {
	var fields []platform.FieldError
	var input service.ReissueInput

	r := body.Recipient
	input.Recipient = invoicing.Recipient{
		TaxIDType: strings.TrimSpace(r.TaxIDType),
		TaxID:     strings.TrimSpace(r.TaxID),
		LegalName: strings.TrimSpace(r.LegalName),
		Address:   strings.TrimSpace(r.Address),
	}
	switch {
	case input.Recipient.TaxIDType == "":
		fields = append(fields, required("recipient.tax_id_type"))
	case input.Recipient.TaxID == "":
		fields = append(fields, required("recipient.tax_id"))
	default:
		normalized, errs := platform.TaxIDFieldErrors("recipient.tax_id_type", "recipient.tax_id", input.Recipient.TaxIDType, input.Recipient.TaxID)
		fields = append(fields, errs...)
		input.Recipient.TaxID = normalized
	}
	fields = appendText(fields, "recipient.legal_name", input.Recipient.LegalName, true)
	fields = appendText(fields, "recipient.address", input.Recipient.Address, false)

	if body.Note != nil {
		if trimmed := strings.TrimSpace(*body.Note); trimmed != "" {
			if utf8.RuneCountInString(trimmed) > invoicing.MaxReissueNoteLength {
				fields = append(fields, platform.FieldError{
					Field:   "note",
					Code:    platform.CodeTooLong,
					Message: "must be at most " + strconv.Itoa(invoicing.MaxReissueNoteLength) + " characters",
				})
			}
			input.Note = trimmed
		}
	}
	return input, fields
}
