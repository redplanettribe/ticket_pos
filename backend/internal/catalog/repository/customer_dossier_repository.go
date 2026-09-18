package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// The reads behind the Customer Dossier (#638, spec #635): everything one Event
// knows about one Customer.
//
// EVENT-SCOPED VALUES ONLY. From the `customers` row this reads the id and the
// email identity and NOTHING ELSE — its name, Tax ID, phone, avatar and consent
// fields are what the person last told the platform, possibly at another
// Organization, and never what they told this Event. Every other value comes
// off this Event's own Ticket Sale snapshots.

// DossierCustomer is the Customer's identity as the Dossier may show it.
type DossierCustomer struct {
	ID    string
	Email string
}

// DossierSaleLine is one Ticket Type bought on a Dossier Sale, with its quantity.
type DossierSaleLine struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

// DossierSale is one of this Event's Ticket Sales to the Customer, reversed or
// not, as the Sale recorded it.
type DossierSale struct {
	ID                        string
	ConfirmationRef           string
	Status                    string
	ReversedAt                *time.Time
	ReplacedBySaleID          *string
	ReplacedByConfirmationRef *string
	ReplacesSaleID            *string
	ReplacesConfirmationRef   *string
	// ReplacementReason is WHY the two Sales are linked (migration 123): a Sale
	// Correction or an Upgrade. Read beside the link because the link alone can
	// no longer say — see sales.ReplacementReason* and dossierSaleStatus. Null
	// exactly when both link columns are, which the column's CHECK enforces.
	ReplacementReason *string
	ImportBatchID     *string
	SoldAt            time.Time
	RecordedAt        time.Time
	Channel           string
	Source            *string
	TicketTypes       []DossierSaleLine
	AmountCents       int
	Currency          string
	PaymentMethod     *string
	CustomerFirstName string
	CustomerLastName  string
	TaxIDType         *string
	TaxIDNumber       *string
}

// GetDossierCustomer returns the Customer's identity, or nil when the id names
// no Customer.
func (r *Repository) GetDossierCustomer(ctx context.Context, customerID string) (*DossierCustomer, error) {
	var c DossierCustomer
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email FROM customers WHERE id = $1
	`, customerID).Scan(&c.ID, &c.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dossier customer: %w", err)
	}
	return &c, nil
}

// ListDossierSales returns every Ticket Sale of this Event (inside this
// Organization) made to the Customer, reversed ones included, newest first.
//
// The amount is the sum of the Sale's lines at their snapshotted unit prices,
// the same sum the Sales list states for a row.
func (r *Repository) ListDossierSales(ctx context.Context, orgID, eventID, customerID string) ([]DossierSale, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			ts.id,
			ts.confirmation_ref,
			ts.status,
			ts.reversed_at,
			ts.replaced_by_sale_id,
			(SELECT confirmation_ref FROM ticket_sales r WHERE r.id = ts.replaced_by_sale_id),
			ts.replaces_sale_id,
			(SELECT confirmation_ref FROM ticket_sales p WHERE p.id = ts.replaces_sale_id),
			ts.replacement_reason,
			ts.import_batch_id,
			ts.sold_at,
			ts.created_at,
			ts.channel,
			ts.source,
			lines.ticket_types,
			lines.amount_cents,
			org.currency,
			ts.payment_method,
			ts.customer_first_name,
			ts.customer_last_name,
			ts.customer_tax_id_type,
			ts.customer_tax_id_number
		FROM ticket_sales ts
		JOIN organizations org ON org.id = ts.organization_id
		JOIN LATERAL (
			SELECT
				COALESCE(SUM(tsl.quantity * tsl.unit_price_cents), 0) AS amount_cents,
				COALESCE(
					json_agg(
						json_build_object('ticket_type_id', tt.id, 'ticket_type_name', tt.name, 'quantity', tsl.quantity)
						ORDER BY tt.sort_order, tt.name
					),
					'[]'::json
				) AS ticket_types
			FROM ticket_sale_lines tsl
			JOIN ticket_types tt ON tt.id = tsl.ticket_type_id
			WHERE tsl.ticket_sale_id = ts.id
		) lines ON TRUE
		WHERE ts.event_id = $1 AND ts.organization_id = $2 AND ts.customer_id = $3
		ORDER BY ts.sold_at DESC, ts.id DESC
	`, eventID, orgID, customerID)
	if err != nil {
		return nil, fmt.Errorf("dossier sales: %w", err)
	}
	defer rows.Close()

	var out []DossierSale
	for rows.Next() {
		var s DossierSale
		var typesJSON []byte
		var reversedAt sql.NullTime
		var replacedBy, replacedByRef, replaces, replacesRef, replacementReason sql.NullString
		var importBatch, source, paymentMethod, taxIDType, taxIDNumber sql.NullString
		if err := rows.Scan(
			&s.ID, &s.ConfirmationRef, &s.Status, &reversedAt,
			&replacedBy, &replacedByRef, &replaces, &replacesRef, &replacementReason, &importBatch,
			&s.SoldAt, &s.RecordedAt, &s.Channel, &source,
			&typesJSON, &s.AmountCents, &s.Currency, &paymentMethod,
			&s.CustomerFirstName, &s.CustomerLastName, &taxIDType, &taxIDNumber,
		); err != nil {
			return nil, fmt.Errorf("dossier sales scan: %w", err)
		}
		if err := json.Unmarshal(typesJSON, &s.TicketTypes); err != nil {
			return nil, fmt.Errorf("dossier sale lines: %w", err)
		}
		if reversedAt.Valid {
			t := reversedAt.Time
			s.ReversedAt = &t
		}
		s.ReplacedBySaleID = dossierString(replacedBy)
		s.ReplacedByConfirmationRef = dossierString(replacedByRef)
		s.ReplacesSaleID = dossierString(replaces)
		s.ReplacesConfirmationRef = dossierString(replacesRef)
		s.ReplacementReason = dossierString(replacementReason)
		s.ImportBatchID = dossierString(importBatch)
		s.Source = dossierString(source)
		s.PaymentMethod = dossierString(paymentMethod)
		s.TaxIDType = dossierString(taxIDType)
		s.TaxIDNumber = dossierString(taxIDNumber)
		out = append(out, s)
	}
	return out, rows.Err()
}

// --- Tickets and Holders (#640) ----------------------------------------------
//
// THE HOLDER COLUMNS ARE REPORTED, NOT DISCLOSED. These reads hand the service
// the raw assignment columns of each Ticket, exactly as HolderTicket does for the
// Holder List, and what an Organization is told about them is decided once, in
// catalog.DiscloseHolder. The one filter over Holders here — "held by this
// Customer" — is `holder_customer_id = customer AND accepted_at IS NOT NULL`,
// which is that rule's `accepted` read through its query-side twin: a Customer
// id is only ever on an accepted Ticket (migration 080), and only an accepted
// Holder is disclosed.

// DossierTicket is one Ticket of a Dossier Sale with its raw assignment columns.
type DossierTicket struct {
	ID             string
	TicketSaleID   string
	Ordinal        int
	TicketTypeID   string
	TicketTypeName string

	HolderEmail           sql.NullString
	HolderCustomerID      sql.NullString
	AssignedAt            sql.NullTime
	AcceptedAt            sql.NullTime
	HolderAddressPurgedAt sql.NullTime
	// The accepted Holder's name, from the Customer their acceptance minted or
	// matched — the Holder List's own source (holderRosterHolderJoin).
	HolderFirstName sql.NullString
	HolderLastName  sql.NullString
}

// ListDossierSaleTickets returns every Ticket of the given Sales, re-scoped to
// this Event of this Organization, grouped by Sale in the catalog's order and
// then by ordinal — the order every list of a Sale's Tickets is read in.
//
// IT IS NOT THE SELF-HELD TICKET'S ORDER and no longer claims to be: since ADR
// 0074 the buyer is seated on the Sale's dearest line, so the Ticket that is
// theirs may sit anywhere in this list. The Dossier says which one it is by
// reading the holder columns, never by position.
func (r *Repository) ListDossierSaleTickets(ctx context.Context, orgID, eventID string, saleIDs []string) ([]DossierTicket, error) {
	if len(saleIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, s.id, tk.ordinal, tt.id, tt.name,
		       tk.holder_email, tk.holder_customer_id, tk.assigned_at, tk.accepted_at, tk.holder_address_purged_at,
		       hc.first_name, hc.last_name
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
	`+holderRosterHolderJoin+`
		WHERE s.event_id = $1 AND s.organization_id = $2 AND s.id = ANY($3)
		ORDER BY s.id, tt.sort_order, tt.name COLLATE "C", l.ticket_type_id, tk.ordinal
	`, eventID, orgID, saleIDs)
	if err != nil {
		return nil, fmt.Errorf("dossier sale tickets: %w", err)
	}
	defer rows.Close()

	var out []DossierTicket
	for rows.Next() {
		var tk DossierTicket
		if err := rows.Scan(
			&tk.ID, &tk.TicketSaleID, &tk.Ordinal, &tk.TicketTypeID, &tk.TicketTypeName,
			&tk.HolderEmail, &tk.HolderCustomerID, &tk.AssignedAt, &tk.AcceptedAt, &tk.HolderAddressPurgedAt,
			&tk.HolderFirstName, &tk.HolderLastName,
		); err != nil {
			return nil, fmt.Errorf("dossier sale tickets scan: %w", err)
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

// DossierHeldTicket is a Ticket the Customer accepted on somebody else's Sale
// of this Event, with that Sale's standing and the buyer's name as given on it.
type DossierHeldTicket struct {
	DossierTicket
	ConfirmationRef string
	SaleStatus      string
	BuyerFirstName  string
	BuyerLastName   string
}

// ListDossierHeldTickets returns the Tickets of this Event the Customer has
// accepted on Sales made to somebody else — reversed Sales included, so a Holder
// whose Sale was reversed is still found — newest Sale first.
//
// Their own Sales' Tickets are excluded: those are listed under the Sale.
func (r *Repository) ListDossierHeldTickets(ctx context.Context, orgID, eventID, customerID string) ([]DossierHeldTicket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, s.id, tk.ordinal, tt.id, tt.name,
		       tk.holder_email, tk.holder_customer_id, tk.assigned_at, tk.accepted_at, tk.holder_address_purged_at,
		       hc.first_name, hc.last_name,
		       s.confirmation_ref, s.status, s.customer_first_name, s.customer_last_name
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
	`+holderRosterHolderJoin+`
		WHERE s.event_id = $1 AND s.organization_id = $2
		  AND tk.holder_customer_id = $3 AND tk.accepted_at IS NOT NULL
		  AND s.customer_id <> $3
		ORDER BY s.sold_at DESC, s.id DESC, tt.sort_order, tt.name COLLATE "C", l.ticket_type_id, tk.ordinal
	`, eventID, orgID, customerID)
	if err != nil {
		return nil, fmt.Errorf("dossier held tickets: %w", err)
	}
	defer rows.Close()

	var out []DossierHeldTicket
	for rows.Next() {
		var tk DossierHeldTicket
		if err := rows.Scan(
			&tk.ID, &tk.TicketSaleID, &tk.Ordinal, &tk.TicketTypeID, &tk.TicketTypeName,
			&tk.HolderEmail, &tk.HolderCustomerID, &tk.AssignedAt, &tk.AcceptedAt, &tk.HolderAddressPurgedAt,
			&tk.HolderFirstName, &tk.HolderLastName,
			&tk.ConfirmationRef, &tk.SaleStatus, &tk.BuyerFirstName, &tk.BuyerLastName,
		); err != nil {
			return nil, fmt.Errorf("dossier held tickets scan: %w", err)
		}
		out = append(out, tk)
	}
	return out, rows.Err()
}

// DossierAssignmentReminder is one Assignment Reminder sent about a Sale.
type DossierAssignmentReminder struct {
	TicketSaleID string
	SentAt       time.Time
}

// ListDossierAssignmentReminders returns when an Assignment Reminder was sent
// about each of the given Sales (the `assignment_reminders` ledger), oldest
// first, re-scoped to this Event of this Organization.
func (r *Repository) ListDossierAssignmentReminders(ctx context.Context, orgID, eventID string, saleIDs []string) ([]DossierAssignmentReminder, error) {
	if len(saleIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ar.ticket_sale_id, ar.sent_at
		FROM assignment_reminders ar
		JOIN ticket_sales s ON s.id = ar.ticket_sale_id
		WHERE s.event_id = $1 AND s.organization_id = $2 AND s.id = ANY($3)
		ORDER BY ar.sent_at ASC, ar.id ASC
	`, eventID, orgID, saleIDs)
	if err != nil {
		return nil, fmt.Errorf("dossier assignment reminders: %w", err)
	}
	defer rows.Close()

	var out []DossierAssignmentReminder
	for rows.Next() {
		var reminder DossierAssignmentReminder
		if err := rows.Scan(&reminder.TicketSaleID, &reminder.SentAt); err != nil {
			return nil, fmt.Errorf("dossier assignment reminders scan: %w", err)
		}
		out = append(out, reminder)
	}
	return out, rows.Err()
}

// DossierAnswerReminder is when an Answer Reminder was last sent about a Ticket.
type DossierAnswerReminder struct {
	TicketID   string
	LastSentAt time.Time
}

// ListDossierLastAnswerReminders returns, for each of the given Tickets that
// has ever been chased, when the last Answer Reminder about it was sent (the
// `answer_reminders` ledger, one row per Ticket a mail covered), re-scoped to
// this Event of this Organization (#641). A Ticket never chased has no row.
func (r *Repository) ListDossierLastAnswerReminders(ctx context.Context, orgID, eventID string, ticketIDs []string) ([]DossierAnswerReminder, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ar.ticket_id, MAX(ar.sent_at)
		FROM answer_reminders ar
		JOIN tickets tk ON tk.id = ar.ticket_id
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1 AND s.organization_id = $2 AND ar.ticket_id = ANY($3)
		GROUP BY ar.ticket_id
	`, eventID, orgID, ticketIDs)
	if err != nil {
		return nil, fmt.Errorf("dossier answer reminders: %w", err)
	}
	defer rows.Close()

	var out []DossierAnswerReminder
	for rows.Next() {
		var reminder DossierAnswerReminder
		if err := rows.Scan(&reminder.TicketID, &reminder.LastSentAt); err != nil {
			return nil, fmt.Errorf("dossier answer reminders scan: %w", err)
		}
		out = append(out, reminder)
	}
	return out, rows.Err()
}

// dossierString turns a nullable column into the pointer the Dossier carries.
func dossierString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
