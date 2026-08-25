package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The downloads (#456): the signed XML and the SRI's authorization XML, each
// served as a file with a filename built from the clave. Refusals are the
// ordinary JSON envelope, never an empty file.

// DownloadSignedXML hands over the signed factura.
//
// @Summary      Download a Tax Invoice's signed XML
// @Description  Returns the factura exactly as it was signed and sent to the SRI — byte for byte the stored document — as `application/xml` with `Content-Disposition: attachment; filename="<clave de acceso>.xml"`. Available in every status, since the document exists from the moment the number was consumed. INVOICE_NOT_FOUND (404) otherwise. Platform Operator only.
// @Tags         operator
// @Produce      application/xml
// @Security     BearerAuth
// @Param        id  path  string  true  "Tax Invoice id"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/xml [get]
func (h *Handler) DownloadSignedXML(w http.ResponseWriter, r *http.Request) {
	h.serveDocument(w, r, h.svc.SignedXML)
}

// DownloadAuthorizationXML hands over the SRI's authorization.
//
// @Summary      Download a Tax Invoice's authorization XML
// @Description  Returns the SRI's `<autorizacion>` document for an authorized invoice — the legal proof: number, date, ambiente and the comprobante — exactly as the SRI returned it, as `application/xml` with `Content-Disposition: attachment; filename="<clave de acceso>-autorizacion.xml"`. An invoice that is pending, rejected or not authorized has no such document and answers AUTHORIZATION_XML_NOT_FOUND (404); INVOICE_NOT_FOUND (404) when there is no such invoice. Platform Operator only.
// @Tags         operator
// @Produce      application/xml
// @Security     BearerAuth
// @Param        id  path  string  true  "Tax Invoice id"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id}/authorization-xml [get]
func (h *Handler) DownloadAuthorizationXML(w http.ResponseWriter, r *http.Request) {
	h.serveDocument(w, r, h.svc.AuthorizationXML)
}

func (h *Handler) serveDocument(w http.ResponseWriter, r *http.Request, load func(context.Context, string) (*invoicing.Document, error)) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	doc, err := load(r.Context(), id)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+doc.Filename+"\"")
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(doc.Body)
}
