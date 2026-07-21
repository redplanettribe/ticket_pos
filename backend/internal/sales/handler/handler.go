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
	CustomerEmail string `json:"customer_email"`
	CustomerName  string `json:"customer_name"`
	TicketTypeID  string `json:"ticket_type_id"`
	Quantity      int    `json:"quantity"`
	PaymentMethod string `json:"payment_method"`
	SoldAt        string `json:"sold_at"`
	AmountCents   *int   `json:"amount_cents"`
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
	}
}

// DownloadSaleImportTemplate returns a per-event .xlsx Sale Import template.
//
// @Summary      Download the Sale Import template
// @Description  Returns a per-event .xlsx pre-listing the Event's Ticket Types as a locked dropdown, with the internal ticket type id in a hidden reference column.
// @Tags         staff
// @Produce      application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Security     BearerAuth
// @Param        id  path  string  true  "Event ID"
// @Success      200
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/staff/events/{id}/sale-imports/template [get]
func (h *Handler) DownloadSaleImportTemplate(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Message: "is required"}})
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
// @Description  Parses an uploaded .csv/.xlsx server-side and returns every row's validation result at once, the matched Ticket Type, the capacity impact per Ticket Type (with per-type oversell overage), soft possible-duplicate flags per row, and a top-level committable flag (false when any row is invalid or any Ticket Type is oversold). No writes.
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
// @Router       /api/v1/staff/events/{id}/sale-imports/preview [post]
func (h *Handler) PreviewSaleImport(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	eventID := strings.TrimSpace(r.PathValue("id"))
	if eventID == "" {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Message: "is required"}})
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
// @Description  Records off-platform (cash/transfer) sales against an Event, decrementing capacity and emailing each customer a Sale Confirmation. All-or-nothing and idempotent. Accepts an uploaded .csv/.xlsx file (with optional `skip_rows`, a comma-separated list of file row numbers to exclude, e.g. resolved duplicates) or a JSON body.
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
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "id", Message: "is required"}})
		return
	}

	if isMultipart(r) {
		h.commitFromFile(w, r, reqID, eventID)
		return
	}
	h.commitFromJSON(w, r, reqID, eventID)
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
		fields = append(fields, platform.FieldError{Field: "idempotency_key", Message: "is required"})
	}
	if source != "direct" {
		fields = append(fields, platform.FieldError{Field: "source", Message: "must be 'direct'"})
	}
	if len(rows) == 0 {
		fields = append(fields, platform.FieldError{Field: "file", Message: "must contain at least one row"})
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
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "file", Message: "must be a valid multipart upload"}})
		return nil, false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		_ = platform.WriteValidationError(w, reqID, []platform.FieldError{{Field: "file", Message: "is required"}})
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
			_ = platform.WriteDomainError(w, reqID, sales.ErrImportFileUnreadable(err.(*importfile.ErrUnreadable).Reason))
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
			return nil, &platform.FieldError{Field: "skip_rows", Message: "must be a comma-separated list of row numbers"}
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
		fields = append(fields, platform.FieldError{Field: "idempotency_key", Message: "is required"})
	}
	if source != "direct" {
		fields = append(fields, platform.FieldError{Field: "source", Message: "must be 'direct'"})
	}
	if len(body.Sales) == 0 {
		fields = append(fields, platform.FieldError{Field: "sales", Message: "must contain at least one row"})
		return fields, nil
	}

	rows := make([]service.ImportSaleInput, 0, len(body.Sales))
	for i, row := range body.Sales {
		prefix := fmt.Sprintf("sales[%d].", i)

		if strings.TrimSpace(row.CustomerEmail) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_email", Message: "is required"})
		} else if _, err := mail.ParseAddress(row.CustomerEmail); err != nil {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_email", Message: "must be a valid email"})
		}
		if strings.TrimSpace(row.CustomerName) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "customer_name", Message: "is required"})
		}
		if strings.TrimSpace(row.TicketTypeID) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "ticket_type_id", Message: "is required"})
		}
		if row.Quantity <= 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "quantity", Message: "must be greater than zero"})
		}
		if row.PaymentMethod != "cash" && row.PaymentMethod != "transfer" {
			fields = append(fields, platform.FieldError{Field: prefix + "payment_method", Message: "must be 'cash' or 'transfer'"})
		}
		var soldAt time.Time
		if strings.TrimSpace(row.SoldAt) == "" {
			fields = append(fields, platform.FieldError{Field: prefix + "sold_at", Message: "is required"})
		} else if parsed, err := time.Parse(time.RFC3339, row.SoldAt); err != nil {
			fields = append(fields, platform.FieldError{Field: prefix + "sold_at", Message: "must be an ISO 8601 timestamp"})
		} else {
			soldAt = parsed
		}
		if row.AmountCents != nil && *row.AmountCents < 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "amount_cents", Message: "must not be negative"})
		}

		rows = append(rows, service.ImportSaleInput{
			CustomerEmail: strings.TrimSpace(row.CustomerEmail),
			CustomerName:  strings.TrimSpace(row.CustomerName),
			TicketTypeID:  strings.TrimSpace(row.TicketTypeID),
			Quantity:      row.Quantity,
			PaymentMethod: row.PaymentMethod,
			SoldAt:        soldAt,
			AmountCents:   row.AmountCents,
		})
	}

	return fields, rows
}
