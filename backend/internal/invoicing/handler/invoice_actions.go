package handler

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Check status and Resend (#455): the two actions on a Tax Invoice the SRI
// has not authorized. Neither takes a body; the invoice id is the whole
// request, and the answer is the invoice as it stands afterwards.

// CheckInvoice asks the SRI again about a Tax Invoice.
//
// @Summary      Check a Tax Invoice's status with the SRI
// @Description  Asks the SRI's autorización service again about a Tax Invoice that is `pending`, `rejected` or `not_authorized`, and updates it from the answer: `AUTORIZADO` stores the authorization number, date and XML; `NO AUTORIZADO` stores the SRI's messages; an answer that decides nothing (still in processing, or nothing known under the clave) leaves the status as it was. Nothing is sent. Exactly one attempts row is written. INVOICE_ALREADY_AUTHORIZED (409) on an authorized invoice; INVOICE_ANNULLED (409) on one marked annulled; INVOICE_WITHDRAWN (409) on a withdrawn Sale Invoice or Credit Note, which was never sent; INVOICE_NOT_ISSUED (409) on one still owed and unsigned; INVOICE_NOT_FOUND (404) otherwise. Returns the invoice as it then stands, with `check_status_hint` true when it is pending and the SRI holds it. A Sale Invoice or Credit Note this check finds authorized is made due for the Sale Invoice Drainer to deliver. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Tax Invoice id"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/check [post]
func (h *Handler) CheckInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	invoice, err := h.svc.CheckInvoice(r.Context(), id)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}

// ResendInvoice resubmits a Tax Invoice to the SRI under the same clave.
//
// @Summary      Resend a Tax Invoice to the SRI
// @Description  Rebuilds the factura of a `pending`, `rejected` or `not_authorized` Tax Invoice from its recorded Recipient, lines and fields — with the Issuer's editable details as they now stand, under the SAME clave de acceso and secuencial — re-signs it with the certificate now in custody, submits it to recepción and polls autorización as an issue does. The signed XML on file is replaced by the re-signed bytes only when the SRI answered `RECIBIDA`, so the artifact the platform holds is always the one the SRI holds. SRI errors 43 (clave already registered) and 70 (in processing) mean the SRI has it: the invoice is `pending` with `check_status_hint` true and the messages kept, never an error. One attempts row per SRI call. INVOICE_ALREADY_AUTHORIZED (409) on an authorized invoice; INVOICE_ANNULLED (409) on one marked annulled; INVOICE_WITHDRAWN (409) on a withdrawn Sale Invoice or Credit Note; INVOICE_NOT_ISSUED (409) on one still owed and unsigned; INVOICE_NOT_FOUND (404); ISSUER_NOT_FOUND (404), CERTIFICATE_NOT_UPLOADED (409), CERTIFICATE_KEY_NOT_CONFIGURED (503) and ISSUER_INCOMPLETE (409) before anything is sent. Returns the invoice as it then stands. A Sale Invoice or Credit Note the resend gets authorized is delivered by the Sale Invoice Drainer on its next round. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Tax Invoice id"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/resend [post]
func (h *Handler) ResendInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	invoice, err := h.svc.ResendInvoice(r.Context(), id)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}
