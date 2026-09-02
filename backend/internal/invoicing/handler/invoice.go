package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/service"
	"github.com/peter/ticket_pos/backend/internal/invoicing/sri"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Tax Invoice endpoints (#454): issue from the form, list, read one,
// preview totals. The shape of a request is refused here, field by field,
// before the service is asked anything — so a mistyped cédula or an empty
// line list never reaches the point where a number could be consumed.

// Bounds on what one invoice may carry. The SRI's schema caps every text at
// 300 characters; a factura with more than a few hundred lines is not one
// the platform issues by hand.
const (
	maxInvoiceLines     = 500
	maxInvoiceTextChars = 300
	defaultPageSize     = 50
	maxPageSize         = 100
)

// recipientBody is the Recipient as entered.
type recipientBody struct {
	TaxIDType string `json:"tax_id_type"`
	TaxID     string `json:"tax_id"`
	LegalName string `json:"legal_name"`
	Address   string `json:"address"`
	Email     string `json:"email"`
}

// lineBody is one line as entered. Quantity is a decimal string with up to
// six decimals; money is integer cents.
type lineBody struct {
	Description    string `json:"description"`
	Quantity       string `json:"quantity"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	DiscountCents  int64  `json:"discount_cents"`
	IVARate        string `json:"iva_rate"`
}

// additionalFieldBody is one operator-entered name/value pair.
type additionalFieldBody struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// issueInvoiceBody is the form as submitted.
type issueInvoiceBody struct {
	Recipient        recipientBody         `json:"recipient"`
	Lines            []lineBody            `json:"lines"`
	PaymentMethod    string                `json:"payment_method"`
	AdditionalFields []additionalFieldBody `json:"additional_fields"`
}

// totalsBody is what the preview takes: the lines alone.
type totalsBody struct {
	Lines []lineBody `json:"lines"`
}

// IssueInvoice issues a Tax Invoice.
//
// @Summary      Issue a Tax Invoice
// @Description  Issues a factura to the Ecuador Issuer's SRI environment from the form (ADR 0059). The Recipient's `tax_id_type` is `ruc`, `cedula` or `passport` (never consumidor final) and `tax_id` is validated exactly as a checkout Tax ID; `legal_name` and `email` are required, `address` optional. Each line has a `description`, a `quantity` (decimal string, up to six decimals), `unit_price_cents`, an optional `discount_cents` and an `iva_rate` of `15`, `0`, `exento` or `no_objeto`. `payment_method` is an SRI forma de pago code, default `20`. Up to 14 `additional_fields` (name/value, 300 chars) may be added — the Recipient's email takes the fifteenth. Totals are computed server-side. Every failing field is named under VALIDATION_FAILED; ISSUER_NOT_FOUND (404), CERTIFICATE_NOT_UPLOADED (409), CERTIFICATE_KEY_NOT_CONFIGURED (503), ISSUER_INCOMPLETE (409) and INVOICE_INVALID (400) are refused before any number is consumed. Otherwise the next secuencial is allocated, the document built and signed, submitted to the SRI and its authorization polled for up to ~15 s; the answer is the invoice as it then stands — `authorized`, `not_authorized`, `rejected` or `pending` — with the SRI's messages verbatim and one attempts row per SRI call. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  issueInvoiceBody  true  "The Recipient, lines, forma de pago and additional fields"
// @Success      201  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Failure      409  {object}  platform.Envelope
// @Failure      503  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices [post]
func (h *Handler) IssueInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body issueInvoiceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	input, fields := validateIssueInvoice(body)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// Who issued comes from the session and nowhere else: the platform is
	// the Issuer of record, and this is who acted for it.
	session, ok := middleware.SessionFromContext(r.Context())
	if !ok {
		_ = platform.WriteUnauthorized(w, reqID, "Missing session token")
		return
	}
	input.IssuedBy = session.Email

	invoice, err := h.svc.IssueInvoice(r.Context(), input)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusCreated, invoice)
}

// PreviewTotals computes the totals of a set of lines.
//
// @Summary      Preview a Tax Invoice's totals
// @Description  Does the factura arithmetic for the lines given — per-rate subtotals, total discount, IVA and total, in cents — exactly as the issued document will carry them, without issuing anything. The form shows these; it never computes its own. Lines are validated as on issue. Platform Operator only.
// @Tags         operator
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body  totalsBody  true  "The lines"
// @Success      200  {object}  openapi.EnvelopeInvoiceTotals
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/totals [post]
func (h *Handler) PreviewTotals(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())

	var body totalsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		_ = platform.WriteInvalidJSON(w, reqID)
		return
	}
	lines, fields := validateLines(body.Lines)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	totals, err := h.svc.PreviewTotals(lines)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, totals)
}

// ListInvoices lists Tax Invoices newest first.
//
// @Summary      List Tax Invoices
// @Description  Returns a page of every Tax Invoice the platform has issued or owes, newest first: the document `kind` (`manual` from the form; `sale` for a Sale Invoice a paid House checkout owed; `credit_note` for its reversal), the printed number (`001-001-000000012`), emission date, Recipient, total, status, country and the environment it was issued under (`test` invoices are badged as such), and — on a `sale` or `credit_note` — the Ticket Sale id and its Sale Confirmation reference. A document still `owed` (ADR 0060) has no number, environment, emission date or signer yet: those are null until the Sale Invoice Drainer signs it. `attention_since` is when a `needs_attention` document was parked, null otherwise. `recipient_warning` is true on an authorized Sale Invoice the SRI warned about — the Recipient's Tax ID does not exist (advertencia 59) or is incorrect (62) — until the document is superseded (ADR 0061); the status is unaffected, and it is always false while SALE_INVOICING_ENABLED is closed. `kind` narrows the page to one document kind; a value that is not `manual`, `sale` or `credit_note` is refused under VALIDATION_FAILED. `status` narrows the page to one status — `owed`, `pending`, `authorized`, `not_authorized`, `rejected`, `needs_attention`, `withdrawn`, `annulled` or `abandoned` — so that, among other things, every number the platform has abandoned (ADR 0068) can be audited; any other value is refused under VALIDATION_FAILED rather than read as "every status". `recipient_warning=true` narrows the page to the documents carrying a Recipient Warning; any other value is refused under VALIDATION_FAILED, and while SALE_INVOICING_ENABLED is closed the filter answers 404 SALE_INVOICING_UNAVAILABLE. `q` narrows the page to the documents matching one case-insensitive substring, matched against the printed number, the Recipient's legal name, the Recipient's Tax ID and the Sale Confirmation reference of the Ticket Sale the document declares — any one of the four is enough. The term is matched literally, so `%` and `_` are those characters and not wildcards; there is no accent folding and no shortcut for a bare sequential number. Blank or absent is every document. `issued_from` and `issued_to` narrow the page to an EMISSION DATE range: inclusive calendar days (`YYYY-MM-DD`) compared against the emission date alone — the day in the Issuer's country the document itself carries, the one this list's date column shows — with no timezone conversion, and either bound may be given on its own. A document with NO emission date, an owed Sale Invoice the Drainer has not signed, does not match once either bound is given, so a fiscal period never counts a document the Tax Authority has not seen; that is deliberately different from the default order, which keeps un-issued documents at the newest end. A malformed date, or an `issued_from` after the `issued_to`, is refused under VALIDATION_FAILED rather than answered with an empty list. Every filter combines. Response is the ADR-0006 nested envelope { data, pagination }; page_size defaults to 50 (max 100). Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        kind               query  string  false  "Document kind: manual, sale or credit_note (default every kind)"
// @Param        status             query  string  false  "Document status: owed, pending, authorized, not_authorized, rejected, needs_attention, withdrawn, annulled or abandoned (default every status)"
// @Param        recipient_warning  query  string  false  "true to list only the documents carrying a Recipient Warning"
// @Param        q                  query  string  false  "Case-insensitive substring matched against the printed number, the Recipient legal name, the Recipient Tax ID and the Sale Confirmation reference (default every document)"
// @Param        issued_from        query  string  false  "Emission Date range start (YYYY-MM-DD, the Issuer's country's calendar day, inclusive); documents with no Emission Date do not match"
// @Param        issued_to          query  string  false  "Emission Date range end (YYYY-MM-DD, the Issuer's country's calendar day, inclusive); documents with no Emission Date do not match"
// @Param        page       query  int     false  "Page number (default 1)"
// @Param        page_size  query  int     false  "Page size (default 50, max 100)"
// @Success      200  {object}  openapi.EnvelopeInvoiceList
// @Failure      400  {object}  platform.Envelope
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices [get]
func (h *Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	query := r.URL.Query()
	kind, fields := kindParam(query.Get("kind"))
	status, statusFields := statusParam(query.Get("status"))
	fields = append(fields, statusFields...)
	warned, warnedFields := recipientWarningParam(query.Get("recipient_warning"))
	fields = append(fields, warnedFields...)
	issuedFrom, issuedTo, issuedFields := issuedRangeParams(query.Get("issued_from"), query.Get("issued_to"))
	fields = append(fields, issuedFields...)
	if len(fields) > 0 {
		_ = platform.WriteValidationError(w, reqID, fields)
		return
	}
	// `q` is trimmed and never validated: a search term is whatever was
	// pasted, and the one thing that could go wrong with it — a LIKE
	// metacharacter read as a pattern — is the repository's escape to
	// prevent, not a reason to refuse an operator's own typing (#595).
	// Trimmed to blank is no search, so an empty box is the whole list.
	search := strings.TrimSpace(query.Get("q"))
	filter := service.InvoiceFilter{
		Kind:             kind,
		Status:           status,
		RecipientWarning: warned,
		Search:           search,
		IssuedFrom:       issuedFrom,
		IssuedTo:         issuedTo,
	}
	list, err := h.svc.ListInvoices(r.Context(), filter, pageParam(query.Get("page")), pageSizeParam(query.Get("page_size")))
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, list)
}

// GetInvoice returns one Tax Invoice in full.
//
// @Summary      Get a Tax Invoice
// @Description  Returns one Tax Invoice in full: its kind, Recipient, the Issuer as snapshotted at issue time, lines with their arithmetic, totals, status, the SRI's last messages verbatim (identifier, message, additional information, type), the clave de acceso and — once authorized — the authorization number and date, and the attempts ledger with one row per SRI call (operation, outcome, messages, duration). A `sale` or `credit_note` document also carries its Ticket Sale id and Sale Confirmation reference, the IVA rate it was priced under, when it was delivered to the buyer and when the Drainer next works it; a `credit_note` names the Sale Invoice it credits and the reversal route. On a document still `owed` the `issuer` and `ecuador` objects and every issue fact are null. INVOICE_NOT_FOUND (404) otherwise. Platform Operator only.
// @Tags         operator
// @Produce      json
// @Security     BearerAuth
// @Param        id  path  string  true  "Tax Invoice id"
// @Success      200  {object}  openapi.EnvelopeInvoiceDetail
// @Failure      401  {object}  platform.Envelope
// @Failure      403  {object}  platform.Envelope
// @Failure      404  {object}  platform.Envelope
// @Router       /api/v1/operator/invoicing/invoices/{id} [get]
func (h *Handler) GetInvoice(w http.ResponseWriter, r *http.Request) {
	reqID := platform.RequestID(r.Context())
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		_ = platform.WriteDomainError(w, reqID, invoicing.ErrInvoiceNotFound())
		return
	}
	invoice, err := h.svc.GetInvoice(r.Context(), id)
	if err != nil {
		_ = platform.WriteDomainError(w, reqID, err)
		return
	}
	_ = platform.WriteSuccess(w, reqID, http.StatusOK, invoice)
}

// validateIssueInvoice checks the whole form at once and names every failing
// field, so an operator who mistyped three things is told three times rather
// than made to press Issue three times.
func validateIssueInvoice(body issueInvoiceBody) (service.IssueInput, []platform.FieldError) {
	var fields []platform.FieldError
	var input service.IssueInput

	r := body.Recipient
	input.Recipient = invoicing.Recipient{
		TaxIDType: strings.TrimSpace(r.TaxIDType),
		TaxID:     strings.TrimSpace(r.TaxID),
		LegalName: strings.TrimSpace(r.LegalName),
		Address:   strings.TrimSpace(r.Address),
		Email:     strings.TrimSpace(r.Email),
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
	switch {
	case input.Recipient.Email == "":
		fields = append(fields, required("recipient.email"))
	case !isEmail(input.Recipient.Email) || utf8.RuneCountInString(input.Recipient.Email) > maxInvoiceTextChars:
		fields = append(fields, platform.FieldError{Field: "recipient.email", Code: platform.CodeInvalidEmail, Message: "must be a valid email address"})
	default:
		input.Recipient.Email = platform.NormalizeEmail(input.Recipient.Email)
	}

	lines, lineErrs := validateLines(body.Lines)
	fields = append(fields, lineErrs...)
	input.Lines = lines

	input.PaymentMethod = strings.TrimSpace(body.PaymentMethod)
	if input.PaymentMethod == "" {
		input.PaymentMethod = string(sri.PaymentMethodDefault)
	} else if sri.PaymentMethodLabel(sri.PaymentMethod(input.PaymentMethod)) == "" {
		fields = append(fields, platform.FieldError{Field: "payment_method", Code: platform.CodeInvalidPaymentMethod, Message: "must be an SRI forma de pago code"})
	}

	if len(body.AdditionalFields) > invoicing.MaxOperatorAdditionalFields {
		fields = append(fields, platform.FieldError{
			Field:   "additional_fields",
			Code:    platform.CodeTooManyItems,
			Message: fmt.Sprintf("at most %d additional fields may be added; the Recipient's email takes the fifteenth", invoicing.MaxOperatorAdditionalFields),
		})
	}
	for i, f := range body.AdditionalFields {
		name := strings.TrimSpace(f.Name)
		value := strings.TrimSpace(f.Value)
		fields = appendText(fields, fmt.Sprintf("additional_fields[%d].name", i), name, true)
		fields = appendText(fields, fmt.Sprintf("additional_fields[%d].value", i), value, true)
		input.AdditionalFields = append(input.AdditionalFields, invoicing.AdditionalField{Position: i + 1, Name: name, Value: value})
	}

	return input, fields
}

// validateLines checks the lines: at least one, each with a description, a
// positive quantity of up to six decimals, non-negative money, a discount no
// larger than the line, and a known IVA rate.
func validateLines(body []lineBody) ([]service.LineInput, []platform.FieldError) {
	var fields []platform.FieldError
	if len(body) == 0 {
		return nil, []platform.FieldError{{Field: "lines", Code: platform.CodeEmptyCollection, Message: "at least one line is required"}}
	}
	if len(body) > maxInvoiceLines {
		return nil, []platform.FieldError{{Field: "lines", Code: platform.CodeTooManyItems, Message: fmt.Sprintf("at most %d lines", maxInvoiceLines)}}
	}
	lines := make([]service.LineInput, 0, len(body))
	for i, l := range body {
		prefix := fmt.Sprintf("lines[%d].", i)
		line := service.LineInput{
			Description:    strings.TrimSpace(l.Description),
			UnitPriceCents: l.UnitPriceCents,
			DiscountCents:  l.DiscountCents,
			IVARate:        invoicing.IVARate(strings.TrimSpace(l.IVARate)),
		}
		fields = appendText(fields, prefix+"description", line.Description, true)
		q, err := sri.ParseQuantity(strings.TrimSpace(l.Quantity))
		switch {
		case strings.TrimSpace(l.Quantity) == "":
			fields = append(fields, required(prefix+"quantity"))
		case err != nil || q <= 0:
			fields = append(fields, platform.FieldError{Field: prefix + "quantity", Code: platform.CodeInvalidPositiveInt, Message: "must be a positive number with at most six decimals"})
		default:
			line.QuantityMillionths = int64(q)
		}
		if l.UnitPriceCents < 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "unit_price_cents", Code: platform.CodeInvalidNonNegativeInt, Message: "must be zero or more"})
		}
		if l.DiscountCents < 0 {
			fields = append(fields, platform.FieldError{Field: prefix + "discount_cents", Code: platform.CodeInvalidNonNegativeInt, Message: "must be zero or more"})
		}
		switch {
		case line.IVARate == "":
			fields = append(fields, required(prefix+"iva_rate"))
		case !line.IVARate.Valid():
			fields = append(fields, platform.FieldError{Field: prefix + "iva_rate", Code: platform.CodeInvalidEnum, Message: "must be one of 15, 0, exento, no_objeto"})
		}
		lines = append(lines, line)
	}
	return lines, fields
}

// isEmail is the same test the staff sign-in applies to an address.
func isEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

func required(field string) platform.FieldError {
	return platform.FieldError{Field: field, Code: platform.CodeRequired, Message: "is required"}
}

func appendText(fields []platform.FieldError, field, value string, isRequired bool) []platform.FieldError {
	switch {
	case value == "" && isRequired:
		return append(fields, required(field))
	case utf8.RuneCountInString(value) > maxInvoiceTextChars:
		return append(fields, platform.FieldError{Field: field, Code: platform.CodeTooLong, Message: fmt.Sprintf("must be at most %d characters", maxInvoiceTextChars)})
	case strings.ContainsRune(value, '\n'):
		return append(fields, platform.FieldError{Field: field, Code: platform.CodeTooLong, Message: "must be a single line"})
	}
	return fields
}

// recipientWarningParam parses the `recipient_warning` query value (#482):
// absent is every document, `true` is the warned ones alone, and anything
// else is refused by name for the reason kindParam refuses a mistyped kind.
func recipientWarningParam(raw string) (bool, []platform.FieldError) {
	switch strings.TrimSpace(raw) {
	case "":
		return false, nil
	case "true":
		return true, nil
	}
	return false, []platform.FieldError{{
		Field:   "recipient_warning",
		Code:    "invalid",
		Message: "must be true when given",
	}}
}

// statusParam parses the `status` query value (#578, ADR 0068): absent is
// every status, and anything but one of the nine is refused by name for the
// reason kindParam refuses a mistyped kind — a filter the API quietly
// ignores shows the operator a list that is not the list they asked for.
// It exists so that every number the platform has given up on can be
// audited (`abandoned`), and admits the whole vocabulary because a filter
// that answers one status and not the eight beside it is one the next
// person has to extend again.
func statusParam(raw string) (invoicing.InvoiceStatus, []platform.FieldError) {
	status := invoicing.InvoiceStatus(strings.TrimSpace(raw))
	if status == "" {
		return "", nil
	}
	for _, s := range invoicing.InvoiceStatuses {
		if status == s {
			return status, nil
		}
	}
	names := make([]string, 0, len(invoicing.InvoiceStatuses))
	for _, s := range invoicing.InvoiceStatuses {
		names = append(names, string(s))
	}
	return "", []platform.FieldError{{
		Field:   "status",
		Code:    platform.CodeInvalidEnum,
		Message: "must be one of " + strings.Join(names, ", "),
	}}
}

// issuedRangeParams parses the `issued_from`/`issued_to` query values into
// the Emission Date range (#596, spec #593): two inclusive calendar days,
// each "YYYY-MM-DD", either of which may be absent for an open bound.
//
// The Sales list's sold_from/sold_to is the prior art, down to the code and
// the message (its validateDate, which is unexported in its own handler
// package; there is no shared parser to reuse, and catalog's Holder List
// carries its own copy of the same three lines for the same reason). What is
// NOT copied is the interpretation: the Sales list reads its days in the
// Event's timezone because sold_at is an instant, while issued_on is already
// a calendar day in the Issuer's country, so nothing here converts anything.
//
// A MALFORMED DATE AND AN INVERTED RANGE ARE BOTH REFUSED rather than
// answered with an empty list: "no document was emitted in that window" and
// "that window is not a window" are different facts, and a silent empty page
// sends an operator looking for documents that are there. The inversion is
// blamed on `issued_from` (CodeStartAfterEnd names the start of a range),
// and only when both bounds parsed — one field error per thing wrong with
// the request, never a second one derived from a value already refused.
func issuedRangeParams(rawFrom, rawTo string) (string, string, []platform.FieldError) {
	var fields []platform.FieldError
	from := issuedDateParam(rawFrom, "issued_from", &fields)
	to := issuedDateParam(rawTo, "issued_to", &fields)
	if from != "" && to != "" && from > to {
		// Lexicographic on YYYY-MM-DD is chronological, which is the whole
		// reason the wire format is that one.
		fields = append(fields, platform.FieldError{
			Field:   "issued_from",
			Code:    platform.CodeStartAfterEnd,
			Message: "must be on or before issued_to",
		})
	}
	return from, to, fields
}

// issuedDateParam trims one bound and checks it is a calendar day. Blank is
// an open bound and returns "".
func issuedDateParam(raw, field string, fields *[]platform.FieldError) string {
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

// kindParam parses the `kind` query value: absent is every kind, and
// anything but the three kinds is refused by name rather than read as
// "every kind", so a mistyped filter never shows the operator a list that
// quietly ignores it.
func kindParam(raw string) (invoicing.DocumentKind, []platform.FieldError) {
	kind := invoicing.DocumentKind(strings.TrimSpace(raw))
	switch kind {
	case "", invoicing.DocumentKindManual, invoicing.DocumentKindSale, invoicing.DocumentKindCreditNote:
		return kind, nil
	}
	return "", []platform.FieldError{{
		Field:   "kind",
		Code:    platform.CodeInvalidEnum,
		Message: "must be one of " + string(invoicing.DocumentKindManual) + ", " + string(invoicing.DocumentKindSale) + ", " + string(invoicing.DocumentKindCreditNote),
	}}
}

func pageParam(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

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
