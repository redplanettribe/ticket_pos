package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The consent vocabulary is imported directly rather than mirrored into
// `platform`, which is where SaleCustomer and SaleTaxID live for crossing the
// sales/customers boundary. The reason those are in platform is that sales and
// customers depend on each other and neither may import the other's packages for
// a data type. Consent is not in that position: it depends on nothing but the
// database and platform (see the consent package's own doc comment), so it sits
// BELOW both, exactly as platform does, and a second spelling of Answers and
// Evidence would be two vocabularies for one set of legal facts.

// Payments: the persistence of a Customer's attempt to pay through a Payment
// Provider (ADR 0012). A Payment begins 'pending' at begin-checkout and settles
// 'approved' — in the same transaction that commits its Ticket Sale through the
// shared CommitSales spine — or 'failed'. 'expired' is the lazy fate of
// abandoned pendings (ADR 0013), written opportunistically by
// ExpireStalePayments; no correctness depends on that transition, because every
// hold-counting query bounds holds by created_at, not by the status flip.

// CheckoutEvent is what beginning a checkout needs to know about the Event: its
// identity, whether it is sellable at all, and the Organization currency the
// amount is denominated in.
type CheckoutEvent struct {
	ID             string
	OrganizationID string
	Name           string
	Status         string
	Currency       string
	// FeeHandling is the Event's Fee Handling mode as stored, which decides
	// whether the buyer prices this checkout quotes carry the Platform Fee and
	// its Fee IVA (ADR 0014).
	FeeHandling string
}

// GetCheckoutEvent resolves an Event by Organization and Event slug for the
// public checkout, returning nil when neither slug matches. Status is returned
// rather than filtered so the caller owns the "published only" rule.
func (r *Repository) GetCheckoutEvent(ctx context.Context, orgSlug, eventSlug string) (*CheckoutEvent, error) {
	var e CheckoutEvent
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT e.id, e.organization_id, e.name, e.status, o.currency, e.fee_handling
		FROM events e
		JOIN organizations o ON o.id = e.organization_id
		WHERE o.slug = $1 AND e.slug = $2
	`, orgSlug, eventSlug).Scan(&e.ID, &e.OrganizationID, &e.Name, &e.Status, &e.Currency, &e.FeeHandling)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// PaymentLine is one Ticket Type, quantity, and price snapshot on a Payment —
// what the approved sale's Ticket Sale Lines are written from. Fee is the
// per-unit economics frozen at begin-checkout (ADR 0014): its buyer price is
// what the Customer pays per unit, and the withholding beside it is what the
// Organization gives up for that unit.
type PaymentLine struct {
	TicketTypeID string
	Quantity     int
	Fee          sales.FeeSnapshot
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
	// Customer is the buyer as they filled in the checkout form, snapshotted here
	// in full because confirm — running on the provider's return redirect —
	// carries nothing of that form back: what is not on this row at begin is lost
	// by the time the sale is committed.
	//
	// That is why the self-asserted flag is stored too, in
	// `customer_session_authorized` (migration 027): the Customer Session was
	// presented to the begin request and cannot be re-established at confirm, and
	// it is what the Customer upsert reads then to decide whether this buyer may
	// replace what a Verified Customer already holds (ADR 0016, #111).
	//
	// The phone is stored NULL when they gave none — the checkout field is
	// optional (#106) — rather than as a blank string: "no phone" is one state,
	// not two, and the column's only consumers ask whether there is a number at
	// all.
	Customer platform.SaleCustomer
	Lines    []PaymentLine
	// AffiliateLinkID is the Affiliate Link this checkout was resolved to at
	// begin, or empty for the ordinary unattributed checkout. Snapshotted here
	// for the same reason the buyer is: confirm arrives on the provider's return
	// redirect carrying nothing but a transaction id, so an attribution not on
	// this row is an attribution lost.
	AffiliateLinkID string
	// Locale is the language of the Storefront page this checkout was completed
	// on, already narrowed to one this platform writes in, and empty for a
	// checkout that named none (ADR 0033). Snapshotted here for the third time
	// the same reason applies: the confirm leg arrives on the provider's return
	// redirect with a transaction id and nothing else, so a language not on this
	// row is a language lost by the time there is a Ticket Sale to put it on.
	Locale string
	// Consent is what the buyer did with the consent boxes on the checkout dialog
	// and the circumstances the platform observed while they did it, held on this
	// row until there is a sale to evidence (#253, ADR 0035).
	//
	// The fourth buyer fact snapshotted here for the third time the same reason
	// applies, and the one with the sharpest deadline: the answers were given in
	// THIS request, and the confirm leg is a redirect back from a third party
	// that knows nothing of them. Held rather than recorded, because a Consent
	// Record evidences a transaction and an abandoned checkout is not one — see
	// migration 064.
	//
	// Each answer is a *bool: nil is "the box was not shown", which is not a No.
	Consent consent.Answers
	// ConsentTermsVersionID is which Terms edition a held Terms answer is about
	// (#537, migration 108): resolved by the consent module at begin-checkout
	// and held beside the answer, because the commit leg must evidence the
	// edition the buyer was shown and not whichever is current when the
	// provider answers. Empty exactly when Consent.TermsAcceptance is nil.
	ConsentTermsVersionID string
	// ConsentEvidence is the technical proof of that same act, derived from the
	// request and never from its body. Empty fields are stored NULL, because
	// "not collected" and "collected as blank" are different answers.
	ConsentEvidence consent.Evidence
	Now             time.Time
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
			customer_tax_id_type, customer_tax_id_number, customer_session_authorized,
			customer_phone, affiliate_link_id, locale,
			consent_policy_acceptance, consent_marketing, consent_networking,
			consent_terms_acceptance, consent_terms_version_id,
			consent_ip, consent_user_agent, consent_origin_url,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6, $7, $8, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $22, $23, $19, $20, $21, $9, $9)
		RETURNING id
	`, in.EventID, in.OrganizationID, in.Provider, in.ClientTransactionID,
		in.AmountCents, in.Customer.Email, in.Customer.FirstName, in.Customer.LastName, in.Now,
		nullString(in.Customer.TaxID.Type), nullString(in.Customer.TaxID.Number), in.Customer.SelfAsserted,
		nullString(in.Customer.Phone), nullString(in.AffiliateLinkID), nullString(in.Locale),
		nullBool(in.Consent.PolicyAcceptance), nullBool(in.Consent.MarketingConsent), nullBool(in.Consent.NetworkingConsent),
		nullString(in.ConsentEvidence.IP), nullString(in.ConsentEvidence.UserAgent), nullString(in.ConsentEvidence.OriginURL),
		nullBool(in.Consent.TermsAcceptance), nullString(in.ConsentTermsVersionID)).Scan(&paymentID)
	if err != nil {
		return "", err
	}

	for _, line := range in.Lines {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO payment_lines (
				payment_id, ticket_type_id, quantity, unit_price_cents,
				base_price_cents, fee_cents, fee_iva_cents, fee_basis_points, fee_iva_basis_points,
				created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, paymentID, line.TicketTypeID, line.Quantity, line.Fee.BuyerUnitPriceCents,
			line.Fee.BasePriceCents, line.Fee.FeeCents, line.Fee.FeeIVACents,
			line.Fee.FeeBasisPoints, line.Fee.FeeIVABasisPoints, in.Now); err != nil {
			return "", err
		}
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return paymentID, nil
}

// LiveCapacityHolds returns the quantities live Capacity Holds currently claim
// per Ticket Type on an Event: pending Payments created strictly after the
// cutoff (ADR 0013). Ticket Types with no live hold are absent from the map.
func (r *Repository) LiveCapacityHolds(ctx context.Context, eventID string, cutoff time.Time) (map[string]int, error) {
	rows, err := r.db.Pool.QueryContext(ctx, sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: "$2", EventExpr: "$1"}), eventID, cutoff)
	if err != nil {
		return nil, err
	}
	return sales.ScanHeldQuantities(rows)
}

// BuyerHoldings identifies the buyer whose holdings are being counted, as the
// customers module resolved them (ADR 0010): the Customer id when a record for
// the email exists, and the normalised email either way.
//
// The two are needed together because the Purchase Limit's two arms are keyed
// differently and cannot be otherwise. A committed Ticket Sale references the
// Customer row, so its arm is keyed on the id. A pending Payment has no
// Customer — the record is upserted only when the sale commits — and carries
// nothing but the email typed at checkout, so its arm is keyed on that. An empty
// CustomerID is the ordinary first-time buyer: they have no Ticket Sales, and
// only their own live Capacity Holds count.
type BuyerHoldings struct {
	CustomerID      string
	NormalizedEmail string
	// ExcludeSaleID names one Ticket Sale whose lines are NOT counted, even
	// while active: the sale a Sale Correction is about to reverse (#351). The
	// replacement is judged against what the buyer will hold once the old sale
	// is gone, which is what makes re-recording the same quantity fit under a
	// limit the old sale had already reached. Empty counts everything.
	ExcludeSaleID string
}

// CustomerEventHoldings returns how much of each of an Event's Ticket Types one
// buyer already holds, which is what a Purchase Limit is measured against
// (ADR 0025): their ACTIVE Ticket Sale Lines plus their live Capacity Holds.
// Ticket Types they hold none of are absent from the map.
//
// The two arms are exactly the two things that consume capacity, which is the
// whole point — the allowance moves like capacity, so a Sale Reversal (status
// leaves 'active') returns it and a failed or expired Payment (status leaves
// 'pending', or its created_at falls behind the cutoff) releases it, with no
// bookkeeping of its own to keep in step.
//
// It is scoped to the Event and returns every Ticket Type at once rather than
// taking a list, so a cart of any size costs one read — the same shape
// LiveCapacityHolds has, for the same reason. A Purchase Limit lives on a Ticket
// Type, and a Ticket Type belongs to one Event, so the Event scope loses
// nothing.
func (r *Repository) CustomerEventHoldings(ctx context.Context, eventID string, buyer BuyerHoldings, cutoff time.Time) (map[string]int, error) {
	// SQL NULL for a buyer with no Customer record yet: the sale arm then matches
	// nothing, which is the truth about someone who has never completed a sale.
	var customerID sql.NullString
	if buyer.CustomerID != "" {
		customerID = sql.NullString{String: buyer.CustomerID, Valid: true}
	}
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ticket_type_id, SUM(held)::int AS held
		FROM (
			SELECT tsl.ticket_type_id AS ticket_type_id, tsl.quantity AS held
			FROM ticket_sale_lines tsl
			JOIN ticket_sales ts ON ts.id = tsl.ticket_sale_id
			WHERE ts.event_id = $1
			  AND ts.status = 'active'
			  AND ts.customer_id = $3::uuid
			  AND ($5::uuid IS NULL OR ts.id <> $5::uuid)
			UNION ALL
			`+sales.LiveHoldsSQL(sales.HoldsFilter{CutoffExpr: "$2", EventExpr: "$1", BuyerEmailExpr: "$4"})+`
		) holdings
		GROUP BY ticket_type_id
	`, eventID, cutoff, customerID, buyer.NormalizedEmail, nullString(buyer.ExcludeSaleID))
	if err != nil {
		return nil, err
	}
	return sales.ScanHeldQuantities(rows)
}

// ExpireStalePayments lazily marks an Event's pending Payments created at or
// before the cutoff as 'expired' (ADR 0013). Purely opportunistic bookkeeping:
// hold-counting never reads the status flip — the created_at cutoff in
// LiveHoldsSQL is the source of truth — so a missed or failed expiry costs
// nothing. It reports how many Payments it flipped.
func (r *Repository) ExpireStalePayments(ctx context.Context, eventID string, cutoff, now time.Time) (int64, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE payments
		SET status = 'expired', updated_at = $3
		WHERE event_id = $1 AND status = 'pending' AND created_at <= $2
	`, eventID, cutoff, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
	// Instrument is the provider's human-readable card description (e.g.
	// "visa ····1234"), kept on the Payment for support lookups; may be empty.
	Instrument string
	// PaymentMethod is recorded on the Ticket Sale (the Payment Provider that
	// collected the money, e.g. 'payphone').
	PaymentMethod   string
	ConfirmationRef string
	// Terms are the Sale Commit Terms this checkout's Ticket Sale is recorded
	// on, exactly as every other channel states them. The Payment's own approval
	// is stamped with Terms.Now too: approving the Payment and recording its sale
	// are one act, and a second clock read here would let them disagree.
	Terms CommitTerms
	// OweSaleInvoice writes the Sale Invoice a paid House sale owes, inside
	// the same transaction (#473). Nil owes nothing.
	OweSaleInvoice OweSaleInvoice
	// CaptureConsent writes the Consent Record for the answers this Payment has
	// been holding since begin-checkout, inside the same transaction (#253).
	//
	// Optional in shape and required in practice: nil is what the reversal and
	// import paths — which settle no checkout dialog — would pass, and what a
	// Payment begun before migration 064 amounts to anyway, since a Payment
	// holding three NULL answers has no capture act to evidence and is skipped.
	//
	// IT IS NOT ONE OF THE COMMIT TERMS, and that is why this struct carries a
	// fourth field beside them rather than a fifth inside CommitTerms: capturing
	// consent is the online checkout's alone. No other channel opens a dialog, so
	// no other channel has an act of consent to evidence.
	CaptureConsent CaptureConsent
}

// CaptureConsent records the Consent Record and applies the consent state for a
// checkout, inside the transaction that is committing its Ticket Sale. The sales
// service supplies it, bound to the consent service, so the cross-module call
// goes through that module's service exactly as UpsertCustomer does.
//
// Its error is fatal to the commit, deliberately. The alternative — recording the
// sale and logging that the evidence could not be written — is precisely the
// unevidenced processing this feature exists to abolish, and at this point in
// the transaction nothing has been charged that a rollback would strand: the
// caller marks the approved-without-sale incident it already knows how to mark.
type CaptureConsent func(ctx context.Context, tx *sql.Tx, capture consent.Capture) error

// OweSaleInvoice is the sale-commit spine's invoicing seam (#473, ADR 0060):
// called for a PAID online sale of a HOUSE ORGANIZATION, inside the
// transaction that is committing its Ticket Sale, with the sale described
// as it was just written. The sales service supplies it, bound to the
// invoicing service, exactly as CaptureConsent is bound to consent.
//
// Its error is fatal to the commit, deliberately and by the ADR's own word:
// "owing in the sale's transaction is what makes the invariant hold: nothing
// can be sold and forgotten." A sale recorded without the document it owes
// would be exactly that, and at this point nothing has been charged that a
// rollback would strand — the caller marks the approved-without-sale
// incident it already knows how to mark.
//
// NIL IS THE NO-INVOICING DEPLOYMENT, and every other channel's commit. A
// nil seam owes nothing and fails nothing, which is what a build without an
// invoicing module wired must do; whether the Organization is a House
// Organization is decided HERE, in the transaction, so that a designation
// made a second after the sale committed can never reach it.
type OweSaleInvoice func(ctx context.Context, tx *sql.Tx, sale sales.PaidOnlineSale) error

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
// The spine's capacity check counts sold + OTHER live Capacity Holds: this
// Payment's id is excluded, so a Payment whose own hold is what fills the last
// capacity still commits — the hold converts into sold_count, it never
// double-counts against itself (ADR 0013).
//
// A lazily-'expired' Payment is accepted here exactly like a pending one: the
// hold window bounds the hold, not the Payment's validity, and by this point
// the provider has approved the charge. Honest behavior is to record the sale
// the Customer paid for if capacity still allows, flipping expired → approved;
// if capacity is gone the commit fails like any lost race and the caller marks
// the approved-without-sale incident.
//
// Any error (a capacity race lost since begin-checkout, most plausibly) rolls
// the whole transaction back, leaving the Payment as it was: the caller owns
// the approved-without-sale incident marking.
func (r *Repository) ApprovePaymentAndCommitSale(ctx context.Context, in ApprovePaymentInput) (*ApprovedPayment, error) {
	tx, err := r.db.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var paymentID, eventID, orgID, status, email, firstName, lastName string
	// The Tax ID columns are nullable forever (ADR 0016): a Payment begun before
	// the checkout collected one confirms into a sale with no Tax ID rather than
	// failing years later at the till.
	var taxIDType, taxIDNumber sql.NullString
	// The phone is nullable for a different reason: the checkout field is
	// OPTIONAL and never fabricated (#103), so a buyer who skipped it leaves NULL
	// here forever. This read is the whole point of snapshotting it at begin —
	// the provider's return redirect carries a transaction id and nothing else,
	// so a phone not stored on the Payment is a phone lost (#106, #107).
	var phone sql.NullString
	var sessionAuthorized bool
	// The Affiliate Link this checkout was begun under, NULL for the ordinary
	// unattributed one. Read here and written onto the sale below, inside the one
	// transaction that records it: every Online Sale — paid or free — passes
	// through this spine, so attribution needs no second path (ADR 0017, #146).
	var affiliateLinkID sql.NullString
	// The Sale Locale this checkout was begun under, NULL when the page named
	// none or named a language this platform does not write (ADR 0033). Read here
	// and copied onto the sale below, inside the one transaction that records it,
	// exactly as the attribution above is.
	var locale sql.NullString
	// The consent answers this Payment has been holding since begin-checkout, and
	// the circumstances they were given in (migration 064). Read here and turned
	// into the immutable Consent Record below, inside the one transaction that
	// records the sale — which is what makes "an abandoned Payment leaves no
	// evidence" true by construction rather than by remembering to clean up.
	//
	// All three answers NULL means no capture act: a Payment begun before the
	// dialog had a consent section, and nothing to evidence.
	var consentPolicy, consentMarketing, consentNetworking, consentTerms sql.NullBool
	var consentIP, consentUserAgent, consentOriginURL, consentTermsVersionID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_id, organization_id, status, customer_email, customer_first_name, customer_last_name,
		       customer_tax_id_type, customer_tax_id_number, customer_phone, customer_session_authorized,
		       affiliate_link_id, locale,
		       consent_policy_acceptance, consent_marketing, consent_networking,
		       consent_terms_acceptance, consent_terms_version_id,
		       consent_ip, consent_user_agent, consent_origin_url
		FROM payments
		WHERE client_transaction_id = $1
		FOR UPDATE
	`, in.ClientTransactionID).Scan(&paymentID, &eventID, &orgID, &status, &email, &firstName, &lastName,
		&taxIDType, &taxIDNumber, &phone, &sessionAuthorized, &affiliateLinkID, &locale,
		&consentPolicy, &consentMarketing, &consentNetworking,
		&consentTerms, &consentTermsVersionID,
		&consentIP, &consentUserAgent, &consentOriginURL)
	if err != nil {
		return nil, err
	}
	if status != "pending" && status != "expired" {
		return &ApprovedPayment{AlreadySettled: true}, nil
	}

	// The Answers this Payment has been holding since begin-checkout (migration
	// 074), read here and copied onto the Tickets the commit below mints — inside
	// the one transaction that records the sale, exactly as the consent answers
	// above are turned into a Consent Record. That is what makes "a Payment that
	// fails or expires produces no Tickets and no Answers" true by construction
	// rather than by remembering to clean up.
	//
	// READ UNCONDITIONALLY, AND NOT BEHIND THE FEATURE FLAG. The flag governs
	// whether anything can be CAPTURED; a Payment begun while it was open and
	// confirmed after it closed still holds answers, and dropping them here would
	// destroy what somebody typed for the sake of a flag that was never about
	// this leg. On the shipped, dark deployment the table is empty and this is one
	// index scan returning nothing.
	heldAnswers, err := listHeldAnswersByPaymentLine(ctx, tx, paymentID)
	if err != nil {
		return nil, err
	}

	lineRows, err := tx.QueryContext(ctx, `
		SELECT id, ticket_type_id, quantity, unit_price_cents,
		       base_price_cents, fee_cents, fee_iva_cents, fee_basis_points, fee_iva_basis_points
		FROM payment_lines
		WHERE payment_id = $1
	`, paymentID)
	if err != nil {
		return nil, err
	}
	var lines []CommitLine
	for lineRows.Next() {
		var lineID, typeID string
		var quantity int
		var fee sales.FeeSnapshot
		if err := lineRows.Scan(&lineID, &typeID, &quantity, &fee.BuyerUnitPriceCents,
			&fee.BasePriceCents, &fee.FeeCents, &fee.FeeIVACents,
			&fee.FeeBasisPoints, &fee.FeeIVABasisPoints); err != nil {
			lineRows.Close()
			return nil, err
		}
		// The Payment's snapshot is copied onto the sale verbatim: what the
		// Customer is paying was decided at begin-checkout, and no catalog price
		// edit, rate change, or Fee Handling flip since then may touch it.
		price := fee.BuyerUnitPriceCents
		snapshot := fee
		lines = append(lines, CommitLine{
			TicketTypeID:   typeID,
			Quantity:       quantity,
			UnitPriceCents: &price,
			Fee:            &snapshot,
			// The Answers ride the LINE from here on, because the line is what
			// mints the Tickets they are about. Keyed by the Payment Line id,
			// which is the half of (payment line, index) this loop is holding.
			Answers: heldAnswers[lineID],
		})
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
		// This Payment's own hold must convert into sold_count, not count
		// against itself (ADR 0013).
		ExcludePaymentID: paymentID,
		Terms:            in.Terms,
		Sales: []CommitSale{{
			// The buyer is rebuilt from the Payment verbatim: the sale records
			// what they supplied at begin-checkout, whatever their profile says
			// by the time the provider answers.
			//
			// The phone travels no further than the Customer upsert — unlike the
			// Tax ID beside it, nothing writes it onto the Ticket Sale (#107) —
			// and whether it may replace what the Customer already holds is
			// decided there, by customer_session_authorized, restored here onto
			// the buyer it describes rather than onto either value it guards
			// (#111).
			Customer: platform.SaleCustomer{
				Email:     email,
				FirstName: firstName,
				LastName:  lastName,
				TaxID: platform.SaleTaxID{
					Type:   taxIDType.String,
					Number: taxIDNumber.String,
				},
				Phone:        phone.String,
				SelfAsserted: sessionAuthorized,
			},
			PaymentMethod:   in.PaymentMethod,
			SoldAt:          in.Terms.Now,
			ConfirmationRef: in.ConfirmationRef,
			AffiliateLinkID: affiliateLinkID.String,
			Locale:          locale.String,
			Lines:           lines,
		}},
	})
	if err != nil {
		return nil, err
	}

	// The evidence, written where the Customer it names has just come into
	// existence and the sale it evidences is already inserted. The order matters
	// only in that all three are one transaction: nothing here may commit without
	// the others, in either direction (ADR 0035, parent spec decision 30).
	//
	// "Was the email proven?" is the same flag that decides whether this buyer may
	// overwrite a Verified Customer's Tax ID — the checkout ran under that
	// Customer's own Customer Session — because it is the same question, asked
	// once at begin-checkout and snapshotted here.
	//
	// IT IS READ RATHER THAN ASSUMED, and that survives ADR 0054 deliberately.
	// Every checkout begun since #386 snapshots it true, so nothing produces a
	// Pending Confirmation any more; but this is the return leg, and the Payments
	// it settles include ones begun before that route existed. Hardcoding a `true`
	// here would rewrite what was true of those checkouts at the moment they
	// happened, which is the one thing an evidence log may not do.
	if in.CaptureConsent != nil && (consentPolicy.Valid || consentMarketing.Valid || consentNetworking.Valid || consentTerms.Valid) {
		if err := in.CaptureConsent(ctx, tx, consent.Capture{
			CustomerID: recorded[0].CustomerID,
			// The address AS ASSERTED on the checkout form, which is not necessarily
			// the Customer's stored one on a Payment begun before ADR 0054, where a
			// guest may have typed a stranger's. On anything begun since, the form
			// had no address to assert and this is the session's own.
			Email:       email,
			Channel:     consent.ChannelCheckout,
			EmailProven: sessionAuthorized,
			Answers: consent.Answers{
				PolicyAcceptance:  nullableBool(consentPolicy),
				MarketingConsent:  nullableBool(consentMarketing),
				NetworkingConsent: nullableBool(consentNetworking),
				TermsAcceptance:   nullableBool(consentTerms),
			},
			// The edition the Terms answer was held WITH (#537, migration 108):
			// the capture evidences the text the buyer was shown at begin, not
			// whichever edition is current on the provider's schedule.
			TermsVersionID: consentTermsVersionID.String,
			Evidence: consent.Evidence{
				IP:        consentIP.String,
				UserAgent: consentUserAgent.String,
				OriginURL: consentOriginURL.String,
				// The language the checkout dialog's notice and labels were
				// rendered in (#567, migration 115). It costs nothing here: the
				// Sale Locale held at begin-checkout (migration 059) is the
				// locale of the Storefront page that drew the boxes, and the
				// Storefront asks the public policy endpoint for exactly that
				// language and is answered strictly — a language the edition
				// does not publish is a 404 and no boxes are drawn at all, so
				// there is no fallback here to be wrong about. It is read from
				// the same row, on the same INSERT, as everything else this
				// checkout held.
				PresentedLocale: platform.Locale(locale.String),
			},
		}); err != nil {
			return nil, err
		}
	}

	// The Sale Invoice a paid House sale owes, written last and inside the
	// same transaction — after the sale it names exists and before anything
	// commits (#473, ADR 0060). Three conditions, all decided here: the seam
	// is wired, money was collected, and the Organization is a House
	// Organization AT THIS MOMENT. The channel is `online` by construction of
	// this function. A free claim owes nothing (nothing to invoice), and an
	// Organization designated after this transaction commits owes nothing
	// for it either: designation is never retroactive, and reading the
	// designation inside the transaction is what makes that a property of
	// the data rather than of timing.
	if in.OweSaleInvoice != nil && recorded[0].AmountCents > 0 {
		house, err := isHouseOrganization(ctx, tx, orgID)
		if err != nil {
			return nil, err
		}
		if house {
			sale, err := paidOnlineSaleOf(ctx, tx, &recorded[0], orgID, eventID, in.PaymentMethod, lines)
			if err != nil {
				return nil, err
			}
			if err := in.OweSaleInvoice(ctx, tx, sale); err != nil {
				return nil, err
			}
			recorded[0].SaleInvoiceOwed = true
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE payments
		SET status = 'approved',
		    ticket_sale_id = $2,
		    provider_transaction_id = COALESCE(NULLIF($3, ''), provider_transaction_id),
		    instrument = COALESCE(NULLIF($4, ''), instrument),
		    updated_at = $5
		WHERE id = $1
	`, paymentID, recorded[0].ID, in.ProviderTransactionID, in.Instrument, in.Terms.Now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ApprovedPayment{Sale: &recorded[0]}, nil
}

// isHouseOrganization reads the House designation inside the sale's own
// transaction: the pair of columns migration 097 keeps, set exactly when
// somebody designated the Organization (ADR 0060).
func isHouseOrganization(ctx context.Context, tx *sql.Tx, orgID string) (bool, error) {
	var house bool
	if err := tx.QueryRowContext(ctx, `
		SELECT house_designated_at IS NOT NULL FROM organizations WHERE id = $1
	`, orgID).Scan(&house); err != nil {
		return false, fmt.Errorf("read house designation: %w", err)
	}
	return house, nil
}

// paidOnlineSaleOf describes the sale just recorded for the invoicing seam:
// the buyer as snapshotted on it, the Event's name, and one line per Ticket
// Sale Line in cart order with the Ticket Type's name and the buyer price
// it was sold at. The names are read now, in the transaction, so that the
// document's lines say what the buyer saw at checkout and never what the
// catalog is renamed to afterwards.
func paidOnlineSaleOf(ctx context.Context, tx *sql.Tx, sale *RecordedSale, orgID, eventID, provider string, lines []CommitLine) (sales.PaidOnlineSale, error) {
	var eventName string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM events WHERE id = $1`, eventID).Scan(&eventName); err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read event name: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT tt.id, tt.name
		FROM ticket_sale_lines l
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
		WHERE l.ticket_sale_id = $1
	`, sale.ID)
	if err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read ticket type names: %w", err)
	}
	defer rows.Close()
	names := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return sales.PaidOnlineSale{}, fmt.Errorf("scan ticket type name: %w", err)
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return sales.PaidOnlineSale{}, fmt.Errorf("read ticket type names: %w", err)
	}
	out := sales.PaidOnlineSale{
		TicketSaleID:    sale.ID,
		OrganizationID:  orgID,
		EventID:         eventID,
		EventName:       eventName,
		ConfirmationRef: sale.ConfirmationRef,
		Buyer: platform.SaleCustomer{
			Email:     sale.CustomerEmail,
			FirstName: sale.CustomerFirstName,
			LastName:  sale.CustomerLastName,
			TaxID:     sale.CustomerTaxID,
		},
		Locale:          sale.Locale,
		PaymentProvider: provider,
		AmountCents:     sale.AmountCents,
	}
	for _, l := range lines {
		name, ok := names[l.TicketTypeID]
		if !ok {
			return sales.PaidOnlineSale{}, fmt.Errorf("ticket type %s of sale %s has no line on file", l.TicketTypeID, sale.ID)
		}
		price := 0
		if l.UnitPriceCents != nil {
			price = *l.UnitPriceCents
		}
		out.Lines = append(out.Lines, sales.PaidOnlineSaleLine{
			TicketTypeName: name,
			Quantity:       l.Quantity,
			UnitPriceCents: price,
		})
	}
	return out, nil
}

// nullableBool restores a held consent answer to the *bool the consent
// vocabulary speaks in, where nil means the box was not shown — a fact SQL NULL
// carries and a plain bool cannot.
func nullableBool(v sql.NullBool) *bool {
	if !v.Valid {
		return nil
	}
	answer := v.Bool
	return &answer
}

// MarkPaymentFailed settles a pending Payment as failed (declined or
// cancelled). It reports whether this call did the settling: false means
// another confirm got there first and the caller should re-read the recorded
// outcome.
func (r *Repository) MarkPaymentFailed(ctx context.Context, clientTransactionID, providerTransactionID, instrument string, now time.Time) (bool, error) {
	return r.settlePayment(ctx, clientTransactionID, providerTransactionID, instrument, "failed", now)
}

// MarkPaymentApprovedWithoutSale records the incident case: the provider
// approved the charge but the sale commit failed, so the Payment ends
// 'approved' with no ticket_sale_id — the marker the platform operator
// reconciles by hand. It reports whether this call did the settling.
func (r *Repository) MarkPaymentApprovedWithoutSale(ctx context.Context, clientTransactionID, providerTransactionID, instrument string, now time.Time) (bool, error) {
	return r.settlePayment(ctx, clientTransactionID, providerTransactionID, instrument, "approved", now)
}

// settlePayment moves an unsettled Payment to a terminal status. 'expired' is
// settleable alongside 'pending': lazy expiry is bookkeeping, not a verdict, so
// a late confirm still records the provider's real outcome over it — including
// the approved-without-sale incident marker after a failed commit (ADR 0013).
func (r *Repository) settlePayment(ctx context.Context, clientTransactionID, providerTransactionID, instrument, status string, now time.Time) (bool, error) {
	res, err := r.db.Pool.ExecContext(ctx, `
		UPDATE payments
		SET status = $2,
		    provider_transaction_id = COALESCE(NULLIF($3, ''), provider_transaction_id),
		    instrument = COALESCE(NULLIF($4, ''), instrument),
		    updated_at = $5
		WHERE client_transaction_id = $1 AND status IN ('pending', 'expired')
	`, clientTransactionID, status, providerTransactionID, instrument, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
