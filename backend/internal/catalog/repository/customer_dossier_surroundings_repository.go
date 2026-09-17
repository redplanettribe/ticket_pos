package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DossierSaleSurroundings is what surrounds one Dossier Sale (#639): the phone
// given on its checkout, the Affiliate Link that attributed it, and when it was
// re-addressed. Its Sale Invoices are the invoicing module's answer and are not
// read here.
type DossierSaleSurroundings struct {
	// Phone is the number given on the checkout this Sale came from, read off
	// that Payment (ADR 0073) — never the Customer record's current phone. Nil
	// for a Sale with no Payment, or a checkout that gave none.
	Phone *string
	// AffiliateLinkName is the display name of the link that attributed the
	// Sale, or nil.
	AffiliateLinkName *string
	// ReAddressedAt is when the Sale's most recent accepted Sale Re-addressing
	// was accepted (ADR 0058), or nil. Neither address and no token is read.
	ReAddressedAt *time.Time
}

// ListDossierSaleSurroundings returns the surroundings of each named Ticket
// Sale of the Event in the Organization, keyed by Sale id. The ids are the ones
// ListDossierSales already scoped, and the scope is re-applied here so a stray
// id from elsewhere is never read; a Sale with nothing around it still gets an
// entry.
func (r *Repository) ListDossierSaleSurroundings(ctx context.Context, organizationID, eventID string, ticketSaleIDs []string) (map[string]DossierSaleSurroundings, error) {
	out := make(map[string]DossierSaleSurroundings, len(ticketSaleIDs))
	if len(ticketSaleIDs) == 0 {
		return out, nil
	}
	// A Sale is linked from at most the one Payment that became it; the order
	// only makes the choice deterministic should that ever not hold, preferring
	// the approved Payment.
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT
			ts.id,
			(SELECT p.customer_phone FROM payments p
			  WHERE p.ticket_sale_id = ts.id
			  ORDER BY (p.status = 'approved') DESC, p.created_at DESC, p.id DESC
			  LIMIT 1),
			al.name,
			(SELECT MAX(ra.accepted_at) FROM sale_re_addressings ra
			  WHERE ra.ticket_sale_id = ts.id AND ra.accepted_at IS NOT NULL)
		FROM ticket_sales ts
		LEFT JOIN affiliate_links al ON al.id = ts.affiliate_link_id
		WHERE ts.id = ANY($1)
		  AND ts.organization_id = $2
		  AND ts.event_id = $3
	`, ticketSaleIDs, organizationID, eventID)
	if err != nil {
		return nil, fmt.Errorf("dossier sale surroundings: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var phone, affiliate sql.NullString
		var reAddressedAt sql.NullTime
		if err := rows.Scan(&id, &phone, &affiliate, &reAddressedAt); err != nil {
			return nil, fmt.Errorf("dossier sale surroundings scan: %w", err)
		}
		s := DossierSaleSurroundings{
			Phone:             dossierString(phone),
			AffiliateLinkName: dossierString(affiliate),
		}
		if reAddressedAt.Valid {
			t := reAddressedAt.Time
			s.ReAddressedAt = &t
		}
		out[id] = s
	}
	return out, rows.Err()
}
