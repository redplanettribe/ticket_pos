package integration

import (
	"database/sql"
	"net/http"
	"testing"
	"time"
)

// Customer identity (ADR 0010): every Ticket Sale on every Sales Channel creates
// or reuses a platform-global Customer, keyed on a normalised email. These tests
// drive the only Sales Channel that records sales today — the Sale Import — over
// HTTP, and read the Customer record back with SQL because no API exposes it yet
// (this ticket ships schema and the sales integration, no endpoints and no UI).

// customerRecord is a Customer row as stored: the profile name the person is
// asserted to have, plus the verification and soft-delete timestamps.
type customerRecord struct {
	ID         string
	Email      string
	FirstName  string
	LastName   string
	VerifiedAt sql.NullTime
	DeletedAt  sql.NullTime
}

// readCustomer reads the single Customer for a normalised email. SQL rather than
// the API: the Customer record is deliberately invisible, with no endpoint and no
// UI, so the database is the only way to observe it.
func readCustomer(t *testing.T, env *testEnv, email string) customerRecord {
	t.Helper()
	var c customerRecord
	if err := env.db.QueryRow(`
		SELECT id, email, first_name, last_name, verified_at, deleted_at
		FROM customers WHERE email = $1
	`, email).Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.VerifiedAt, &c.DeletedAt); err != nil {
		t.Fatalf("read customer %q: %v", email, err)
	}
	return c
}

// countCustomers counts every Customer on the platform. Customers are not scoped
// to an Organization, so this is deliberately unscoped.
func countCustomers(t *testing.T, env *testEnv) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers`).Scan(&n); err != nil {
		t.Fatalf("count customers: %v", err)
	}
	return n
}

// saleRecord is the Customer identity a Ticket Sale carries: the reference to the
// Customer plus the sale's own immutable record of what was transacted.
type saleRecord struct {
	CustomerID string
	Email      string
	FirstName  string
	LastName   string
}

// readSaleByRef reads one Ticket Sale by its Sale Confirmation reference. SQL
// because customer_id is not exposed on any API surface.
func readSaleByRef(t *testing.T, env *testEnv, ref string) saleRecord {
	t.Helper()
	var s saleRecord
	if err := env.db.QueryRow(`
		SELECT customer_id, customer_email, customer_first_name, customer_last_name
		FROM ticket_sales WHERE confirmation_ref = $1
	`, ref).Scan(&s.CustomerID, &s.Email, &s.FirstName, &s.LastName); err != nil {
		t.Fatalf("read sale %q: %v", ref, err)
	}
	return s
}

// importOneSale commits a single-sale Direct Sale Import and returns the Sale
// Confirmation reference of the Ticket Sale it recorded, so the caller can read
// that exact sale back.
func importOneSale(t *testing.T, env *testEnv, sessionID, eventID, idempotencyKey, ticketTypeID, email, first, last string) string {
	t.Helper()
	before := len(env.email.Confirmations())
	commitBatch(t, env, sessionID, eventID, idempotencyKey, []map[string]any{
		{"customer_email": email, "customer_first_name": first, "customer_last_name": last,
			"ticket_type_id": ticketTypeID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
	})
	confs := env.email.Confirmations()
	if len(confs) != before+1 {
		t.Fatalf("confirmations = %d, want %d after one imported sale", len(confs), before+1)
	}
	return confs[len(confs)-1].Reference
}

// TestSaleForUnknownEmailCreatesUnverifiedCustomer proves a Ticket Sale for an
// email nobody has bought under creates a Customer, that the sale references it,
// and that the new record is inert: nothing in this ticket verifies it, and the
// soft-delete timestamp ships unused.
func TestSaleForUnknownEmailCreatesUnverifiedCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Identity Fest", "identity-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	ref := importOneSale(t, env, sessionID, eventID, "cust-new", ttID, "ana@example.com", "Ana", "Lopez")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1", n)
	}
	customer := readCustomer(t, env, "ana@example.com")
	if customer.FirstName != "Ana" || customer.LastName != "Lopez" {
		t.Fatalf("customer name = %q / %q, want Ana / Lopez", customer.FirstName, customer.LastName)
	}
	// A record created by a sale is inert: nobody has proven they own it.
	if customer.VerifiedAt.Valid {
		t.Fatalf("verified_at = %v, want null on a newly created Customer", customer.VerifiedAt.Time)
	}
	// deleted_at ships unused: nothing reads or writes it yet.
	if customer.DeletedAt.Valid {
		t.Fatalf("deleted_at = %v, want null (the column ships unused)", customer.DeletedAt.Time)
	}

	sale := readSaleByRef(t, env, ref)
	if sale.CustomerID != customer.ID {
		t.Fatalf("sale customer_id = %q, want %q", sale.CustomerID, customer.ID)
	}
}

// TestFurtherSaleForSameEmailReusesCustomer proves one Customer accumulates
// several Ticket Sales rather than one record per purchase.
func TestFurtherSaleForSameEmailReusesCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Return Fest", "return-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	firstRef := importOneSale(t, env, sessionID, eventID, "cust-reuse-1", ttID, "ana@example.com", "Ana", "Lopez")
	secondRef := importOneSale(t, env, sessionID, eventID, "cust-reuse-2", ttID, "ana@example.com", "Ana", "Lopez")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 for two sales by the same Customer", n)
	}
	customer := readCustomer(t, env, "ana@example.com")
	if got := readSaleByRef(t, env, firstRef).CustomerID; got != customer.ID {
		t.Fatalf("first sale customer_id = %q, want %q", got, customer.ID)
	}
	if got := readSaleByRef(t, env, secondRef).CustomerID; got != customer.ID {
		t.Fatalf("second sale customer_id = %q, want %q", got, customer.ID)
	}
}

// TestCustomerEmailNormalisationCollapsesToOneCustomer proves emails differing
// only by letter case or surrounding whitespace resolve to one Customer, so two
// box offices entering the same address differently cannot fragment a person's
// history — while each Ticket Sale still keeps the email exactly as recorded.
func TestCustomerEmailNormalisationCollapsesToOneCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Case Fest", "case-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	lowerRef := importOneSale(t, env, sessionID, eventID, "cust-norm-1", ttID, "ana@example.com", "Ana", "Lopez")
	upperRef := importOneSale(t, env, sessionID, eventID, "cust-norm-2", ttID, "ANA@Example.COM", "Ana", "Lopez")
	paddedRef := importOneSale(t, env, sessionID, eventID, "cust-norm-3", ttID, "  Ana@example.com  ", "Ana", "Lopez")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 — case and whitespace must not fragment a Customer", n)
	}
	customer := readCustomer(t, env, "ana@example.com")
	for _, ref := range []string{lowerRef, upperRef, paddedRef} {
		if got := readSaleByRef(t, env, ref).CustomerID; got != customer.ID {
			t.Fatalf("sale %s customer_id = %q, want %q", ref, got, customer.ID)
		}
	}

	// The sale keeps the email as it was recorded; only the Customer is normalised.
	if got := readSaleByRef(t, env, upperRef).Email; got != "ANA@Example.COM" {
		t.Fatalf("sale customer_email = %q, want the recorded %q", got, "ANA@Example.COM")
	}
}

// TestSaleInSecondOrganizationReusesCustomer proves a Customer is
// platform-global: buying from a second Organization reuses the one record
// rather than creating an identity per promoter (ADR 0010).
func TestSaleInSecondOrganizationReusesCustomer(t *testing.T) {
	env := setupTest(t)

	firstSession := orgAdminSession(t, env)
	firstEvent := createDraftEvent(t, env, firstSession, "Org One Fest", "org-one-fest")
	firstTT := createTicketTypeWithCapacity(t, env, firstSession, firstEvent, "GA", 1000, 50)

	// A second Organization with its own Org Admin, Event, and Ticket Type.
	secondSession := verifyOTP(t, env, "second-admin@example.com")
	createOrganization(t, env, secondSession, "Second Org", "second-org")
	secondEvent := createDraftEvent(t, env, secondSession, "Org Two Fest", "org-two-fest")
	secondTT := createTicketTypeWithCapacity(t, env, secondSession, secondEvent, "GA", 1000, 50)

	firstRef := importOneSale(t, env, firstSession, firstEvent, "cust-org-1", firstTT, "ana@example.com", "Ana", "Lopez")
	secondRef := importOneSale(t, env, secondSession, secondEvent, "cust-org-2", secondTT, "ana@example.com", "Ana", "Lopez")

	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 spanning both Organizations", n)
	}
	customer := readCustomer(t, env, "ana@example.com")
	if got := readSaleByRef(t, env, firstRef).CustomerID; got != customer.ID {
		t.Fatalf("first Organization's sale customer_id = %q, want %q", got, customer.ID)
	}
	if got := readSaleByRef(t, env, secondRef).CustomerID; got != customer.ID {
		t.Fatalf("second Organization's sale customer_id = %q, want %q", got, customer.ID)
	}
}

// TestUnverifiedCustomerNameRefreshedByLaterSale proves a record assembled on
// someone's behalf self-corrects: while the Customer is unverified, a later sale
// improves the profile name — and the earlier Ticket Sale keeps the name it
// recorded, because an Org Admin reconciling their Sales list must see exactly
// what was recorded at the time.
func TestUnverifiedCustomerNameRefreshedByLaterSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Typo Fest", "typo-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// A box office records a nickname and a misspelled surname.
	typoRef := importOneSale(t, env, sessionID, eventID, "cust-refresh-1", ttID, "ana@example.com", "Annie", "Lpoez")
	if c := readCustomer(t, env, "ana@example.com"); c.FirstName != "Annie" || c.LastName != "Lpoez" {
		t.Fatalf("customer name = %q / %q, want Annie / Lpoez", c.FirstName, c.LastName)
	}

	// A later, better-spelled sale refreshes the unverified profile name.
	goodRef := importOneSale(t, env, sessionID, eventID, "cust-refresh-2", ttID, "ana@example.com", "Ana", "Lopez")

	customer := readCustomer(t, env, "ana@example.com")
	if customer.FirstName != "Ana" || customer.LastName != "Lopez" {
		t.Fatalf("customer name = %q / %q, want the refreshed Ana / Lopez", customer.FirstName, customer.LastName)
	}
	if customer.VerifiedAt.Valid {
		t.Fatalf("verified_at = %v, want null — no sale ever verifies a Customer", customer.VerifiedAt.Time)
	}

	// Neither Ticket Sale's own recorded name was rewritten.
	if typoSale := readSaleByRef(t, env, typoRef); typoSale.FirstName != "Annie" || typoSale.LastName != "Lpoez" {
		t.Fatalf("first sale recorded name = %q / %q, want the immutable Annie / Lpoez", typoSale.FirstName, typoSale.LastName)
	}
	if goodSale := readSaleByRef(t, env, goodRef); goodSale.FirstName != "Ana" || goodSale.LastName != "Lopez" {
		t.Fatalf("second sale recorded name = %q / %q, want Ana / Lopez", goodSale.FirstName, goodSale.LastName)
	}
}

// TestVerifiedCustomerNameNotOverwrittenByLaterSale proves a person owns their
// own name once they have claimed it: a promoter's later spreadsheet cannot
// rename a Verified Customer. There is no sign-in yet, so the verified state is
// seeded directly with SQL — the only way to reach it in this ticket.
func TestVerifiedCustomerNameNotOverwrittenByLaterSale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Claimed Fest", "claimed-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	firstRef := importOneSale(t, env, sessionID, eventID, "cust-verified-1", ttID, "ana@example.com", "Ana", "Lopez")
	customer := readCustomer(t, env, "ana@example.com")

	// Seed the Verified Customer state and the name she asserts. SQL because no
	// sign-in exists yet: nothing in this ticket can set verified_at.
	verifiedAt := time.Date(2026, 7, 6, 9, 0, 0, 0, time.UTC)
	if _, err := env.db.Exec(`
		UPDATE customers SET verified_at = $1, first_name = 'Ana María', last_name = 'López'
		WHERE id = $2
	`, verifiedAt, customer.ID); err != nil {
		t.Fatalf("seed verified customer: %v", err)
	}

	// A later sale records a different name for the same email.
	laterRef := importOneSale(t, env, sessionID, eventID, "cust-verified-2", ttID, "ana@example.com", "Anna", "Lopes")

	// The verified profile name is untouched, and the record is still the same one.
	after := readCustomer(t, env, "ana@example.com")
	if after.ID != customer.ID {
		t.Fatalf("customer id = %q, want the reused %q", after.ID, customer.ID)
	}
	if after.FirstName != "Ana María" || after.LastName != "López" {
		t.Fatalf("customer name = %q / %q, want the person-owned Ana María / López", after.FirstName, after.LastName)
	}
	if !after.VerifiedAt.Valid {
		t.Fatal("verified_at was cleared; a sale must not unverify a Customer")
	}
	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1", n)
	}

	// Each Ticket Sale still holds exactly the name it recorded.
	if s := readSaleByRef(t, env, firstRef); s.FirstName != "Ana" || s.LastName != "Lopez" {
		t.Fatalf("first sale recorded name = %q / %q, want Ana / Lopez", s.FirstName, s.LastName)
	}
	later := readSaleByRef(t, env, laterRef)
	if later.FirstName != "Anna" || later.LastName != "Lopes" {
		t.Fatalf("later sale recorded name = %q / %q, want Anna / Lopes", later.FirstName, later.LastName)
	}
	if later.CustomerID != customer.ID {
		t.Fatalf("later sale customer_id = %q, want %q", later.CustomerID, customer.ID)
	}
}

// TestSaleFillsNameOfCustomerVerifiedBeforeBuying proves the one case where a
// sale may write onto a Verified Customer: the person who signed in before ever
// buying. Verification builds their record from an email alone, so it carries no
// name for them to own; their first Ticket Sale fills that blank in rather than
// leaving them permanently nameless. Verification itself is untouched — the
// record stays verified — and the sale keeps its own recorded name.
func TestSaleFillsNameOfCustomerVerifiedBeforeBuying(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Nameless Fest", "nameless-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// She signs in before buying anything: a Verified Customer with no name.
	customerSignIn(t, env, "ana@example.com")
	before := readCustomer(t, env, "ana@example.com")
	if before.FirstName != "" || before.LastName != "" {
		t.Fatalf("customer name = %q / %q, want empty — verification builds the record from an email alone",
			before.FirstName, before.LastName)
	}
	if !before.VerifiedAt.Valid {
		t.Fatal("verified_at is null after a completed passcode verification")
	}

	// Her first purchase supplies the name nothing had yet.
	ref := importOneSale(t, env, sessionID, eventID, "cust-fill-1", ttID, "ana@example.com", "Ana", "Lopez")

	after := readCustomer(t, env, "ana@example.com")
	if after.ID != before.ID {
		t.Fatalf("customer id = %q, want the reused %q", after.ID, before.ID)
	}
	if after.FirstName != "Ana" || after.LastName != "Lopez" {
		t.Fatalf("customer name = %q / %q, want the first sale to fill in Ana / Lopez",
			after.FirstName, after.LastName)
	}
	if !after.VerifiedAt.Valid || !after.VerifiedAt.Time.Equal(before.VerifiedAt.Time) {
		t.Fatalf("verified_at = %v, want the preserved %v — a sale must not restamp verification",
			after.VerifiedAt, before.VerifiedAt.Time)
	}
	if n := countCustomers(t, env); n != 1 {
		t.Fatalf("customers = %d, want 1 — signing in then buying is one person", n)
	}

	// The Ticket Sale still holds exactly what was transacted.
	if s := readSaleByRef(t, env, ref); s.FirstName != "Ana" || s.LastName != "Lopez" || s.CustomerID != after.ID {
		t.Fatalf("sale = %q / %q / %q, want Ana / Lopez / %q", s.FirstName, s.LastName, s.CustomerID, after.ID)
	}
}

// TestSaleDoesNotRenameCustomerWhoSignedInAfterBuying proves the closing half of
// that rule over the real sign-in path: once a name is there, verifying protects
// it, and a promoter's later spreadsheet cannot rename the person. The name here
// arrived from her own first sale rather than from SQL seeding, which is how
// almost every Verified Customer will actually acquire one.
func TestSaleDoesNotRenameCustomerWhoSignedInAfterBuying(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Claimed Later Fest", "claimed-later-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	firstRef := importOneSale(t, env, sessionID, eventID, "cust-claim-1", ttID, "ana@example.com", "Ana", "Lopez")

	// She then claims the record by proving she owns the address.
	customerSignIn(t, env, "ana@example.com")
	claimed := readCustomer(t, env, "ana@example.com")
	if !claimed.VerifiedAt.Valid {
		t.Fatal("verified_at is null after a completed passcode verification")
	}
	if claimed.FirstName != "Ana" || claimed.LastName != "Lopez" {
		t.Fatalf("customer name = %q / %q, want signing in to leave Ana / Lopez alone",
			claimed.FirstName, claimed.LastName)
	}

	// A later sale for the same email records a different name.
	laterRef := importOneSale(t, env, sessionID, eventID, "cust-claim-2", ttID, "ana@example.com", "Anna", "Lopes")

	after := readCustomer(t, env, "ana@example.com")
	if after.ID != claimed.ID {
		t.Fatalf("customer id = %q, want the reused %q", after.ID, claimed.ID)
	}
	if after.FirstName != "Ana" || after.LastName != "Lopez" {
		t.Fatalf("customer name = %q / %q, want the person-owned Ana / Lopez", after.FirstName, after.LastName)
	}
	if !after.VerifiedAt.Valid {
		t.Fatal("verified_at was cleared; a sale must not unverify a Customer")
	}

	// Both Ticket Sales still hold exactly the names they recorded.
	if s := readSaleByRef(t, env, firstRef); s.FirstName != "Ana" || s.LastName != "Lopez" {
		t.Fatalf("first sale recorded name = %q / %q, want Ana / Lopez", s.FirstName, s.LastName)
	}
	if s := readSaleByRef(t, env, laterRef); s.FirstName != "Anna" || s.LastName != "Lopes" {
		t.Fatalf("later sale recorded name = %q / %q, want Anna / Lopes", s.FirstName, s.LastName)
	}
}

// TestEveryTicketSaleReferencesACustomer proves the schema enforces the
// invariant rather than leaving it to application code: ticket_sales.customer_id
// is NOT NULL, so no Ticket Sale can exist without a Customer.
func TestEveryTicketSaleReferencesACustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Enforced Fest", "enforced-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	commitBatch(t, env, sessionID, eventID, "cust-enforced", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": ttID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
			"ticket_type_id": ttID, "quantity": 2, "payment_method": "transfer", "sold_at": "2026-07-02T10:00:00Z"},
	})

	var orphans int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales WHERE customer_id IS NULL`).Scan(&orphans); err != nil {
		t.Fatalf("count orphan sales: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("ticket sales without a Customer = %d, want 0", orphans)
	}
	if n := countCustomers(t, env); n != 2 {
		t.Fatalf("customers = %d, want 2", n)
	}

	// The column is NOT NULL, so the guarantee survives any future write path.
	var nullable string
	if err := env.db.QueryRow(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'ticket_sales' AND column_name = 'customer_id'
	`).Scan(&nullable); err != nil {
		t.Fatalf("read customer_id nullability: %v", err)
	}
	if nullable != "NO" {
		t.Fatalf("ticket_sales.customer_id is_nullable = %q, want NO", nullable)
	}
}

// TestFailedImportLeavesNoCustomer proves a Sale Import that fails and rolls
// back leaves no Customer behind for a sale that was never recorded — the
// Customer upsert shares the batch's all-or-nothing transaction rather than
// persisting independently of it.
func TestFailedImportLeavesNoCustomer(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rollback Fest", "rollback-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 5)

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports", map[string]any{
		"idempotency_key": "cust-rollback",
		"source":          "direct",
		"sales": []map[string]any{
			{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
				"ticket_type_id": ttID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
			{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
				"ticket_type_id": ttID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-01T10:00:00Z"},
		},
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "IMPORT_BATCH_FAILED" {
		t.Fatalf("error = %+v, want IMPORT_BATCH_FAILED", body.Error)
	}

	if n := countCustomers(t, env); n != 0 {
		t.Fatalf("customers = %d, want 0 — the upsert must roll back with the sale", n)
	}
}
