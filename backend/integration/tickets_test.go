package integration

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	salesrepo "github.com/peter/ticket_pos/backend/internal/sales/repository"
	"github.com/peter/ticket_pos/backend/migrations"
)

// A Ticket is one unit of admission within a Ticket Sale: where a Ticket Sale
// Line says three, there are three Tickets (#308, ADR 0043). These tests hold
// the one invariant the model does not enforce with a constraint — that an
// Event's Tickets are exactly as many as its Ticket Sale Lines' quantities
// summed — across every Sales Channel that can record a sale.
//
// SQL rather than API, throughout: nothing exposes a Ticket yet, deliberately.
// This ticket mints the rows and stops there.

// ticketsForSale counts the Tickets minted for one Ticket Sale, across all of
// its Ticket Sale Lines.
func ticketsForSale(t *testing.T, env *testEnv, saleID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
	`, saleID).Scan(&n); err != nil {
		t.Fatalf("count Tickets for sale: %v", err)
	}
	return n
}

// ticketsForEvent counts every Ticket on an Event, reversed Ticket Sales
// included — a Sale Reversal voids a sale, it does not unmint its Tickets.
func ticketsForEvent(t *testing.T, env *testEnv, eventID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*)
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1
	`, eventID).Scan(&n); err != nil {
		t.Fatalf("count Tickets for event: %v", err)
	}
	return n
}

// summedQuantities is the figure a Ticket is a projection of: the quantities of
// an Event's Ticket Sale Lines summed. Deliberately the SUM(quantity) shape
// Tickets Sold uses everywhere, so a change to one and not the other shows up
// here as a difference.
func summedQuantities(t *testing.T, env *testEnv, eventID string, activeOnly bool) int {
	t.Helper()
	where := ""
	if activeOnly {
		where = " AND s.status = 'active'"
	}
	var n int
	if err := env.db.QueryRow(`
		SELECT COALESCE(SUM(l.quantity), 0)
		FROM ticket_sale_lines l
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1`+where, eventID).Scan(&n); err != nil {
		t.Fatalf("sum Ticket Sale Line quantities: %v", err)
	}
	return n
}

// TestOnlineSaleMintsOneTicketPerTicketSold drives a real Storefront checkout
// and expects its Tickets to appear with the sale, in the same transaction that
// recorded it.
func TestOnlineSaleMintsOneTicketPerTicketSold(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ttID := publishCheckoutEvent(t, env, sessionID, "Mint Fest", "mint-fest", 1000, 50)

	begin := beginCheckoutOK(t, env, "test-org", "mint-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ttID, "quantity": 3}))

	// Before the Payment is approved there is no Ticket Sale, so there is
	// nothing to have minted from: a live Capacity Hold is quantity-shaped and
	// has no Tickets at all (ADR 0043).
	if got := ticketsForEvent(t, env, eventID); got != 0 {
		t.Fatalf("Tickets while the Payment is pending = %d, want 0", got)
	}

	if got := confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved"); got.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", got.Status)
	}
	_, saleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
	if saleID == nil {
		t.Fatal("approved Payment recorded no Ticket Sale")
	}

	if got := ticketsForSale(t, env, *saleID); got != 3 {
		t.Fatalf("Tickets for the Online Sale = %d, want 3", got)
	}
	if got, want := ticketsForEvent(t, env, eventID), summedQuantities(t, env, eventID, false); got != want {
		t.Fatalf("Tickets = %d, summed Ticket Sale Line quantities = %d", got, want)
	}
}

// TestUnapprovedPaymentLeavesNoTickets is the reason Tickets are minted in the
// sale-commit transaction and never before: a Payment that is declined,
// cancelled or abandoned never becomes a Ticket Sale, and must leave nothing
// behind to explain.
func TestUnapprovedPaymentLeavesNoTickets(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ttID := publishCheckoutEvent(t, env, sessionID, "Declined Fest", "declined-fest", 1000, 50)

	begin := beginCheckoutOK(t, env, "test-org", "declined-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ttID, "quantity": 4}))
	if got := confirmCheckoutOK(t, env, begin.ClientTransactionID, "declined"); got.Status != "failed" {
		t.Fatalf("confirm status = %q, want failed", got.Status)
	}

	status, saleID, _ := paymentRecord(t, env, begin.ClientTransactionID)
	if status != "failed" || saleID != nil {
		t.Fatalf("Payment status = %q, ticket_sale_id = %v; want failed and none", status, saleID)
	}
	if got := ticketsForEvent(t, env, eventID); got != 0 {
		t.Fatalf("Tickets left by a failed Payment = %d, want 0", got)
	}
}

// TestSaleImportMintsOneTicketPerTicketSold covers the `import` channel, which
// reaches the same spine through CommitImport. A five-ticket batch is five
// Tickets, spread over the two Ticket Sales that sold them.
func TestSaleImportMintsOneTicketPerTicketSold(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Import Fest", "import-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	commitBatch(t, env, sessionID, eventID, "batch-tickets", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 3, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})

	if got := ticketsForEvent(t, env, eventID); got != 5 {
		t.Fatalf("Tickets for the Sale Import = %d, want 5", got)
	}
	if got, want := ticketsForEvent(t, env, eventID), summedQuantities(t, env, eventID, false); got != want {
		t.Fatalf("Tickets = %d, summed Ticket Sale Line quantities = %d", got, want)
	}
}

// TestInPersonSaleMintsOneTicketPerTicketSold reaches the commit spine
// directly, because there is no In-Person Sale endpoint yet: the POS is
// unbuilt, and `in_person` exists today as a Sales Channel value on the schema
// CHECK and the sales list filter (see sale_paths_external_event_test.go).
//
// The test is still worth writing now rather than deferring it to the POS.
// Minting lives in the shared spine precisely so that no channel can record a
// Ticket Sale without its Tickets, and this asserts the spine does it for a
// channel whose entry point does not exist — which is the whole claim.
func TestInPersonSaleMintsOneTicketPerTicketSold(t *testing.T) {
	env := setupTest(t)
	ctx := context.Background()
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Door Fest", "door-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	var orgID string
	if err := env.db.QueryRow(`SELECT organization_id FROM events WHERE id = $1`, eventID).Scan(&orgID); err != nil {
		t.Fatalf("read organization: %v", err)
	}

	tx, err := sharedApp.DB.Pool.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	recorded, err := sharedApp.SalesRepo.CommitSales(ctx, tx, salesrepo.CommitSalesInput{
		EventID:        eventID,
		OrganizationID: orgID,
		Channel:        "in_person",
		Now:            env.fixedClock,
		UpsertCustomer: sharedApp.CustomersService.UpsertForSale,
		Sales: []salesrepo.CommitSale{{
			Customer: platform.SaleCustomer{
				Email:     "door@example.com",
				FirstName: "Dora",
				LastName:  "Vega",
				// A native Sales Channel never records a sale without its Tax ID
				// (ADR 0016), and the spine asserts it.
				TaxID: platform.SaleTaxID{Type: "cedula", Number: validCedula},
			},
			// No Payment Method: the schema reserves that column for a Direct
			// Sale, which is an `import` sale with the `direct` Sales Source.
			ConfirmationRef: "TP-DOOR1",
			SoldAt:          env.fixedClock,
			Lines:           []salesrepo.CommitLine{{TicketTypeID: ttID, Quantity: 2}},
		}},
	})
	if err != nil {
		t.Fatalf("commit In-Person Sale: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit transaction: %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("recorded %d sales, want 1", len(recorded))
	}

	if got := ticketsForSale(t, env, recorded[0].ID); got != 2 {
		t.Fatalf("Tickets for the In-Person Sale = %d, want 2", got)
	}
}

// TestTicketRowsEqualSummedQuantitiesForAnEvent is the invariant the issue asks
// for, held over an Event that has sold on more than one Sales Channel and has
// had one of those sales reversed.
//
// It asserts both halves of ADR 0043's bargain: an Event's Tickets equal its
// Ticket Sale Lines' quantities summed over EVERY sale, and Tickets Sold —
// which counts only the active ones, and is not read off this table — is
// untouched by any of it.
func TestTicketRowsEqualSummedQuantitiesForAnEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, ttID := publishCheckoutEvent(t, env, sessionID, "Mixed Fest", "mixed-fest", 1000, 100)

	// online: 3
	begin := beginCheckoutOK(t, env, "test-org", "mixed-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": ttID, "quantity": 3}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// import: 2 kept, plus 4 in a batch undone below
	commitBatch(t, env, sessionID, eventID, "mixed-keep", []map[string]any{
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng", "ticket_type_id": ttID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	undone := commitBatch(t, env, sessionID, eventID, "mixed-undo", []map[string]any{
		{"customer_email": "cyd@example.com", "customer_first_name": "Cyd", "customer_last_name": "Roa", "ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-02T10:00:00Z"},
	})

	if got := ticketsForEvent(t, env, eventID); got != 9 {
		t.Fatalf("Tickets before the Sale Reversal = %d, want 9", got)
	}

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+undone+"/undo", map[string]any{
		"notify_buyers": false,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A Ticket carries no status. The reversed Sale keeps its four Tickets and
	// they are read as void through the Ticket Sale, so the total is unmoved.
	if got := ticketsForEvent(t, env, eventID); got != 9 {
		t.Fatalf("Tickets after the Sale Reversal = %d, want 9 — a Sale Reversal voids a sale, it does not unmint Tickets", got)
	}
	if got, want := ticketsForEvent(t, env, eventID), summedQuantities(t, env, eventID, false); got != want {
		t.Fatalf("Tickets = %d, summed Ticket Sale Line quantities = %d", got, want)
	}

	// Tickets Sold is the ACTIVE sum, still counted from the quantities: the
	// reversed batch has dropped out of it while its Tickets remain.
	if got := summedQuantities(t, env, eventID, true); got != 5 {
		t.Fatalf("Tickets Sold = %d, want 5", got)
	}
	if got := soldCount(t, env, sessionID, eventID, ttID); got != 5 {
		t.Fatalf("sold_count = %d, want 5", got)
	}
}

// TestBackfillMintsTicketsForEveryExistingTicketSale runs the backfill
// migration verbatim over Ticket Sale Lines written the way every sale recorded
// before this feature was: with no Tickets beside them.
//
// It executes the migration FILE rather than a copy of its SQL. A backfill that
// is tested by re-typing it is tested against the wrong statement, and this one
// is safe to re-run — it is ON CONFLICT DO NOTHING against the unique
// (ticket_sale_line_id, ordinal), which is also what makes it safe to apply to
// a database where the minting code has already been live.
func TestBackfillMintsTicketsForEveryExistingTicketSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Legacy Fest", "legacy-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// Two sales as the schema held them before Tickets existed: one active, one
	// reversed. The reversed one is backfilled too — a reversed Ticket Sale is
	// never deleted, and its Tickets are read as void through it.
	seedLegacySale := func(email, status string, quantity int) string {
		t.Helper()
		var customerID string
		if err := env.db.QueryRow(`
			INSERT INTO customers (email, first_name, last_name, created_at)
			VALUES ($1, 'Legacy', 'Buyer', NOW())
			RETURNING id
		`, email).Scan(&customerID); err != nil {
			t.Fatalf("seed legacy customer: %v", err)
		}
		var saleID string
		if err := env.db.QueryRow(`
			INSERT INTO ticket_sales (
				event_id, organization_id, channel, source, payment_method,
				customer_id, customer_email, customer_first_name, customer_last_name,
				sold_at, confirmation_ref, status, created_at
			)
			SELECT e.id, e.organization_id, 'import', 'direct', 'cash',
				$5, $2, 'Legacy', 'Buyer',
				NOW(), $3, $4, NOW()
			FROM events e WHERE e.id = $1
			RETURNING id
		`, eventID, email, "TP-LEG"+email[:1], status, customerID).Scan(&saleID); err != nil {
			t.Fatalf("seed legacy sale: %v", err)
		}
		if _, err := env.db.Exec(`
			INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents, created_at)
			VALUES ($1, $2, $3, 1000, NOW())
		`, saleID, ttID, quantity); err != nil {
			t.Fatalf("seed legacy line: %v", err)
		}
		return saleID
	}
	activeSale := seedLegacySale("ana@example.com", "active", 3)
	reversedSale := seedLegacySale("bob@example.com", "reversed", 2)

	if got := ticketsForEvent(t, env, eventID); got != 0 {
		t.Fatalf("Tickets before the backfill = %d, want 0 — the fixture bypasses the commit spine on purpose", got)
	}

	backfill, err := migrations.Files.ReadFile("071_backfill_tickets.sql")
	if err != nil {
		t.Fatalf("read backfill migration: %v", err)
	}
	if _, err := env.db.Exec(string(backfill)); err != nil {
		t.Fatalf("run backfill migration: %v", err)
	}

	if got := ticketsForSale(t, env, activeSale); got != 3 {
		t.Fatalf("Tickets backfilled onto the active sale = %d, want 3", got)
	}
	if got := ticketsForSale(t, env, reversedSale); got != 2 {
		t.Fatalf("Tickets backfilled onto the reversed sale = %d, want 2", got)
	}
	if got, want := ticketsForEvent(t, env, eventID), summedQuantities(t, env, eventID, false); got != want {
		t.Fatalf("Tickets = %d, summed Ticket Sale Line quantities = %d", got, want)
	}

	// Re-running mints nothing further. The migration runner applies a file once,
	// but a backfill that doubles the table when it is applied twice is a trap
	// laid for whoever ever has to replay one.
	if _, err := env.db.Exec(string(backfill)); err != nil {
		t.Fatalf("re-run backfill migration: %v", err)
	}
	if got := ticketsForEvent(t, env, eventID); got != 5 {
		t.Fatalf("Tickets after re-running the backfill = %d, want 5", got)
	}
}
