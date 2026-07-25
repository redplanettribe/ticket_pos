package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Payments: the persistence of a Customer's attempt to pay through a Payment
// Provider (ADR 0012). A Payment begins 'pending' at begin-checkout and settles
// 'approved' — in the same transaction that commits its Ticket Sale through the
// shared CommitSales spine — or 'failed'. 'expired' is the lazy fate of
// abandoned pendings (ADR 0013, not written by anything yet).

// CheckoutEvent is what beginning a checkout needs to know about the Event: its
// identity, whether it is sellable at all, and the Organization currency the
// amount is denominated in.
type CheckoutEvent struct {
	ID             string
	OrganizationID string
	Name           string
	Status         string
	Currency       string
}

// GetCheckoutEvent resolves an Event by Organization and Event slug for the
// public checkout, returning nil when neither slug matches. Status is returned
// rather than filtered so the caller owns the "published only" rule.
func (r *Repository) GetCheckoutEvent(ctx context.Context, orgSlug, eventSlug string) (*CheckoutEvent, error) {
	var e CheckoutEvent
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.id, e.organization_id, e.name, e.status, o.currency
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE o.slug = $1 AND e.slug = $2
	`, orgSlug, eventSlug).Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Status, &e.Currency)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// PaymentLine is one Ticket Type, quantity, and unit-price snapshot on a
// Payment — what the approved sale's Ticket Sale Lines are written from.
type PaymentLine struct {
	TicketTypeID   string
	Quantity       int
	UnitPriceCents int
}

// CreatePaymentInput is a pending Payment to record at begin-checkout: the
// line/price snapshot, the amount they sum to, and the checkout identity as
// entered.
type CreatePaymentInput struct {
	EventID             string
	OrganizationID      string
	Provider            string
	ClientTransactionID string
	AmountCents         int
	CustomerEmail       string
	CustomerFirstName   string
	CustomerLastName    string
	Lines               []PaymentLine
	Now                 time.Time
}

// CreatePayment records a pending Payment and its line snapshot atomically,
// returning the Payment id.
func (r *Repository) CreatePayment(ctx context.Context, in CreatePaymentInput) (string, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var paymentID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO payments (
			event_id, organization_id, provider, client_transaction_id,
			status, amount_cents, customer_email, customer_first_name, customer_last_name,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $9, $9)
		RETURNING id
	`, in.EventID, in.OrganizationID, in.Provider, in.ClientTransactionID,
		in.AmountCents, in.CustomerEmail, in.CustomerFirstName, in.CustomerLastName, in.Now).Scan(&paymentID)
	if err != nil {
		return "", err
	}

	for _, line := range in.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO payment_lines (payment_id, ticket_type_id, quantity, unit_price_cents, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, paymentID, line.TicketTypeID, line.Quantity, line.UnitPriceCents, in.Now); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return paymentID, nil
}

// SetPaymentProviderTransactionID records the provider's id for a Payment once
// the provider has assigned one (at initiation for providers that do). A blank
// id is a no-op.
func (r *Repository) SetPaymentProviderTransactionID(ctx context.Context, clientTransactionID, providerTransactionID string, now time.Time) error {
	if providerTransactionID == "" {
		return nil
	}
	_, err := r.db.Pool.ExecContext(ctx, `
		UPDATE payments SET provider_transaction_id = $2, updated_at = $3
		WHERE client_transaction_id = $1
	`, clientTransactionID, providerTransactionID, now)
	return err
}

// Payment is one payment attempt as stored, with — when it produced one — the
// confirmation reference of its Ticket Sale joined in for the idempotent
// re-confirm read.
type Payment struct {
	ID                  string
	EventID             string
	OrganizationID      string
	Provider            string
	ClientTransactionID string
	Status              string
	AmountCents         int
	CustomerEmail       string
	CustomerFirstName   string
	CustomerLastName    string
	// TicketSaleID and ConfirmationRef are set only for an approved Payment
	// whose sale was recorded; an approved Payment without them is the
	// loudly-logged commit-failure incident.
	TicketSaleID    string
	ConfirmationRef string
}

// GetPaymentByClientTransactionID returns the Payment carrying the given client
// transaction id, or nil when none does.
func (r *Repository) GetPaymentByClientTransactionID(ctx context.Context, clientTransactionID string) (*Payment, error) {
	var p Payment
	var ticketSaleID, confirmationRef sql.NullString
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT p.id, p.event_id, p.organization_id, p.provider, p.client_transaction_id,
		       p.status, p.amount_cents, p.customer_email, p.customer_first_name, p.customer_last_name,
		       p.ticket_sale_id, ts.confirmation_ref
		FROM payments p
		LEFT JOIN ticket_sales ts ON ts.id = p.ticket_sale_id
		WHERE p.client_transaction_id = $1
	`, clientTransactionID).Scan(
		&p.ID, &p.EventID, &p.OrganizationID, &p.Provider, &p.ClientTransactionID,
		&p.Status, &p.AmountCents, &p.CustomerEmail, &p.CustomerFirstName, &p.CustomerLastName,
		&ticketSaleID, &confirmationRef,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.TicketSaleID = ticketSaleID.String
	p.ConfirmationRef = confirmationRef.String
	return &p, nil
}

// ApprovePaymentInput settles a provider-approved Payment: the sale to record
// from its snapshot, and the provider's transaction id to keep for support.
type ApprovePaymentInput struct {
	ClientTransactionID   string
	ProviderTransactionID string
	// PaymentMethod is recorded on the Ticket Sale (the Payment Provider that
	// collected the money, e.g. 'payphone').
	PaymentMethod   string
	ConfirmationRef string
	Now             time.Time
	// UpsertCustomer resolves the sale's Customer within the transaction.
	UpsertCustomer UpsertCustomer
}

// ApprovedPayment is the outcome of ApprovePaymentAndCommitSale. When another
// confirm settled the Payment first, AlreadySettled is set and nothing was
// written; the caller re-reads the Payment for the recorded outcome.
type ApprovedPayment struct {
	AlreadySettled bool
	Sale           *RecordedSale
}

// ApprovePaymentAndCommitSale marks a pending Payment approved and commits its
// Ticket Sale through the shared CommitSales spine — row locks, capacity check,
// Customer upsert, sold_count increment — in ONE transaction, then links the
// Payment to the sale it produced. The Payment row is locked FOR UPDATE first,
// so two racing confirms serialize: the loser finds the Payment settled and
// returns AlreadySettled without writing anything.
//
// Any error (a capacity race lost since begin-checkout, most plausibly) rolls
// the whole transaction back, leaving the Payment pending: the caller owns the
// approved-without-sale incident marking.
func (r *Repository) ApprovePaymentAndCommitSale(ctx context.Context, in ApprovePaymentInput) (*ApprovedPayment, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var paymentID, eventID, orgID, status, email, firstName, lastName string
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_id, organization_id, status, customer_email, customer_first_name, customer_last_name
		FROM payments
		WHERE client_transaction_id = $1
		FOR UPDATE
	`, in.ClientTransactionID).Scan(&paymentID, &eventID, &orgID, &status, &email, &firstName, &lastName)
	if err != nil {
		return nil, err
	}
	if status != "pending" {
		return &ApprovedPayment{AlreadySettled: true}, nil
	}

	lineRows, err := tx.QueryContext(ctx, `
		SELECT ticket_type_id, quantity, unit_price_cents
		FROM payment_lines
		WHERE payment_id = $1
	`, paymentID)
	if err != nil {
		return nil, err
	}
	var lines []CommitLine
	for lineRows.Next() {
		var typeID string
		var quantity, unitPrice int
		if err := lineRows.Scan(&typeID, &quantity, &unitPrice); err != nil {
			lineRows.Close()
			return nil, err
		}
		price := unitPrice
		lines = append(lines, CommitLine{TicketTypeID: typeID, Quantity: quantity, UnitPriceCents: &price})
	}
	if err := lineRows.Err(); err != nil {
		lineRows.Close()
		return nil, err
	}
	lineRows.Close()

	recorded, err := r.CommitSales(ctx, tx, CommitSalesInput{
		EventID:        eventID,
		OrganizationID: orgID,
		Channel:        "online",
		// An Online Sale carries no Sales Source; that qualifier belongs to the
		// import channel alone.
		Source: "",
		Sales: []CommitSale{{
			CustomerEmail:     email,
			CustomerFirstName: firstName,
			CustomerLastName:  lastName,
			PaymentMethod:     in.PaymentMethod,
			SoldAt:            in.Now,
			ConfirmationRef:   in.ConfirmationRef,
			Lines:             lines,
		}},
		Now:            in.Now,
		UpsertCustomer: in.UpsertCustomer,
	})
	if err != nil {
		return nil, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET status = 'approved',
		    ticket_sale_id = $2,
		    provider_transaction_id = COALESCE(NULLIF($3, ''), provider_transaction_id),
		    updated_at = $4
		WHERE id = $1
	`, paymentID, recorded[0].ID, in.ProviderTransactionID, in.Now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ApprovedPayment{Sale: &recorded[0]}, nil
}

// MarkPaymentFailed settles a pending Payment as failed (declined or
// cancelled). It reports whether this call did the settling: false means
// another confirm got there first and the caller should re-read the recorded
// outcome.
func (r *Repository) MarkPaymentFailed(ctx context.Context, clientTransactionID, providerTransactionID string, now time.Time) (bool, error) {
	return r.settlePayment(ctx, clientTransactionID, providerTransactionID, "failed", now)
}

// MarkPaymentApprovedWithoutSale records the incident case: the provider
// approved the charge but the sale commit failed, so the Payment ends
// 'approved' with no ticket_sale_id — the marker the platform operator
// reconciles by hand. It reports whether this call did the settling.
func (r *Repository) MarkPaymentApprovedWithoutSale(ctx context.Context, clientTransactionID, providerTransactionID string, now time.Time) (bool, error) {
	return r.settlePayment(ctx, clientTransactionID, providerTransactionID, "approved", now)
}

func (r *Repository) settlePayment(ctx context.Context, clientTransactionID, providerTransactionID, status string, now time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE payments
		SET status = $2,
		    provider_transaction_id = COALESCE(NULLIF($3, ''), provider_transaction_id),
		    updated_at = $4
		WHERE client_transaction_id = $1 AND status = 'pending'
	`, clientTransactionID, status, providerTransactionID, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
