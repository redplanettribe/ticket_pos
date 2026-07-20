// Package handler exposes HTTP endpoints for the sales domain.
// Sales covers online, in-person, and import sales plus capacity logic.
package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/identity/middleware"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/service"
)

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

// CommitDirectSaleImport records a batch of Direct Sales for an Event.
//
// @Summary      Commit a Direct Sale Import
// @Description  Records off-platform (cash/transfer) sales against an Event, decrementing capacity and emailing each customer a Sale Confirmation. All-or-nothing and idempotent.
// @Tags         staff
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        id    path      string            true  "Event ID"
// @Param        body  body      commitImportBody  true  "Sale import batch"
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

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	_ = platform.WriteSuccess(w, reqID, status, result)
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
