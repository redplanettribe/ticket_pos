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
	ImportBatchID             *string
	SoldAt                    time.Time
	RecordedAt                time.Time
	Channel                   string
	Source                    *string
	TicketTypes               []DossierSaleLine
	AmountCents               int
	Currency                  string
	PaymentMethod             *string
	CustomerFirstName         string
	CustomerLastName          string
	TaxIDType                 *string
	TaxIDNumber               *string
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
		var replacedBy, replacedByRef, replaces, importBatch, source, paymentMethod, taxIDType, taxIDNumber sql.NullString
		if err := rows.Scan(
			&s.ID, &s.ConfirmationRef, &s.Status, &reversedAt,
			&replacedBy, &replacedByRef, &replaces, &importBatch,
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
		s.ImportBatchID = dossierString(importBatch)
		s.Source = dossierString(source)
		s.PaymentMethod = dossierString(paymentMethod)
		s.TaxIDType = dossierString(taxIDType)
		s.TaxIDNumber = dossierString(taxIDNumber)
		out = append(out, s)
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
