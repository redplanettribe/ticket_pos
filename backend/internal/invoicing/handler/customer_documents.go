package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	customersmiddleware "github.com/peter/ticket_pos/backend/internal/customers/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The buyer's routes (#475, ADR 0060), served by this handler under the
// customer namespace exactly as the sales handler serves the undo there:
// the credential is a Customer Session, which is that namespace's business,
// and the document is a Tax Document, which is this module's. The Customer
// on the session is the only identity either route acts on; the ids in the
// path say WHICH of that Customer's Sales and documents, and the service
// narrows a Confirmation Link session to its one Sale.

// ListCustomerSaleDocuments lists a Sale's documents for its buyer.
//
// @Summary      List the Tax Documents of one of the Customer's Ticket Sales
// @Description  The Customer Area's read of a Sale's documents (ADR 0060): every Sale Invoice and Credit Note the Sale owes or was issued, each with its `kind` (`sale` for a factura, `credit_note`), a `status` in the platform's own words — `authorized`, or `on_its_way` for a document still owed, pending at the SRI or parked for an operator — and two download paths, `download_url` for the signed XML and `ride_url` for the RIDE (PDF), each set once authorized and null before (ADR 0062: the RIDE is rendered on request, so a document authorized before it existed carries one too). After a Sale Invoice Reissue (ADR 0061) the list holds the whole chain, and each document carries its `role` — `current` for the factura that stands, `superseded` for one a reissue corrected (still authorized and downloadable), `credit_note` — and the ids it points at: `supersedes_invoice_id`, `superseded_by_invoice_id`, `credits_invoice_id`, each another document of this list or null. Nothing the SRI said ever travels here. An empty list for a Sale that owes no document (a free, imported or non-House sale), for a Sale that is not this Customer's, and for any Sale but the one a Confirmation Link session names: not yours and not there are one answer. Requires a Customer Session; a Confirmation Link session is enough.
// @Tags         customer
// @Produce      json
// @Security     BearerAuth
// @Param        ticketSaleId  path  string  true  "Ticket Sale id"
// @Success      200  {object}  openapi.EnvelopeCustomerDocuments
// @Failure      401  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents [get]
func (h *Handler) ListCustomerSaleDocuments(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session")
		return
	}
	saleID := r.PathValue("ticketSaleId")
	if _, err := uuid.Parse(saleID); err != nil {
		_ = platform.WriteSuccess(w, reqID, http.StatusOK, []struct{}{})
		return
	}
	docs, err := h.svc.CustomerSaleDocuments(r.Context(), session.CustomerID, session.TicketSaleID, saleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, docs)
}

// DownloadCustomerSaleDocumentXML hands the buyer the signed XML.
//
// @Summary      Download the signed XML of one of the Customer's Tax Documents
// @Description  The buyer's download (ADR 0060): the factura or nota de crédito exactly as it was signed and authorized, as `application/xml` with `Content-Disposition: attachment; filename="<clave de acceso>.xml"` - the same bytes the operator route serves. Gated on the Sale: the Customer Session must own the Ticket Sale in the path, or be a Confirmation Link session naming it. Any other Customer, any other Sale, a document that is not this Sale's, and a document not yet authorized all answer INVOICE_NOT_FOUND (404), one refusal for every case so that ids cannot be probed. No session at all is 401. The response carries `Cache-Control: no-store`, since the document is buyer personal and tax data.
// @Tags         customer
// @Produce      application/xml
// @Security     BearerAuth
// @Param        ticketSaleId  path  string  true  "Ticket Sale id"
// @Param        id            path  string  true  "Tax Document id"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents/{id}/xml [get]
func (h *Handler) DownloadCustomerSaleDocumentXML(w http.ResponseWriter, r *http.Request) {
	h.serveCustomerDocument(w, r, h.svc.CustomerSaleDocumentXML)
}

// DownloadCustomerSaleDocumentRIDE hands the buyer the RIDE (#497, ADR 0062).
//
// @Summary      Download the RIDE (PDF) of one of the Customer's Tax Documents
// @Description  The buyer's download of the RIDE - the Representación Impresa del Documento Electrónico - of an authorized factura or nota de crédito, as `application/pdf` with `Content-Disposition: attachment; filename="<clave de acceso>.pdf"`: the same bytes the operator route serves and the delivery mail carries, rendered afresh from the stored document on every request and stored nowhere. A document authorized before the RIDE existed renders on its first request; nothing is re-mailed. Gated exactly as the XML download: the Customer Session must own the Ticket Sale in the path, or be a Confirmation Link session naming it. Any other Customer, any other Sale, a document that is not this Sale's, and a document not yet authorized all answer INVOICE_NOT_FOUND (404), one refusal for every case so that ids cannot be probed. No session at all is 401. The response carries `Cache-Control: no-store`, since the document is buyer personal and tax data.
// @Tags         customer
// @Produce      application/pdf
// @Security     BearerAuth
// @Param        ticketSaleId  path  string  true  "Ticket Sale id"
// @Param        id            path  string  true  "Tax Document id"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/customer/ticket-sales/{ticketSaleId}/tax-documents/{id}/ride [get]
func (h *Handler) DownloadCustomerSaleDocumentRIDE(w http.ResponseWriter, r *http.Request) {
	h.serveCustomerDocument(w, r, h.svc.CustomerSaleDocumentRIDE)
}

// serveCustomerDocument is the buyer's download, whichever file: the session
// is the identity, the two path ids say which of that Customer's Sales and
// documents, and an id that is not one answers INVOICE_NOT_FOUND before the
// service is asked, the same refusal it gives for everything else. A
// document leaves as a file under its own filename; a refusal is the
// ordinary JSON envelope, never an empty file.
func (h *Handler) serveCustomerDocument(w http.ResponseWriter, r *http.Request, load func(ctx context.Context, customerID, sessionTicketSaleID, ticketSaleID, invoiceID string) (*invoicing.Document, error)) {
	reqID := platform.RequestID(r.Context())
	session, ok := customersmiddleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session")
		return
	}
	saleID, id := r.PathValue("ticketSaleId"), r.PathValue("id")
	if _, err := uuid.Parse(saleID); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	doc, err := load(r.Context(), session.CustomerID, session.TicketSaleID, saleID, id)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+doc.Filename+"\"")
	platform.NoStore(w)
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Body)
}
