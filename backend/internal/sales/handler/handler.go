// Package handler exposes HTTP endpoints for the sales domain.
// Sales covers online, in-person, and import sales plus capacity logic.
package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

// maxUploadBytes caps an uploaded Sale Import file at 32 MiB, comfortably above
// the ~10,000-row limit while bounding memory.
const maxUploadBytes = 32 << 20

// Handler exposes HTTP endpoints for the sales domain.
type Handler struct {
	svc *service.Service
}

// New returns a sales HTTP handler.
func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

type importSaleRow struct {
	CustomerEmail     string `json:"customer_email"`
	CustomerFirstName string `json:"customer_first_name"`
	CustomerLastName  string `json:"customer_last_name"`
	TicketTypeID      string `json:"ticket_type_id"`
	Quantity          int    `json:"quantity"`
	PaymentMethod     string `json:"payment_method"`
	SoldAt            string `json:"sold_at"`
	AmountCents       *int   `json:"amount_cents"`
}

type commitImportBody struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Source         string          `json:"source"`
	Sales          []importSaleRow `json:"sales"`
}

func actorFromRequest(r *http.Request) service.ActorContext {
	member, _ := middleware.ActiveMemberFromContext(r.Context())
	return service.ActorContext{
		MemberID:       member.MemberID,
		OrganizationID: member.OrganizationID,
		Email:          member.Email,
	}
}

// DownloadSaleImportTemplate returns a per-event .xlsx Sale Import template.
//
// @Summary      Download the Sale Import template
// @Description  Returns a per-event .xlsx pre-listing the Event's Ticket Types as a locked dropdown, with the internal ticket type id in a hidden reference column. Columns: customer_email, customer_first_name, customer_last_name, ticket_type, quantity, payment_method, sold_at, amount, plus the optional pair customer_tax_id_type (cedula|ruc|passport) and customer_tax_id_number.
// @Tags         staff
// @Produce      application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports/template [get]
func (h *Handler) DownloadSaleImportTemplate(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	data, eventName, err := h.svc.BuildTemplate(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	filename := "sale-import-" + slugFilename(eventName) + ".xlsx"
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// PreviewSaleImport parses an uploaded .csv/.xlsx and returns a per-row
// validation result plus the capacity impact, writing nothing.
//
// @Summary      Preview a Sale Import file
// @Description  Parses an uploaded .csv/.xlsx server-side and returns every row's validation result at once, the matched Ticket Type, the normalised Tax ID when the row supplies one (the customer_tax_id_type/customer_tax_id_number columns are optional; a present-but-invalid value is a row error naming the failing column), the capacity impact per Ticket Type (with per-type oversell overage), soft possible-duplicate flags per row, and a top-level committable flag (false when any row is invalid or any Ticket Type is oversold). A row that would take one customer past a ticket type's max_per_customer is invalid, with a blocking error on its quantity cell; rows in the same file count against each other, and the complaint names the earlier row when that is what the row conflicts with. No writes.
// @Tags         staff
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string  true  "Event ID"
// @Param        file  formData  file    true  "Sale import file (.csv or .xlsx)"
// @Success      200   {object}  platform.Envelope
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports/preview [post]
func (h *Handler) PreviewSaleImport(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	// Ahead of the upload, for the reason the commit checks ahead of the body:
	// there is nothing to preview against an Event that sells no tickets, and
	// the file's own problems are beside the point (issue #212).
	if err := h.svc.EnsureEventSellsTickets(r.Context(), actorFromRequest(r), eventID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	rows, ok := h.parseUploadedFile(w, r, reqID)
	if !ok {
		return
	}

	result, err := h.svc.PreviewImport(r.Context(), actorFromRequest(r), eventID, rows)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// CommitDirectSaleImport records a batch of Direct Sales for an Event. The batch
// is supplied either as multipart/form-data (a .csv/.xlsx `file` plus
// `idempotency_key` and `source` fields) or as a JSON body.
//
// @Summary      Commit a Direct Sale Import
// @Description  Records off-platform (cash/transfer) sales against an Event, decrementing capacity and emailing each customer a Sale Confirmation. All-or-nothing and idempotent. Accepts an uploaded .csv/.xlsx file (with optional `skip_rows`, a comma-separated list of file row numbers to exclude, e.g. resolved duplicates) or a JSON body. The file form re-runs the preview's max_per_customer check rather than trusting that a preview ran, so a row over a ticket type's limit fails the batch with VALIDATION_FAILED. The JSON form does NOT check it, and no parity should be inferred: it carries no per-row complaint channel to report a refusal through, and a Purchase Limit is a guardrail an Organization sets for itself — the same Org Admin may clear the limit, import, and set it back, which is the documented way to import history recorded before the limit existed (ADR 0025). An Event that registers externally sells no tickets on this platform, so every form of this call — both Sales Sources, file and JSON alike — is refused with EVENT_IS_EXTERNAL_REGISTRATION before the body is judged and before any Ticket Type is resolved: the refusal names the mode rather than a Ticket Type that does not exist and never will (ADR 0028). Each row's Tickets are minted one per unit and the row's BUYER holds the first of them as a Self-held Ticket — assigned to them and `accepted` in the same transaction, so the Holder List names them (ADR 0055). The warrant is the transcription rather than a proof: a Sale Import records a transaction that already happened, so the email column is the person who bought, and the buyer is PRESUMED to attend. It makes nobody Verified, sends no Assignment mail, and the buyer's remedy for a wrong presumption is reassignment, exactly as an online buyer's is. There is deliberately NO opt-out column on the template. Nothing is written while TICKET_ASSIGNMENT_ENABLED is off.
// @Tags         staff
// @Accept       json
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string            true  "Event ID"
// @Param        body  body      commitImportBody  false "Sale import batch (JSON form)"
// @Success      201   {object}  platform.Envelope
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports [post]
func (h *Handler) CommitDirectSaleImport(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	// Before the body is read at all: an externally registered Event sells no
	// tickets, so no batch it could be carrying is importable, and telling the
	// caller to fix their Sales Source or their ticket_type_id would be sending
	// them after a problem that is not there (ADR 0028, issue #212). The service
	// enforces the same invariant on the way in; this is what makes the refusal
	// beat the body's own validation.
	if err := h.svc.EnsureEventSellsTickets(r.Context(), actorFromRequest(r), eventID); err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	if isMultipart(r) {
		h.commitFromFile(w, r, reqID, eventID)
		return
	}
	h.commitFromJSON(w, r, reqID, eventID)
}

// ListSaleImports returns the Event's committed Sale Import batches, newest
// first, for the per-event import history surface.
//
// @Summary      List Sale Import history
// @Description  Returns the Event's Sale Import batches (newest first) for the per-event history: batch id, created_at, sale_count, source, status, and the acting Member's id/email. Read-only.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports [get]
func (h *Handler) ListSaleImports(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	entries, err := h.svc.ListImportHistory(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, entries)
}

// Sales list pagination bounds (ADR-0006): page_size defaults to 50 and is
// clamped to a maximum of 100; page floors at 1.
const (
	defaultPageSize = 50
	maxPageSize     = 100
)

// Sales list filter allowlists. Payment method spans the Direct Sale values
// (cash, transfer), payphone, the Payment Provider on Online Sales, and free,
// the zero-total checkouts the platform settled itself (ADR 0017).
var (
	salesStatusValues        = []string{"active", "reversed"}
	salesChannelValues       = []string{"online", "in_person", "import"}
	salesSourceValues        = []string{"direct", "external_platform"}
	salesPaymentMethodValues = []string{"cash", "transfer", "payphone", "free"}
)

// ListSales returns a page of the Event's Ticket Sales for the Sales list — one
// row per Ticket Sale — in the ADR-0006 nested envelope, narrowed by the query
// filters.
//
// @Summary      List an Event's Ticket Sales
// @Description  Returns a page of the Event's Ticket Sales for the Sales list: one row per Ticket Sale with the Customer, rolled-up Ticket Types, amount in the Event currency, sold_at, channel/source, status, confirmation_ref, the Tax ID snapshot the sale was transacted under (tax_id_type/tax_id_number, both null on sales recorded without one), the Sale Reversal provenance on a reversed row (reversed_at and reversed_by, which is `customer` when the buyer reversed their own Online Sale, `staff` when a Sale Import undo or a single-sale staff reversal did, and `operator` when the platform reversed it after refunding the buyer off-platform at the Organization's request; both null on an active sale and on a sale reversed before either was recorded — the Operator Reversal's money memo is operator-facing only and never appears here), the Sale Correction linkage (replaced_by_sale_id on a corrected sale and replaces_sale_id on its replacement, each with the linked sale's Confirmation reference beside it as replaced_by_confirmation_ref / replaces_confirmation_ref, all null until a correction is recorded — ADR 0050), `origin` — how the sale reached the platform, one of `sale_import` (it arrived in an uploaded Sale Import batch), `manually_recorded` (a Manually Recorded Sale somebody typed), `correction_replacement` (a Sale Correction's replacement) or `channel_sale` (it sold on a Sales Channel of its own), derived from the row and stored nowhere, so a row nobody recognises can be accounted for (ADR 0052) — each rolled-up Ticket Type carrying its ticket_type_id so the Correct form can be pre-filled from the row, held_ticket_count (how many of the sale's Tickets have an accepted Holder, the people a reversal would tell), and the recorded-at and payment method for the row-detail expand. Filterable by status (default active), ticket type (sales including that type), sold-at date range (interpreted in the Event timezone as a half-open interval, end date inclusive), a case-insensitive substring search over customer email/name/confirmation_ref/Tax ID number, and channel/source/payment_method. Sortable by `sort` (sold_at, recorded_at, customer, amount) and `dir` (asc/desc), both validated against allowlists and defaulting to sold_at descending; every sort carries a secondary id tiebreaker so equal values keep a stable order across pages. Response is the ADR-0006 nested envelope { data, pagination, reversed_count } with total via COUNT(*) OVER(); page_size defaults to 50 (max 100) and page floors at 1. `reversed_count` is how many of the Event's Ticket Sales are reversed, across the whole Event and independent of every filter on the request (including status), so a Sale Reversal is visible rather than a row that silently left the default view; it is 0 on an Event that has never had one. Visible to any Member of the Event.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id              path   string  true   "Event ID"
// @Param        page            query  int     false  "Page number (1-based; floors at 1)"
// @Param        page_size       query  int     false  "Page size (default 50, max 100)"
// @Param        status          query  string  false  "Ticket Sale status"  Enums(active, reversed)
// @Param        ticket_type_id  query  string  false  "Keep only sales that include this Ticket Type"
// @Param        sold_from       query  string  false  "Sold-at range start (YYYY-MM-DD, Event timezone, inclusive)"
// @Param        sold_to         query  string  false  "Sold-at range end (YYYY-MM-DD, Event timezone, inclusive of the whole day)"
// @Param        q               query  string  false  "Case-insensitive substring over customer email, name, confirmation_ref, and Tax ID number (full or partial)"
// @Param        channel         query  string  false  "Sales Channel"  Enums(online, in_person, import)
// @Param        source          query  string  false  "Sales Source"  Enums(direct, external_platform)
// @Param        payment_method  query  string  false  "Payment Method"  Enums(cash, transfer, payphone, free)
// @Param        sort            query  string  false  "Sort column (default sold_at)"  Enums(sold_at, recorded_at, customer, amount)
// @Param        dir             query  string  false  "Sort direction (default desc)"  Enums(asc, desc)
// @Success      200  {object}  platform.Envelope
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales [get]
func (h *Handler) ListSales(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	query := r.URL.Query()
	params, fields := parseSalesFilters(query)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	params.Page = pageParam(query.Get("page"))
	params.PageSize = pageSizeParam(query.Get("page_size"))
	params.Sort = sortParam(query.Get("sort"))
	params.Dir = dirParam(query.Get("dir"))

	result, err := h.svc.ListSales(r.Context(), actorFromRequest(r), eventID, params)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// ExportSales returns the Event's Ticket Sales as an .xlsx, narrowed by the same
// filters as the Sales list.
//
// @Summary      Export an Event's Ticket Sales as a spreadsheet
// @Description  Returns an .xlsx of the Event's Ticket Sales — one row per Ticket Sale — reflecting exactly the filters supplied, so the file matches the Sales list screen it was taken from. Accepts the SAME query parameters as the Sales list (status, ticket_type_id, sold_from/sold_to, q, channel, source, payment_method, sort, dir) and parses them with the list's own helper, so the two cannot drift; the pagination parameters are ignored, since a file is the whole answer. Status still defaults to `active`, so the default file omits reversed sales exactly as the default screen does, and the `status` filter reaches them in both places. The sold-at range is still interpreted in the Event timezone. Columns, left to right: confirmation_ref, sold_at, customer_first_name, customer_last_name, customer_email, tax_id_type, tax_id_number, one column per Ticket Type in the Event's live catalog, total_quantity, amount, net_proceeds, currency, channel, source, payment_method, status, reversed_at, reversed_by. Cells are really typed: sold_at and reversed_at are Excel date cells formatted `yyyy-mm-dd hh:mm` drawn in the Event's timezone, quantities are whole numbers, and amount and net_proceeds are numbers in major units (25.00, never 2500 and never a currency-prefixed string) with the currency in its own column. `reversed_at`/`reversed_by` are the Sale Reversal's provenance and are blank together on an active sale. `reversed_by` names the ROUTE only — `customer` (the buyer undid their own Online Sale), `platform` (an Operator Reversal), or `import_undo` (a whole Sale Import batch undone), `staff_reversal` (one imported sale reversed on its own) or `correction` (one replaced by a Sale Correction) — and never the acting Platform Operator's identity or their note, which are operator-facing and never reach this file (ADR-0019). The Sale Correction linkage follows the reversal pair: corrected_by carries the replacement's Confirmation reference on a corrected sale and corrects carries the mistaken sale's reference on its replacement, both blank on every other row (ADR 0050). The Tax ID pair carries the snapshot the sale was transacted under and is blank — never a placeholder — on a sale recorded without one. The workbook has exactly two sheets. `Info` comes first and is the active sheet on open: it states the Event's name, the generated-at timestamp (which also tells a reader which moment's Ticket Type catalog the headings reflect), the timezone named outright as the Event's, the row count, the currency, and the applied filters rendered in words rather than as query parameters — including, plainly, that reversed sales were excluded when the status filter left them out. The free-text search is stated as having been applied but its term is never written into the file, since it matches customer email and Tax ID number. The data sheet is named `Ticket Sales` and deliberately not `Sales`: the Sale Import parser selects its sheet by that name, so an export accidentally uploaded as an import fails rather than duplicating every sale. It carries nothing above its header row, so select-all, autofilter and pivot source ranges work without deleting a preamble — which is why the stamp is a sheet of its own. The filename is set by Content-Disposition as `sales-{event-slug}-{YYYY-MM-DD}.xlsx`. Generation is synchronous and the workbook is buffered in memory, so the file is CAPPED at 10,000 Ticket Sales — the same constant the Sale Import accepts, so an export can never exceed what the importer would take back. A request whose filters match MORE than the cap builds nothing and is refused with the standard VALIDATION_FAILED envelope, carrying one field error on `filters` whose message names how many sales matched and how many may be downloaded at once, so the caller knows how much narrower to go; exactly the cap succeeds. One structured log line is written per generated file — the acting Member, Organization, Event, the structural filters and the row count — because this is the largest concentration of buyer PII the product emits and "who pulled the customer list" cannot be answered retroactively. The free-text search is recorded in it as a boolean only: it matches customer email and Tax ID number, and logging the term would copy a buyer's PII into a log aggregator. Restricted to Org Admins and Event Owners — the same guard as the sales summary, because this file concentrates every buyer's email and Tax ID for an Event into something that is forwarded and retained; Event Staff are refused and keep the on-screen Sales list.
// @Tags         staff
// @Produce      application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security     BearerAuth
// @Param        id              path   string  true   "Event ID"
// @Param        status          query  string  false  "Ticket Sale status (default active)"  Enums(active, reversed)
// @Param        ticket_type_id  query  string  false  "Keep only sales that include this Ticket Type"
// @Param        sold_from       query  string  false  "Sold-at range start (YYYY-MM-DD, Event timezone, inclusive)"
// @Param        sold_to         query  string  false  "Sold-at range end (YYYY-MM-DD, Event timezone, inclusive of the whole day)"
// @Param        q               query  string  false  "Case-insensitive substring over customer email, name, confirmation_ref, and Tax ID number"
// @Param        channel         query  string  false  "Sales Channel"  Enums(online, in_person, import)
// @Param        source          query  string  false  "Sales Source"  Enums(direct, external_platform)
// @Param        payment_method  query  string  false  "Payment Method"  Enums(cash, transfer, payphone, free)
// @Param        sort            query  string  false  "Sort column (default sold_at)"  Enums(sold_at, recorded_at, customer, amount)
// @Param        dir             query  string  false  "Sort direction (default desc)"  Enums(asc, desc)
// @Success      200
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/export [get]
func (h *Handler) ExportSales(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	// The Sales list's own parser, verbatim: the export exists to hand back what
	// the screen was showing, and two readings of the same query string would be
	// two chances for the file and the screen to disagree. Pagination is the one
	// thing not carried over — a file is the whole answer.
	query := r.URL.Query()
	params, fields := parseSalesFilters(query)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	params.Sort = sortParam(query.Get("sort"))
	params.Dir = dirParam(query.Get("dir"))

	export, fieldErrs, err := h.svc.ExportSales(r.Context(), actorFromRequest(r), eventID, params)
	if len(fieldErrs) > 0 {
		// More matching sales than one file may carry. It is VALIDATION_FAILED
		// rather than a domain error because the answer is something the caller
		// changes about their request — the filters, which the staff app has on
		// screen beside the button — and the same envelope the invalid-filter
		// refusal above uses means the client has one error path to render, not
		// two.
		_ = platform.WriteValidationError(w, reqID, fieldErrs)
		return
	}
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+export.Filename+"\"")
	w.Header().Set("X-Request-ID", reqID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(export.Data)
}

// GetSalesSummary returns the Event's Net Proceeds and active sales count for
// the Sales tab's stat strip.
//
// @Summary      Get an Event's sales summary
// @Description  Returns the Sales tab stat strip for the Event: net_proceeds_cents — what the Event's active Online Sales have left the Organization after the Platform Fee and its Fee IVA, summed from the per-line snapshots the sales froze (in-person and imported sales contribute nothing; reversed sales drop out) — plus the Event currency, the count of its active Ticket Sales across all channels, and tickets_sold — the quantities of those sales' lines summed, likewise across all channels, so one Ticket Sale of four tickets counts 1 toward sales_count and 4 here. The platform's cut is never returned as a number. Restricted to Org Admins and Event Owners; Event Staff are refused.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeSalesSummary
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/summary [get]
func (h *Handler) GetSalesSummary(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	summary, err := h.svc.EventSalesSummary(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, summary)
}

// GetSalesTrends returns the Event's per-day tickets and Takings by Ticket Type
// for the Sales Trends tab.
//
// @Summary      Get an Event's Sales Trends
// @Description  Returns everything the Sales Trends tab draws, in one request: the Event's selling life as a CONTIGUOUS run of days, each split by Ticket Type into tickets sold and Takings earned. `timezone` is the Event's own zone (UTC where it carries none) and days are bucketed in it, resolved by the same helper the Sales Export uses, so a late-night sale falls on the day it felt like locally and matches the Sales list and the Sales Export. Bucketing reads `sold_at` — the day the sale was MADE — never `created_at`, so a Sale Import of last year's history lands on last year's days instead of spiking on the upload day. `days` runs from the first sale's day to the earlier of today and the Event's end, zero-filled: every calendar day in the span is present and a day that sold nothing carries an empty `lines` array. Within a day, a Ticket Type that sold nothing is OMITTED rather than sent as a zero. `ticket_types` is the Event's whole catalog in display order (`sort_order`), including Ticket Types that have sold nothing, so a legend built from it is stable and colours stay put across loads; names are the current catalog names, as the Sales Export's columns are. `takings_cents` is TAKINGS (ADR 0040) — what the sale earned the Organization on whatever Sales Channel it sold: the Net Proceeds of an Online Sale, and the full price of a sale the platform took no cut of, since in-person and imported lines carry zero fee snapshots. It is deliberately NOT the Sales tab's `net_proceeds_cents`, which is online-only, and it will legitimately exceed it on any Event that sold anywhere but online; both surfaces name their figure. Free Ticket Types contribute quantity and nothing to Takings. Reversed sales are excluded from every quantity and every Takings figure, as they are from every other aggregate, and `reversed_count` states how many of the Event's Ticket Sales are reversed, across the whole Event and independent of the span. `currency` is the Organization's. There is no pagination and no filtering — this is a bounded aggregate, not a list, and the surface deliberately does not obey the Sales list's filters (whose status filter can select reversed sales). An Event that has sold nothing returns an empty `days` array with its catalog still stated. Restricted to Org Admins and Event Owners — the guard the Event's money already carries; Event Staff are refused.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200  {object}  openapi.EnvelopeSalesTrends
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/trends [get]
func (h *Handler) GetSalesTrends(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return
	}

	trends, err := h.svc.EventSalesTrends(r.Context(), actorFromRequest(r), eventID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, trends)
}

// parseSalesFilters validates the Sales list filter query params, returning the
// service params and any field errors (invalid enum values and malformed dates
// are rejected per the repo's VALIDATION_FAILED convention). Blank/absent
// filters are left unset; status defaults to active in the service.
func parseSalesFilters(query url.Values) (service.ListSalesParams, []platform.FieldError) {
	var fields []platform.FieldError

	status := validateEnum(query.Get("status"), "status", salesStatusValues, &fields)
	channel := validateEnum(query.Get("channel"), "channel", salesChannelValues, &fields)
	source := validateEnum(query.Get("source"), "source", salesSourceValues, &fields)
	paymentMethod := validateEnum(query.Get("payment_method"), "payment_method", salesPaymentMethodValues, &fields)
	soldFrom := validateDate(query.Get("sold_from"), "sold_from", &fields)
	soldTo := validateDate(query.Get("sold_to"), "sold_to", &fields)

	return service.ListSalesParams{
		Status:        status,
		TicketTypeID:  validateUUID(query.Get("ticket_type_id"), "ticket_type_id", &fields),
		SoldFrom:      soldFrom,
		SoldTo:        soldTo,
		Search:        strings.TrimSpace(query.Get("q")),
		Channel:       channel,
		Source:        source,
		PaymentMethod: paymentMethod,
	}, fields
}

// validateEnum trims a query value and checks it against an allowlist, appending
// a field error when it is present but not allowed. A blank value is valid
// (unfiltered) and returned as "".
func validateEnum(raw, field string, allowed []string, fields *[]platform.FieldError) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	for _, a := range allowed {
		if value == a {
			return value
		}
	}
	*fields = append(*fields, platform.FieldError{
		Field:   field,
		Code:    platform.CodeInvalidEnum,
		Message: "must be one of " + strings.Join(allowed, ", "),
	})
	return ""
}

// validateUUID trims a query value and checks it is a well-formed UUID (the
// ticket_type_id column is UUID NOT NULL, so a malformed value would otherwise
// error at the database as a 500 rather than a VALIDATION_FAILED 400). A blank
// value is valid (unfiltered) and returned as "".
func validateUUID(raw, field string, fields *[]platform.FieldError) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := uuid.Parse(value); err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidID, Message: "must be a valid id"})
		return ""
	}
	return value
}

// validateDate trims a query value and checks it parses as a "YYYY-MM-DD"
// calendar date, appending a field error otherwise. A blank value is valid
// (open bound) and returned as "".
func validateDate(raw, field string, fields *[]platform.FieldError) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		*fields = append(*fields, platform.FieldError{Field: field, Code: platform.CodeInvalidDate, Message: "must be a date (YYYY-MM-DD)"})
		return ""
	}
	return value
}

// pageParam parses the `page` query value, flooring at 1: a missing, invalid, or
// below-one value becomes page 1.
func pageParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// Sales list sort allowlists (ADR-0006): `sort` and `dir` are validated against
// these fixed sets; an unknown value normalizes to the default (sold_at desc) so
// a hand-edited or stale shared URL stays usable rather than erroring.
const (
	defaultSort = "sold_at"
	defaultDir  = "desc"
)

var allowedSorts = map[string]bool{
	"sold_at":     true,
	"recorded_at": true,
	"customer":    true,
	"amount":      true,
}

// sortParam resolves the `sort` query value against the allowlist, defaulting to
// sold_at when missing or unknown.
func sortParam(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if allowedSorts[v] {
		return v
	}
	return defaultSort
}

// dirParam resolves the `dir` query value to "asc" or "desc", defaulting to desc
// when missing or unknown.
func dirParam(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "asc" || v == "desc" {
		return v
	}
	return defaultDir
}

// pageSizeParam parses the `page_size` query value, defaulting to 50 and
// clamping to [1, 100]: a missing or invalid value uses the default; a value
// above the maximum is clamped down; a below-one value uses the default.
func pageSizeParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return defaultPageSize
	}
	if n > maxPageSize {
		return maxPageSize
	}
	return n
}

type undoImportBody struct {
	NotifyBuyers bool `json:"notify_buyers"`
}

// UndoSaleImport reverses the latest committed Sale Import batch on an Event,
// restoring capacity and optionally emailing affected buyers a void notice.
//
// @Summary      Undo a Sale Import
// @Description  Reverses the most recent committed Sale Import batch on an Event: marks its Ticket Sales reversed, restores each Ticket Type's sold_count, and marks the batch reversed. Only the latest batch is reversible (409 IMPORT_NOT_LATEST_BATCH otherwise; 409 IMPORT_ALREADY_REVERSED if already undone). With `notify_buyers` true, each affected buyer is emailed a void/cancellation notice referencing their Sale Confirmation; false sends the buyers nothing. Independently of the flag, every Holder who ACCEPTED a Ticket Assignment on a swept sale is told the Ticket is no longer theirs — they proved their address and accepted, so the notice is theirs by right. The one exception is a buyer holding their own sale's first Ticket as a presumed Self-held Ticket (ADR 0055): they accepted nothing, so that notice follows `notify_buyers` like any other buyer mail, which is what keeps an undo of a 184-sale batch from mailing 184 people who asked for nothing.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id       path      string          true  "Event ID"
// @Param        batchId  path      string          true  "Sale Import batch ID"
// @Param        body     body      undoImportBody  false "Undo options"
// @Success      200      {object}  platform.Envelope
// @Failure      400      {object}  platform.Envelope
// @Failure      401      {object}  platform.Envelope
// @Failure      403      {object}  platform.Envelope
// @Failure      404      {object}  platform.Envelope
// @Failure      409      {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports/{batchId}/undo [post]
func (h *Handler) UndoSaleImport(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	batchID := strings.TrimSpace(r.PathValue("batchId"))
	var fields []platform.FieldError
	if eventID == "" {
		fields = append(fields, platform.FieldError{Field: "id", Code: platform.CodeRequired, Message: "is required"})
	}
	if batchID == "" {
		fields = append(fields, platform.FieldError{Field: "batchId", Code: platform.CodeRequired, Message: "is required"})
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	// The body is optional; a missing/empty body defaults notify_buyers to false.
	var body undoImportBody
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err != io.EOF {
			_ = platform.WriteInvalidJSON(w, reqID)
			return
		}
	}

	result, err := h.svc.UndoImport(r.Context(), actorFromRequest(r), eventID, batchID, body.NotifyBuyers)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

func (h *Handler) commitFromJSON(w http.ResponseWriter, r *http.Request, reqID, eventID string) {
	var body commitImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = "direct"
	}

	fields, rows := validateImport(source, body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	result, err := h.svc.CommitImport(r.Context(), actorFromRequest(r), eventID, service.CommitImportInput{
		Source:         source,
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
		Sales:          rows,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}

	writeCommitResult(w, reqID, result)
}

func (h *Handler) commitFromFile(w http.ResponseWriter, r *http.Request, reqID, eventID string) {
	rows, ok := h.parseUploadedFile(w, r, reqID)
	if !ok {
		return
	}

	source := strings.TrimSpace(r.FormValue("source"))
	if source == "" {
		source = "direct"
	}
	idempotencyKey := strings.TrimSpace(r.FormValue("idempotency_key"))
	skipRows, skipErr := parseSkipRows(r.FormValue("skip_rows"))

	var fields []platform.FieldError
	if idempotencyKey == "" {
		fields = append(fields, platform.FieldError{Field: "idempotency_key", Code: platform.CodeRequired, Message: "is required"})
	}
	if source != "direct" {
		fields = append(fields, platform.FieldError{Field: "source", Code: platform.CodeInvalidImportSource, Message: "must be 'direct'"})
	}
	if len(rows) == 0 {
		fields = append(fields, platform.FieldError{Field: "file", Code: platform.CodeEmptyCollection, Message: "must contain at least one row"})
	}
	if skipErr != nil {
		fields = append(fields, *skipErr)
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}

	result, invalid, err := h.svc.CommitImportFile(r.Context(), actorFromRequest(r), eventID, service.FileCommitInput{
		Source:         source,
		IdempotencyKey: idempotencyKey,
		Rows:           rows,
		SkipRows:       skipRows,
	})
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	if invalid != nil {
		_ = platform.WriteValidationError(w, reqID, rowValidationFields(invalid))
		return
	}
	writeCommitResult(w, reqID, result)
}

func writeCommitResult(w http.ResponseWriter, reqID string, result *service.ImportResult) {
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	_ = platform.WriteSuccess(w, reqID, status, result)
}

// parseUploadedFile reads the multipart `file` field and parses it into rows,
// writing the appropriate error envelope and returning ok=false on failure.
func (h *Handler) parseUploadedFile(w http.ResponseWriter, r *http.Request, reqID string) ([]importfile.RawRow, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "file", Code: platform.CodeInvalidUpload, Message: "must be a valid multipart upload"}})
		return nil, false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "file", Code: platform.CodeRequired, Message: "is required"}})
		return nil, false
	}
	defer func() { _ = file.Close() }()

	filename := ""
	if header != nil {
		filename = header.Filename
	}
	rows, err := importfile.Parse(filename, io.LimitReader(file, maxUploadBytes))
	if err != nil {
		switch {
		case importfile.IsUnreadable(err):
			reason, _ := importfile.UnreadableReason(err)
			_ = platform.WriteDomainError(w, reqID, sales.ErrImportFileUnreadable(reason))
		case err == importfile.ErrTooLarge:
			_ = platform.WriteDomainError(w, reqID, sales.ErrImportFileTooLarge(importfile.MaxRows))
		default:
			_ = platform.WriteDomainError(w, reqID, sales.ErrImportFileUnreadable(err.Error()))
		}
		return nil, false
	}
	return rows, true
}

// rowValidationFields flattens per-row errors into the field-error list a
// VALIDATION_FAILED envelope carries (e.g. "rows[3].customer_email").
//
// These carry no stable code. A RowError is a spreadsheet cell's complaint,
// already shown verbatim beside the row in the Sale Import preview table, and it
// is Staff-only — the code exists so the Storefront can key Spanish copy on it,
// and nothing here is reachable from the Storefront. An empty code is the
// documented "fall back to the message" case rather than an omission.
func rowValidationFields(result *importfile.ValidateResult) []platform.FieldError {
	var fields []platform.FieldError
	for _, row := range result.Rows {
		for _, e := range row.Errors {
			fields = append(fields, platform.FieldError{
				Field:   fmt.Sprintf("rows[%d].%s", row.Row, e.Field),
				Message: e.Message,
			})
		}
	}
	return fields
}

// bareRowValidationFields flattens ONE typed row's complaints into field errors
// under the template's own column names — "quantity", not "rows[1].quantity".
//
// A form has an input per template column and no row index anywhere on it, so a
// prefix here would name a field the caller cannot find and every typed route
// would have to strip it back off (#367). The uploaded file keeps the prefix,
// where it is the only thing telling row 3's complaint from row 40's.
//
// No stable code, for the reason rowValidationFields gives: a RowError is a
// spreadsheet cell's complaint, shown verbatim, and Staff-only.
func bareRowValidationFields(result *importfile.ValidateResult) []platform.FieldError {
	var fields []platform.FieldError
	for _, row := range result.Rows {
		for _, e := range row.Errors {
			fields = append(fields, platform.FieldError{Field: e.Field, Message: e.Message})
		}
	}
	return fields
}

// parseSkipRows parses the multipart `skip_rows` field: a comma-separated list of
// file row numbers to exclude from the commit (e.g. "3,7"), tolerating optional
// surrounding brackets and whitespace. Blank means skip nothing.
func parseSkipRows(raw string) ([]int, *platform.FieldError) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "[]")
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, &platform.FieldError{Field: "skip_rows", Code: platform.CodeInvalidRowSelection, Message: "must be a comma-separated list of row numbers"}
		}
		out = append(out, n)
	}
	return out, nil
}

func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
}

// slugFilename reduces an Event name to a filename-safe slug for downloads.
func slugFilename(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "event"
	}
	return url.PathEscape(slug)
}

func validateImport(source string, body commitImportBody) ([]platform.FieldError, []service.ImportSaleInput) {
	var fields []platform.FieldError

	if strings.TrimSpace(body.IdempotencyKey) == "" {
		fields = append(fields, platform.FieldError{Field: "idempotency_key", Code: platform.CodeRequired, Message: "is required"})
	}
	if source != "direct" {
		fields = append(fields, platform.FieldError{Field: "source", Code: platform.CodeInvalidImportSource, Message: "must be 'direct'"})
	}
	if len(body.Sales) == 0 {
		fields = append(fields, platform.FieldError{Field: "sales", Code: platform.CodeEmptyCollection, Message: "must contain at least one row"})
		return fields, nil
	}

	rows := make([]service.ImportSaleInput, 0, len(body.Sales))
	for i, row := range body.Sales {
		prefix := fmt.Sprintf("sales[%d].", i)

		if strings.TrimSpace(row.CustomerEmail) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_email", Code: platform.CodeRequired, Message: "is required"})
		} else if _, err := mail.ParseAddress(row.CustomerEmail); err != nil {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_email", Code: platform.CodeInvalidEmail, Message: "must be a valid email"})
		}
		if strings.TrimSpace(row.CustomerFirstName) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_first_name", Code: platform.CodeRequired, Message: "is required"})
		}
		if strings.TrimSpace(row.CustomerLastName) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_last_name", Code: platform.CodeRequired, Message: "is required"})
		}
		if strings.TrimSpace(row.TicketTypeID) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Code: platform.CodeRequired, Message: "is required"})
		}
		if row.Quantity <= 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "quantity", Code: platform.CodeInvalidPositiveInt, Message: "must be greater than zero"})
		}
		if row.PaymentMethod != "cash" && row.PaymentMethod != "transfer" {
			fields = append(fields, platform.FieldError{Field: prefix + "payment_method", Code: platform.CodeInvalidPaymentMethod, Message: "must be 'cash' or 'transfer'"})
		}
		var soldAt time.Time
		if strings.TrimSpace(row.SoldAt) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "sold_at", Code: platform.CodeRequired, Message: "is required"})
		} else if parsed, err := time.Parse(time.RFC3339, row.SoldAt); err != nil {
			fields = append(fields, platform.FieldError{Field: prefix + "sold_at", Code: platform.CodeInvalidTimestamp, Message: "must be an ISO 8601 timestamp"})
		} else {
			soldAt = parsed
		}
		if row.AmountCents != nil && *row.AmountCents < 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "amount_cents", Code: platform.CodeInvalidNonNegativeInt, Message: "must not be negative"})
		}

		rows = append(rows, service.ImportSaleInput{
			CustomerEmail:     strings.TrimSpace(row.CustomerEmail),
			CustomerFirstName: strings.TrimSpace(row.CustomerFirstName),
			CustomerLastName:  strings.TrimSpace(row.CustomerLastName),
			TicketTypeID:      strings.TrimSpace(row.TicketTypeID),
			Quantity:          row.Quantity,
			PaymentMethod:     row.PaymentMethod,
			SoldAt:            soldAt,
			AmountCents:       row.AmountCents,
		})
	}

	return fields, rows
}

// ReverseSale reverses one imported Ticket Sale from the Sales list.
//
// @Summary      Reverse one imported Ticket Sale
// @Description  Reverses a single active `import`-channel Ticket Sale from any Sale Import batch, however old (ADR 0050): the sale is marked reversed by staff, each Ticket Type's sold_count is restored, every Holder who accepted a Ticket Assignment on it is told it is no longer theirs, and the buyer is mailed nothing — including when the buyer holds the sale's first Ticket as a presumed Self-held Ticket, which on this route is not a Holder who gets told (ADR 0055). The batch is not touched and stays undoable for its remaining active sales. Refused with 409 SALE_NOT_IMPORTED on an Online or In-Person Sale, 409 SALE_ALREADY_REVERSED on a reversed sale, and 404 TICKET_SALE_NOT_FOUND when the sale is not on this Event. Gated by the same permission as Sale Import.
// @Tags         staff
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string  true  "Event ID"
// @Param        saleId  path      string  true  "Ticket Sale ID"
// @Success      200     {object}  platform.Envelope
// @Failure      400     {object}  platform.Envelope
// @Failure      401     {object}  platform.Envelope
// @Failure      403     {object}  platform.Envelope
// @Failure      404     {object}  platform.Envelope
// @Failure      409     {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/{saleId}/reverse [post]
func (h *Handler) ReverseSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID, saleID, ok := saleTarget(w, r, reqID)
	if !ok {
		return
	}

	result, err := h.svc.ReverseImportedSale(r.Context(), actorFromRequest(r), eventID, saleID)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// correctSaleBody is the Sale Correction form: the Sale Import template's
// columns for the replacement, plus send_confirmation (default false).
type correctSaleBody struct {
	CustomerEmail       string `json:"customer_email"`
	CustomerFirstName   string `json:"customer_first_name"`
	CustomerLastName    string `json:"customer_last_name"`
	CustomerTaxIDType   string `json:"customer_tax_id_type"`
	CustomerTaxIDNumber string `json:"customer_tax_id_number"`
	TicketTypeID        string `json:"ticket_type_id"`
	Quantity            int    `json:"quantity"`
	PaymentMethod       string `json:"payment_method"`
	SoldAt              string `json:"sold_at"`
	AmountCents         *int   `json:"amount_cents"`
	SendConfirmation    bool   `json:"send_confirmation"`
}

// correctSaleInput lifts the decoded form into the service's input: the
// template's columns through the shared trim, plus the one field a correction
// alone carries.
func correctSaleInput(body correctSaleBody) service.CorrectSaleInput {
	return service.CorrectSaleInput{
		ImportRowInput: trimmedImportRow(service.ImportRowInput{
			CustomerEmail:       body.CustomerEmail,
			CustomerFirstName:   body.CustomerFirstName,
			CustomerLastName:    body.CustomerLastName,
			CustomerTaxIDType:   body.CustomerTaxIDType,
			CustomerTaxIDNumber: body.CustomerTaxIDNumber,
			TicketTypeID:        body.TicketTypeID,
			Quantity:            body.Quantity,
			PaymentMethod:       body.PaymentMethod,
			SoldAt:              body.SoldAt,
			AmountCents:         body.AmountCents,
		}),
		SendConfirmation: body.SendConfirmation,
	}
}

// trimmedImportRow trims every text cell of one typed Sale Import row, and is
// the one place any route that types a row rather than uploading one does it
// (#367).
//
// It is shared rather than repeated because the trim is part of the verdict,
// not decoration: " a@b.com " is a valid email and "a@b.com " is not, so a
// route that forgot to trim would refuse a row its sibling accepts — the exact
// drift between the two typed routes that ADR 0052 requires cannot happen.
// The uploaded file gets the same treatment from the parser, before a RawRow
// exists at all.
func trimmedImportRow(in service.ImportRowInput) service.ImportRowInput {
	in.CustomerEmail = strings.TrimSpace(in.CustomerEmail)
	in.CustomerFirstName = strings.TrimSpace(in.CustomerFirstName)
	in.CustomerLastName = strings.TrimSpace(in.CustomerLastName)
	in.CustomerTaxIDType = strings.TrimSpace(in.CustomerTaxIDType)
	in.CustomerTaxIDNumber = strings.TrimSpace(in.CustomerTaxIDNumber)
	in.TicketTypeID = strings.TrimSpace(in.TicketTypeID)
	in.PaymentMethod = strings.TrimSpace(in.PaymentMethod)
	in.SoldAt = strings.TrimSpace(in.SoldAt)
	return in
}

// saleTarget reads and checks the two path ids of a correction call,
// writing the refusal itself when either is missing or malformed.
func saleTarget(w http.ResponseWriter, r *http.Request, reqID string) (eventID, saleID string, ok bool) {
	eventID = strings.TrimSpace(r.PathValue("id"))
	saleID = strings.TrimSpace(r.PathValue("saleId"))
	var fields []platform.FieldError
	if eventID == "" {
		fields = append(fields, platform.FieldError{Field: "id", Code: platform.CodeRequired, Message: "is required"})
	}
	if saleID == "" {
		fields = append(fields, platform.FieldError{Field: "saleId", Code: platform.CodeRequired, Message: "is required"})
	}
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return "", "", false
	}
	if _, err := uuid.Parse(saleID); err != nil {
		_ = platform.WriteDomainError(w, reqID, sales.ErrTicketSaleIDNotFound(saleID))
		return "", "", false
	}
	return eventID, saleID, true
}

// PreviewSaleCorrection judges a Sale Correction's replacement without
// recording anything.
//
// @Summary      Preview one Sale Correction
// @Description  The Correct form's live verdict (#352, ADR 0050): validates the replacement exactly as POST .../correct would, and writes nothing. The body is the same as the commit's (send_confirmation is accepted and ignored). Returns the Sale Import preview's shape — `rows` holding exactly one row with its `valid` flag and per-column `errors` (field names are the template's column names), `capacity_impact` per Ticket Type, and `committable` — with every figure counted NET of the sale being corrected: re-submitting the sale's own quantity never reads as an oversell, and the Purchase Limit leaves the sale's own holding out. A replacement matching another active sale on the Event by email, Ticket Type and sold-at date carries `possible_duplicate` with `duplicate_of_date`, a warning that does not block; the sale being corrected is never compared against itself. Always 200 when the sale can be corrected, whatever the verdict; the same refusals as the commit otherwise — 409 SALE_NOT_IMPORTED, 409 SALE_ALREADY_REVERSED, 404 TICKET_SALE_NOT_FOUND — and the same permission gate.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string           true  "Event ID"
// @Param        saleId  path      string           true  "Ticket Sale ID"
// @Param        body    body      correctSaleBody  true  "The replacement, as the template's columns"
// @Success      200     {object}  platform.Envelope
// @Failure      400     {object}  platform.Envelope
// @Failure      401     {object}  platform.Envelope
// @Failure      403     {object}  platform.Envelope
// @Failure      404     {object}  platform.Envelope
// @Failure      409     {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/{saleId}/correct/preview [post]
func (h *Handler) PreviewSaleCorrection(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, saleID, ok := saleTarget(w, r, reqID)
	if !ok {
		return
	}
	var body correctSaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	result, err := h.svc.PreviewCorrection(r.Context(), actorFromRequest(r), eventID, saleID, correctSaleInput(body))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}

// CorrectSale reverses one imported Ticket Sale and records its replacement.
//
// @Summary      Correct one imported Ticket Sale
// @Description  A Sale Correction (ADR 0050): reverses a single active `import`-channel Ticket Sale and records a replacement in the SAME transaction, each pointing at the other (replaced_by_sale_id on the old sale, replaces_sale_id on the new one). The body is the Sale Import template's columns for the replacement — customer_email, customer_first_name, customer_last_name, the optional customer_tax_id_type/customer_tax_id_number pair, ticket_type_id, quantity, payment_method (cash|transfer), sold_at (ISO 8601, naive values read in the Event timezone), and an optional amount_cents giving the price of ONE ticket, overriding the catalog price and multiplied by the quantity — plus `send_confirmation` (default false). The replacement is validated exactly like an import row: Ticket Type on the Event, Tax ID rules when either half is filled, Payment Method required, sold_at not in the future, capacity and the Purchase Limit both counted NET of the sale being reversed. A refused replacement returns 400 VALIDATION_FAILED with one field error per offending column (field names are the template's column names) and writes nothing — the original sale stays active. The replacement is a fresh sale: channel `import`, source `direct`, no Sale Import batch (so a later batch undo never sweeps it), a new Sale Confirmation reference, and fresh Tickets — no Ticket Assignment or Answer is carried over, though the replacement's BUYER holds its first Ticket as a Self-held Ticket exactly as on any imported Sale (ADR 0055), so a correction re-seats the buyer on the Holder List instead of dropping them off it; the rest start `unassigned`, and nothing is written while TICKET_ASSIGNMENT_ENABLED is off. Every Holder who ACCEPTED a Ticket Assignment on the old sale is told it is no longer theirs, whatever send_confirmation says; the buyer's own presumed Self-held Ticket is the exception, since the buyer never accepted anything — that No Longer Holding notice follows send_confirmation like any other buyer mail (ADR 0055). The buyer is otherwise mailed NOTHING unless send_confirmation is true, in which case the replacement's Sale Confirmation (with the outstanding-answers line) goes to the replacement's email — never a voided mail. Works before, during and after the Event. Refused with 409 SALE_NOT_IMPORTED on an Online or In-Person Sale, 409 SALE_ALREADY_REVERSED on a reversed (or already corrected) sale, 404 TICKET_SALE_NOT_FOUND when the sale is not on this Event, and 409 IMPORT_BATCH_FAILED if capacity was lost to a race between validation and commit. Gated by the same permission as Sale Import.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id      path      string           true  "Event ID"
// @Param        saleId  path      string           true  "Ticket Sale ID"
// @Param        body    body      correctSaleBody  true  "The replacement, as the template's columns, plus send_confirmation"
// @Success      201     {object}  platform.Envelope
// @Failure      400     {object}  platform.Envelope
// @Failure      401     {object}  platform.Envelope
// @Failure      403     {object}  platform.Envelope
// @Failure      404     {object}  platform.Envelope
// @Failure      409     {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/{saleId}/correct [post]
func (h *Handler) CorrectSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, saleID, ok := saleTarget(w, r, reqID)
	if !ok {
		return
	}

	var body correctSaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}

	result, invalid, err := h.svc.CorrectImportedSale(r.Context(), actorFromRequest(r), eventID, saleID, correctSaleInput(body))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	if invalid != nil {
		_ = platform.WriteValidationError(w, reqID, bareRowValidationFields(invalid))
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, result)
}

// manualSaleBody is the Manually Recorded Sale form: the Sale Import template's
// columns, cell for cell, and nothing else (#368, ADR 0052).
//
// It does NOT embed the Sale Correction's body even though the two are the same
// columns today. Sharing one struct would put both routes' OpenAPI schema on one
// definition, so a field added for one would silently appear on the other's
// documented contract — and the two bodies are deliberately diverging: a
// correction carries send_confirmation and this never will.
//
// THERE IS NO send_confirmation AND NO idempotency_key. The buyer is always
// mailed, because no prior Sale Confirmation exists to fall back on; and the
// create carries no key because there is no batch to hang one on, with the hole
// that leaves named and accepted in ADR 0052's consequences.
type manualSaleBody struct {
	CustomerEmail       string `json:"customer_email"`
	CustomerFirstName   string `json:"customer_first_name"`
	CustomerLastName    string `json:"customer_last_name"`
	CustomerTaxIDType   string `json:"customer_tax_id_type"`
	CustomerTaxIDNumber string `json:"customer_tax_id_number"`
	TicketTypeID        string `json:"ticket_type_id"`
	// Quantity is a whole number of tickets. It is decoded as a raw JSON number
	// and documented as an integer; manualSaleQuantity explains why the Go type
	// is not one, and no client should read it as licence to send a decimal.
	Quantity      json.Number `json:"quantity" swaggertype:"integer"`
	PaymentMethod string      `json:"payment_method"`
	SoldAt        string      `json:"sold_at"`
	AmountCents   *int        `json:"amount_cents"`
}

// manualSaleQuantity reads the quantity cell, and is the whole reason the field
// is a json.Number.
//
// A spreadsheet carries "1.5" as text and the import validator answers it on the
// quantity column, with MsgQuantityNotWhole. A form carries it as a JSON number,
// which an int field refuses at the decoder — turning a complaint the organizer
// could act on into an unparseable body naming nothing. So the fractional case
// is caught here and answered in the validator's own words, on the validator's
// own column, and everything else falls through to be judged exactly as an
// uploaded row is.
//
// A blank or absent quantity reads as 0 and is left to the validator, which
// already says "must be greater than zero" about it.
func manualSaleQuantity(raw json.Number) (int, *platform.FieldError) {
	if strings.TrimSpace(raw.String()) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw.String())
	if err != nil {
		return 0, &platform.FieldError{Field: importfile.ColQuantity, Message: importfile.MsgQuantityNotWhole}
	}
	return n, nil
}

// manualSaleInput lifts the decoded form into the shared typed-import-row input,
// through the same trim every typed route uses — the trim is part of the
// verdict, not decoration (see trimmedImportRow).
func manualSaleInput(body manualSaleBody, quantity int) service.ImportRowInput {
	return trimmedImportRow(service.ImportRowInput{
		CustomerEmail:       body.CustomerEmail,
		CustomerFirstName:   body.CustomerFirstName,
		CustomerLastName:    body.CustomerLastName,
		CustomerTaxIDType:   body.CustomerTaxIDType,
		CustomerTaxIDNumber: body.CustomerTaxIDNumber,
		TicketTypeID:        body.TicketTypeID,
		Quantity:            quantity,
		PaymentMethod:       body.PaymentMethod,
		SoldAt:              body.SoldAt,
		AmountCents:         body.AmountCents,
	})
}

// manualSaleForm reads the Event id and decodes the form both manual-sale routes
// take, writing the refusal itself when either is unusable.
//
// The quantity complaint is written as a VALIDATION_FAILED naming the column,
// exactly as the service's own refusals are, so a form that renders errors
// beside their inputs needs no second shape for this one.
func manualSaleForm(w http.ResponseWriter, r *http.Request, reqID string) (eventID string, in service.ImportRowInput, ok bool) {
	eventID = strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Code: platform.CodeRequired, Message: "is required"}})
		return "", service.ImportRowInput{}, false
	}
	var body manualSaleBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return "", service.ImportRowInput{}, false
	}
	quantity, complaint := manualSaleQuantity(body.Quantity)
	if complaint != nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{*complaint})
		return "", service.ImportRowInput{}, false
	}
	return eventID, manualSaleInput(body, quantity), true
}

// RecordManualSale records one Manually Recorded Sale.
//
// @Summary      Record one Ticket Sale by hand
// @Description  A Manually Recorded Sale (#368, ADR 0052): ONE Sale Import row typed instead of uploaded. The body is the Sale Import template's columns — customer_email, customer_first_name, customer_last_name, the optional customer_tax_id_type/customer_tax_id_number pair, ticket_type_id, quantity, payment_method (cash|transfer), sold_at (ISO 8601, naive values read in the Event timezone), and an optional amount_cents giving the price of ONE ticket, overriding the Ticket Type's catalog price and multiplied by the quantity (0 records a comp). There is deliberately NO send_confirmation and NO idempotency_key. The row is validated exactly as an import row: Ticket Type on this Event, the Tax ID pair rule with the pair optional, a positive integer quantity, sold_at not in the future — plus two departures from the file import, both deliberate: CAPACITY is refused here on the quantity rather than deferred to a batch commit, and the PURCHASE LIMIT is enforced. A refused row returns 400 VALIDATION_FAILED with one field error per offending column, named by the template's bare column name with no rows[N]. prefix, and writes nothing. A row matching another active sale on the Event by email, Ticket Type and sold-at date carries possible_duplicate with duplicate_of_date — a warning that never blocks the record. The recorded sale is a Direct Sale on the `import` channel with source `direct` and NO Sale Import batch: it never appears in the Import history, and recording one never stops an earlier upload being the latest batch, so an existing batch stays undoable. Tickets are minted one per unit and the BUYER holds the first of them as a Self-held Ticket — assigned to them and `accepted` in the same transaction, so the Holder List names them (ADR 0055) — with the rest `unassigned`; the presumption makes nobody Verified, sends no Assignment mail, has deliberately NO checkbox to opt out of, and is not written at all while TICKET_ASSIGNMENT_ENABLED is off. No fee snapshot is written, so the sale contributes its full price to Takings and nothing to Net Proceeds. The buyer is ALWAYS mailed their Sale Confirmation, in their own remembered language; confirmation_sent reports whether that mail went out and a failure does not undo the sale. Refused with 409 EVENT_IS_EXTERNAL_REGISTRATION before any field is judged on an Event that registers externally, 404 EVENT_NOT_FOUND when the Event is not this Organization's, and 409 IMPORT_BATCH_FAILED if capacity was lost to a race between validation and commit. Gated by the same permission as Sale Import; reading the Sales list is not.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string          true  "Event ID"
// @Param        body  body      manualSaleBody  true  "The sale, as the Sale Import template's columns"
// @Success      201   {object}  platform.Envelope
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales [post]
func (h *Handler) RecordManualSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, in, ok := manualSaleForm(w, r, reqID)
	if !ok {
		return
	}

	result, invalid, err := h.svc.RecordManualSale(r.Context(), actorFromRequest(r), eventID, in)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	if invalid != nil {
		_ = platform.WriteValidationError(w, reqID, bareRowValidationFields(invalid))
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, result)
}

// PreviewManualSale judges a Manually Recorded Sale without recording anything.
//
// @Summary      Preview one Manually Recorded Sale
// @Description  The manual sale form's live verdict (#368, ADR 0052): validates the row exactly as POST .../sales would, and writes nothing. The body is the same as the record's. Returns the Sale Import preview's shape — `rows` holding exactly one row with its `valid` flag and per-column `errors` (field names are the template's bare column names), `capacity_impact` per Ticket Type, and `committable`. Capacity and the Purchase Limit are both decided here, on the quantity column, which is what lets the organizer learn the Event is full or the buyer over their allowance while the number can still be changed. A row matching another active sale on the Event by email, Ticket Type and sold-at date carries `possible_duplicate` with `duplicate_of_date`, a warning that does not block. Always 200 whatever the verdict, with the same refusals and the same permission gate as the record.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string          true  "Event ID"
// @Param        body  body      manualSaleBody  true  "The sale, as the Sale Import template's columns"
// @Success      200   {object}  platform.Envelope
// @Failure      400   {object}  platform.Envelope
// @Failure      401   {object}  platform.Envelope
// @Failure      403   {object}  platform.Envelope
// @Failure      404   {object}  platform.Envelope
// @Failure      409   {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sales/preview [post]
func (h *Handler) PreviewManualSale(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	eventID, in, ok := manualSaleForm(w, r, reqID)
	if !ok {
		return
	}

	result, err := h.svc.PreviewManualSale(r.Context(), actorFromRequest(r), eventID, in)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, result)
}
