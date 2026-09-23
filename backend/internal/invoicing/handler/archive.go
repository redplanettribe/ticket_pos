package handler

import (
	"net/http"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// DownloadTaxDocumentArchive streams the Tax Document Archive (#630, spec
// #629) for a range of Emission Dates, beside the single-document downloads.
//
// EVERY REFUSAL COMES BEFORE THE FIRST BYTE: the range, the session and the
// Issuer are all answered while the ordinary JSON envelope can still be sent.
// After that the status is committed, so a failure mid-stream aborts the
// connection (http.ErrAbortHandler) and the browser reports a failed download
// rather than saving a ZIP that ends early.
//
// @Summary      Download the Tax Document Archive for an Emission Date range
// @Description  Streams one ZIP, `application/zip` with `Content-Disposition: attachment; filename="comprobantes-<RUC>-<from>-<to>.zip"`, holding every document of the Ecuador Issuer - manual Tax Invoices, Sale Invoices (Superseded ones included) and Credit Notes - whose status is `authorized` and whose environment is `production`, with an Emission Date from `from` to `to`, BOTH ENDS INCLUDED, compared as calendar days in the Issuer's country. Each document is its stored signed XML, byte for byte what the single-document XML download serves, named `<clave de acceso>.xml` at the top level of the ZIP, ordered by Emission Date then clave. Pending, needs_attention, not_authorized, rejected, withdrawn, annulled, abandoned and owed documents are never in it, nor are test-environment documents. The LAST entry is a readme - `LEEME.txt` in Spanish or `README.txt` in English, following the requesting operator's Staff Locale and English when none is stated - naming the Issuer, the period and the moment it was generated, the count of facturas and of Credit Notes actually written, and the count of production documents emitted in the range that are still `pending` or `needs_attention`. An empty range is not an error: the ZIP holds the readme alone. The archive is built on the spot and streamed, with no document cap and no range limit, and the Tax Invoices list's other filters never apply. `from` and `to` are required `YYYY-MM-DD` days: a missing or malformed bound, or `from` after `to`, is refused under VALIDATION_FAILED exactly as the list's `issued_from`/`issued_to` are. ISSUER_NOT_FOUND (404) when no Ecuador Issuer has been recorded. Every archive taken writes one log line naming the operator, the range and the counts. Platform Operator only. The response carries `Cache-Control: no-store`, since the archive is buyer personal and tax data.
// @Tags         operator
// @Produce      application/zip
// @Security     BearerAuth
// @Param        from  query  string  true  "First Emission Date of the range (YYYY-MM-DD, inclusive)"
// @Param        to    query  string  true  "Last Emission Date of the range (YYYY-MM-DD, inclusive)"
// @Success      200  {file}  binary
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/archive [get]
func (h *Handler) DownloadTaxDocumentArchive(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	from, to, fields := archiveRange.parse(query.Get("from"), query.Get("to"))
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Who took the archive comes from the session: it is who the log line
	// names and whose Staff Locale the readme is written in.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	archive, err := h.svc.OpenTaxDocumentArchive(r.Context(), session.Email, from, to)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	w.Header().Set("Content-Type", service.ContentTypeZIP)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+archive.Filename+"\"")
	platform.NoStore(w)
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	if err := archive.WriteTo(r.Context(), w); err != nil {
		// Already logged by the service. Aborting is the only honest answer
		// left once a 200 is on the wire.
		panic(http.ErrAbortHandler)
	}
}

// SummarizeTaxDocumentArchive tells the operator what the Tax Document
// Archive of a range would hold before they download it (#631, spec #629).
// It reads its range through the archive's own parser and counts through the
// archive's own definitions, so it refuses what the archive refuses and
// counts what the archive holds.
//
// @Summary      Summarize the Tax Document Archive for an Emission Date range
// @Description  Returns what `GET /api/v1/operator/invoicing/archive` would hold for the same `from` and `to`, counted by the SAME definitions the archive streams by: `facturas` (manual Tax Invoices and Sale Invoices, Superseded ones included) and `credit_notes` of the Ecuador Issuer that are `authorized` in `production` with an Emission Date from `from` to `to`, BOTH ENDS INCLUDED; and `unsettled`, the production documents emitted in the range that are still `pending` or `needs_attention` — left out of the archive, not a reason to refuse it. An empty range is not an error: all three are zero. A document that settles between this read and the download changes the archive, not this answer. `from` and `to` are required `YYYY-MM-DD` days: a missing or malformed bound, or `from` after `to`, is refused under VALIDATION_FAILED exactly as the archive refuses them. ISSUER_NOT_FOUND (404) when no Ecuador Issuer has been recorded. Read-only and not logged. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        from  query  string  true  "First Emission Date of the range (YYYY-MM-DD, inclusive)"
// @Param        to    query  string  true  "Last Emission Date of the range (YYYY-MM-DD, inclusive)"
// @Success      200  {object}  openapi.EnvelopeTaxDocumentArchiveSummary
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/archive/summary [get]
func (h *Handler) SummarizeTaxDocumentArchive(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	from, to, fields := archiveRange.parse(query.Get("from"), query.Get("to"))
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	summary, err := h.svc.SummarizeTaxDocumentArchive(r.Context(), from, to)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, summary)
}
