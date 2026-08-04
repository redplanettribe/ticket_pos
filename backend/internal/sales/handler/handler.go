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
// @Description  Records off-platform (cash/transfer) sales against an Event, decrementing capacity and emailing each customer a Sale Confirmation. All-or-nothing and idempotent. Accepts an uploaded .csv/.xlsx file (with optional `skip_rows`, a comma-separated list of file row numbers to exclude, e.g. resolved duplicates) or a JSON body. The file form re-runs the preview's max_per_customer check rather than trusting that a preview ran, so a row over a ticket type's limit fails the batch with VALIDATION_FAILED. The JSON form does NOT check it, and no parity should be inferred: it carries no per-row complaint channel to report a refusal through, and a Purchase Limit is a guardrail an Organization sets for itself — the same Org Admin may clear the limit, import, and set it back, which is the documented way to import history recorded before the limit existed (ADR 0025). An Event that registers externally sells no tickets on this platform, so every form of this call — both Sales Sources, file and JSON alike — is refused with EVENT_IS_EXTERNAL_REGISTRATION before the body is judged and before any Ticket Type is resolved: the refusal names the mode rather than a Ticket Type that does not exist and never will (ADR 0028).
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
// @Description  Returns a page of the Event's Ticket Sales for the Sales list: one row per Ticket Sale with the Customer, rolled-up Ticket Types, amount in the Event currency, sold_at, channel/source, status, confirmation_ref, the Tax ID snapshot the sale was transacted under (tax_id_type/tax_id_number, both null on sales recorded without one), the Sale Reversal provenance on a reversed row (reversed_at and reversed_by, which is `customer` when the buyer reversed their own Online Sale, `staff` when a Sale Import undo did, and `operator` when the platform reversed it after refunding the buyer off-platform at the Organization's request; both null on an active sale and on a sale reversed before either was recorded — the Operator Reversal's money memo is operator-facing only and never appears here), and the recorded-at and payment method for the row-detail expand. Filterable by status (default active), ticket type (sales including that type), sold-at date range (interpreted in the Event timezone as a half-open interval, end date inclusive), a case-insensitive substring search over customer email/name/confirmation_ref/Tax ID number, and channel/source/payment_method. Sortable by `sort` (sold_at, recorded_at, customer, amount) and `dir` (asc/desc), both validated against allowlists and defaulting to sold_at descending; every sort carries a secondary id tiebreaker so equal values keep a stable order across pages. Response is the ADR-0006 nested envelope { data, pagination, reversed_count } with total via COUNT(*) OVER(); page_size defaults to 50 (max 100) and page floors at 1. `reversed_count` is how many of the Event's Ticket Sales are reversed, across the whole Event and independent of every filter on the request (including status), so a Sale Reversal is visible rather than a row that silently left the default view; it is 0 on an Event that has never had one. Visible to any Member of the Event.
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

// GetSalesSummary returns the Event's Net Proceeds and active sales count for
// the Sales tab's stat strip.
//
// @Summary      Get an Event's sales summary
// @Description  Returns the Sales tab stat strip for the Event: net_proceeds_cents — what the Event's active Online Sales have left the Organization after the Platform Fee and its Fee IVA, summed from the per-line snapshots the sales froze (in-person and imported sales contribute nothing; reversed sales drop out) — plus the Event currency and the count of its active Ticket Sales across all channels. The platform's cut is never returned as a number. Restricted to Org Admins and Event Owners; Event Staff are refused.
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
// @Description  Reverses the most recent committed Sale Import batch on an Event: marks its Ticket Sales reversed, restores each Ticket Type's sold_count, and marks the batch reversed. Only the latest batch is reversible (409 IMPORT_NOT_LATEST_BATCH otherwise; 409 IMPORT_ALREADY_REVERSED if already undone). With `notify_buyers` true, each affected buyer is emailed a void/cancellation notice referencing their Sale Confirmation; false sends nothing.
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
