package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The corrected address accepts the Re-addressing Link and the Sale becomes
// theirs (issue #421, parent #419, ADR 0058).
//
// WHAT THIS SLICE IS. #420 recorded the address the buyer meant and mailed it a
// Re-addressing Link; nothing moved. This is the click. It is Proof of Email
// Ownership (ADR 0035): it mints or matches a Verified Customer exactly as
// accepting an Assignment Link does, and in ONE transaction the Sale's Customer
// and snapshot email move, the Self-held Ticket follows if its Holder is still
// the wrong address, the record is stamped accepted, and a fresh Sale
// Confirmation goes to the corrected inbox. The accept returns a Customer
// Session on the same terms as a passcode sign-in — which means the consent
// gate applies, and a person who has never accepted the Policy is handed the
// consent step rather than a session (ADR 0035).
//
// EVERYTHING TRANSACTED STAYS. The Sale's reference, snapshot name, timestamps
// and money figures; the Payment row and the snapshot the provider was given;
// the Platform Fee; the Reversal Window; every other Ticket's Holder; consent
// records of either Customer. Each is asserted here against what an actor can
// read, and the wrong address is mailed nothing at any point.
//
// THE PAYMENT PROVIDER IS NEVER CALLED. The tracer buys through the fake
// PayPhone server so the stub can be asked to have done nothing.
//
// Withdraw, replace and resend are #423, the Storefront page is #422, and the
// purge / reversal-kills-pending behaviour is #424; the one place those touch
// this file is the "no longer valid" refusal, which #421 owes already.

const reAddressingLinkPath = "/api/v1/public/re-addressing-link"

// reAddressingLinkView is the public GET view: what the page shows before the
// button. It names the Event, the reference and the corrected address — and
// nothing else about the purchase.
type reAddressingLinkView struct {
	EventName       string  `json:"event_name"`
	ConfirmationRef string  `json:"confirmation_ref"`
	CorrectedEmail  string  `json:"corrected_email"`
	AcceptedAt      *string `json:"accepted_at"`
}

// reAddressingAccepted is the public POST result: the view plus where the buyer
// lands, plus the sign-in outcome on the passcode's own terms.
type reAddressingAccepted struct {
	TicketSaleID    string  `json:"ticket_sale_id"`
	EventName       string  `json:"event_name"`
	ConfirmationRef string  `json:"confirmation_ref"`
	CorrectedEmail  string  `json:"corrected_email"`
	AcceptedAt      *string `json:"accepted_at"`
	customerVerifyData
}

func viewReAddressingLink(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	return env.get(t, reAddressingLinkPath+"?token="+url.QueryEscape(token), nil)
}

func viewReAddressingLinkOK(t *testing.T, env *testEnv, token string) reAddressingLinkView {
	t.Helper()
	resp, body := viewReAddressingLink(t, payphoneEnv, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("view re-addressing link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out reAddressingLinkView
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode re-addressing link view: %v", err)
	}
	return out
}

func acceptReAddressingLink(t *testing.T, env *testEnv, token string) (*http.Response, envelope) {
	t.Helper()
	return env.post(t, reAddressingLinkPath, map[string]string{"token": token}, nil)
}

func acceptReAddressingLinkOK(t *testing.T, env *testEnv, token string) reAddressingAccepted {
	t.Helper()
	resp, body := acceptReAddressingLink(t, payphoneEnv, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept re-addressing link status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out reAddressingAccepted
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode re-addressing accept: %v", err)
	}
	if out.SessionID == "" && out.ConsentRequired == nil {
		t.Fatalf("accept returned neither a Customer Session nor a consent step: %s", body.Data)
	}
	return out
}

// acceptAndSignIn accepts and finishes the sign-in the way the Storefront page
// will: straight through when a session came back, or via the consent step
// when the Policy was outstanding — exactly what a passcode sign-in does.
func acceptAndSignIn(t *testing.T, env *testEnv, token string) (reAddressingAccepted, string) {
	t.Helper()
	accepted := acceptReAddressingLinkOK(t, payphoneEnv, token)
	return accepted, completeConsentStep(t, env, accepted.customerVerifyData).SessionID
}

// assertNothingWentToTheWrongAddress fails if ANY captured mail of the kinds a
// Sale can produce went to the address that was wrong. It is the wrong-address
// rule of ADR 0058 read off the whole outbox rather than one list.
func assertNothingWentToTheWrongAddress(t *testing.T, env *testEnv, wrong string) {
	t.Helper()
	for _, m := range env.email.Confirmations() {
		if m.To == wrong {
			t.Errorf("a Sale Confirmation went to the wrong address %s", wrong)
		}
	}
	for _, m := range env.email.Voided() {
		if m.To == wrong {
			t.Errorf("a void notice went to the wrong address %s", wrong)
		}
	}
	for _, m := range env.email.SaleReAddressingsSent() {
		if m.To == wrong {
			t.Errorf("a Re-addressing mail went to the wrong address %s", wrong)
		}
	}
	for _, m := range env.email.TicketAssignmentsSent() {
		if m.To == wrong {
			t.Errorf("an Assignment mail went to the wrong address %s", wrong)
		}
	}
	for _, m := range env.email.NoLongerHoldingsSent() {
		if m.To == wrong {
			t.Errorf("a no-longer-holding notice went to the wrong address %s", wrong)
		}
	}
}

// sessionFacts is the slice of the Customer Session view the carry rule is
// read off: who the platform now says this person is.
type sessionFacts struct {
	Email     string  `json:"email"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	TaxIDType *string `json:"tax_id_type"`
	Phone     *string `json:"phone"`
}

func readSessionFacts(t *testing.T, env *testEnv, session string) sessionFacts {
	t.Helper()
	resp, body := env.get(t, "/api/v1/customer/auth/session", authHeader(session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var me sessionFacts
	if err := json.Unmarshal(body.Data, &me); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return me
}

// enableTicketAssignmentThroughPayPhone opens the assignment feature on the
// PayPhone app too, whose checkout is where every stranded Sale below is
// bought: without it that checkout seats nobody on Ticket 1, and there is no
// Self-held Ticket to follow the Sale.
func enableTicketAssignmentThroughPayPhone(t *testing.T) {
	t.Helper()
	payphoneApp.CatalogService.WithTicketAssignment(true)
	payphoneApp.SalesService.WithTicketAssignment(true)
	t.Cleanup(func() {
		payphoneApp.CatalogService.WithTicketAssignment(false)
		payphoneApp.SalesService.WithTicketAssignment(false)
	})
}

func consentRecordCount(t *testing.T, env *testEnv, email string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM consent_records cr
		JOIN customers c ON c.id = cr.customer_id
		WHERE c.email = $1
	`, email).Scan(&n); err != nil {
		t.Fatalf("count consent records of %s: %v", email, err)
	}
	return n
}

// paymentSnapshot is the buyer snapshot the Payment Provider was handed, read
// off the Payment row. It must never move: it is what the provider was told,
// and what a dispute would be settled against.
type paymentSnapshot struct {
	email, firstName, lastName, status string
	amountCents                        int
}

func readPaymentSnapshot(t *testing.T, env *testEnv, clientTransactionID string) paymentSnapshot {
	t.Helper()
	var p paymentSnapshot
	if err := env.db.QueryRow(`
		SELECT customer_email, customer_first_name, customer_last_name, status, amount_cents
		FROM payments WHERE client_transaction_id = $1
	`, clientTransactionID).Scan(&p.email, &p.firstName, &p.lastName, &p.status, &p.amountCents); err != nil {
		t.Fatalf("read the Payment %s: %v", clientTransactionID, err)
	}
	return p
}

// strandedSale is one Sale bought under the wrong address and re-addressed by
// the Operator, with the link plucked from the corrected inbox — the starting
// state of every test below.
type strandedSale struct {
	eventID, gaID, ref, saleID, clientTransactionID string
	orgSession, operator                            string
	// token is the Re-addressing Link's token. It verifies against the app
	// that minted it — the PayPhone one, since each test app derives its own
	// link secret — so every open and accept below goes through payphoneEnv.
	token string
}

// strandSale buys `quantity` GA Tickets as `wrong` through PayPhone, has the
// Operator record `corrected`, and returns the link's token. The outbox is
// reset after the recording, so what every test reads is what the click
// produced.
func strandSale(t *testing.T, env *testEnv, name, slug, wrong, corrected string, quantity int) strandedSale {
	t.Helper()
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishEventStarting(t, env, sessionID, name, slug,
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref, ctid := buyOnlineThroughPayPhone(t, slug, gaID, wrong, quantity)
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()
	reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{
		Email: corrected,
		Note:  strPtr("buyer wrote in from the support address"),
	})
	token := reAddressingTokenFrom(t, reAddressingMailFor(t, env, corrected))
	env.email.Reset()
	return strandedSale{
		eventID: eventID, gaID: gaID, ref: ref,
		saleID:              saleIDOfPayment(t, env, ctid),
		clientTransactionID: ctid,
		orgSession:          sessionID,
		operator:            operatorSessionID,
		token:               token,
	}
}

// TestTheCorrectedAddressAcceptsTheReAddressingLinkAndTheSaleBecomesTheirs is
// the tracer bullet: the view names the purchase, the click moves the Sale and
// its Self-held Ticket, a fresh Sale Confirmation reaches the corrected inbox,
// the buyer lands signed in, every transacted fact stays, every other Holder
// keeps their Ticket, and the provider is never asked to do anything.
func TestTheCorrectedAddressAcceptsTheReAddressingLinkAndTheSaleBecomesTheirs(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	s := strandSale(t, env, "Accept Fest", "readdress-accept-fest", "ana.lopes@example.com", "ana.lopez@example.com", 2)
	orgSession := s.orgSession

	// A second Ticket of the Sale has its own Holder, who accepted: a fact that
	// belongs to Carla and must survive the Sale changing hands.
	ghost := buyerSession(t, env, "ana.lopes@example.com")
	tickets := listBuyerTickets(t, env, ghost, s.saleID)
	var otherTicketID string
	for _, tk := range tickets {
		if tk.Ordinal == 2 {
			otherTicketID = tk.TicketID
		}
	}
	assignTicketOK(t, env, ghost, s.saleID, otherTicketID, "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))
	env.email.Reset()

	// THE WORLD BEFORE THE CLICK, as the Operator and the Organization read it.
	before, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if before.Sale.Customer.Email != "ana.lopes@example.com" || before.ReAddressing.Pending == nil {
		t.Fatalf("before the click the sale reads %+v / pending=%v", before.Sale.Customer, before.ReAddressing.Pending)
	}
	pendingID := before.ReAddressing.Pending.ID
	payment := readPaymentSnapshot(t, env, s.clientTransactionID)
	resp, body := env.get(t, "/api/v1/staff/events/"+s.eventID+"/sales", authHeader(orgSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if rows := salesList(t, body).Data; len(rows) != 1 || rows[0].CustomerEmail != "ana.lopes@example.com" {
		t.Fatalf("before acceptance the Sales list shows %+v, want the old address", rows)
	}
	_, exportBefore := downloadSalesExport(t, env, orgSession, s.eventID, "")
	openSalesExport(t, exportBefore).rowOf(t, "ana.lopes@example.com")
	ghostConsents := consentRecordCount(t, env, "ana.lopes@example.com")
	if _, exists := readHolderCustomer(t, env, "ana.lopez@example.com"); exists {
		t.Fatal("the fixture already knows ana.lopez@example.com; this test is about an address nobody has proven")
	}

	// THE VIEW: what the page shows before the button. The Event, the
	// reference, the corrected address — and no session, since nothing has
	// been proven yet.
	view := viewReAddressingLinkOK(t, payphoneEnv, s.token)
	if view.EventName != "Accept Fest" || view.ConfirmationRef != s.ref || view.CorrectedEmail != "ana.lopez@example.com" || view.AcceptedAt != nil {
		t.Fatalf("view = %+v, want the Event, the reference and the corrected address, unaccepted", view)
	}
	if len(env.email.Confirmations()) != 0 {
		t.Fatal("viewing the link sent a Sale Confirmation; only the click moves anything")
	}

	// THE CLICK.
	accepted, session := acceptAndSignIn(t, payphoneEnv, s.token)

	// The provider was asked to do nothing: no money moves when a Sale is
	// re-addressed.
	if got := payphoneStub.reverseCount(); got != 0 {
		t.Fatalf("PayPhone was asked to reverse %d times; accepting a re-addressing moves no money", got)
	}

	if accepted.TicketSaleID != s.saleID || accepted.ConfirmationRef != s.ref || accepted.EventName != "Accept Fest" ||
		accepted.CorrectedEmail != "ana.lopez@example.com" {
		t.Fatalf("accepted = %+v, want the Sale the buyer lands on", accepted)
	}
	if accepted.AcceptedAt == nil || *accepted.AcceptedAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Fatalf("accepted_at = %v, want the server's clock", accepted.AcceptedAt)
	}

	// A VERIFIED CUSTOMER NOW EXISTS FOR THE CORRECTED ADDRESS, minted by the
	// click and by no passcode. Nobody had named them, so the ghost's name
	// carried across.
	customer, exists := readHolderCustomer(t, env, "ana.lopez@example.com")
	if !exists || !customer.verifiedAt.Valid {
		t.Fatalf("customer=%+v exists=%v; the click mints a Verified Customer (ADR 0058)", customer, exists)
	}
	if customer.firstName != "Ana" || customer.lastName != "Lopez" {
		t.Errorf("the nameless Customer reads %q %q, want the ghost's name carried across", customer.firstName, customer.lastName)
	}
	if me := readSessionFacts(t, env, session); me.FirstName != "Ana" || me.TaxIDType == nil || *me.TaxIDType != "cedula" {
		t.Errorf("the corrected Customer's own view reads %+v, want the ghost's name and Tax ID carried", me)
	}

	// THE SESSION IS THE CORRECTED ADDRESS'S, and it reaches the Sale.
	area := readCustomerArea(t, env, session, "")
	if sale := onlySale(t, area); sale.ID != s.saleID || sale.ConfirmationRef != s.ref {
		t.Fatalf("the corrected Customer's Area shows %+v, want the re-addressed Sale", sale)
	}
	// AND THE GHOST NO LONGER HAS IT.
	if ghostArea := readCustomerArea(t, env, ghost, ""); len(ghostArea.Upcoming)+len(ghostArea.Past) != 0 {
		t.Errorf("the ghost's Area still lists %d Sales after the click", len(ghostArea.Upcoming)+len(ghostArea.Past))
	}

	// THE SELF-HELD TICKET FOLLOWED; CARLA'S DID NOT.
	own := listBuyerTickets(t, env, session, s.saleID)
	for _, tk := range own {
		switch tk.Ordinal {
		case 1:
			if tk.HolderEmail != "ana.lopez@example.com" || tk.AssignmentState != "accepted" || !tk.SelfHeld {
				t.Errorf("Ticket 1 reads holder=%q state=%q self_held=%v; want the corrected address holding it", tk.HolderEmail, tk.AssignmentState, tk.SelfHeld)
			}
		case 2:
			if tk.HolderEmail != "carla@example.com" || tk.AssignmentState != "accepted" {
				t.Errorf("Ticket 2 reads holder=%q state=%q; another Holder's Ticket must not move", tk.HolderEmail, tk.AssignmentState)
			}
		}
	}
	rows := importedTicketRows(t, env, s.saleID)
	if rows[0].holder == nil || *rows[0].holder != "ana.lopez@example.com" || rows[1].holder == nil || *rows[1].holder != "carla@example.com" {
		t.Errorf("ticket holders after the click = %v / %v", rows[0].holder, rows[1].holder)
	}

	// ONE FRESH SALE CONFIRMATION, TO THE CORRECTED ADDRESS, naming the Sale —
	// and nothing to the wrong one, and no void notice: nothing was undone.
	confirmations := env.email.Confirmations()
	if len(confirmations) != 1 || confirmations[0].To != "ana.lopez@example.com" || confirmations[0].Reference != s.ref {
		t.Fatalf("captured Sale Confirmations = %+v, want exactly one to the corrected address for %s", confirmations, s.ref)
	}
	if confirmations[0].EventName != "Accept Fest" || confirmations[0].ConfirmationLink == "" {
		t.Errorf("the fresh Confirmation reads %+v, want the Event and a Confirmation Link", confirmations[0])
	}
	if len(env.email.Voided()) != 0 || len(env.email.SaleReAddressingsSent()) != 0 {
		t.Errorf("the click sent %d void notices and %d Re-addressing mails, want none", len(env.email.Voided()), len(env.email.SaleReAddressingsSent()))
	}
	assertNothingWentToTheWrongAddress(t, env, "ana.lopes@example.com")

	// EVERYTHING TRANSACTED STAYS. The Operator reads the same Sale — same id,
	// reference, snapshot name, timestamps, money and Reversal Window — with
	// only the address changed.
	after, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if after.Sale.Customer.Email != "ana.lopez@example.com" {
		t.Fatalf("after the click the Operator sees %q, want the corrected address", after.Sale.Customer.Email)
	}
	if after.Sale.ID != before.Sale.ID || after.Sale.ConfirmationRef != before.Sale.ConfirmationRef ||
		after.Sale.Status != "active" || after.Sale.SoldAt != before.Sale.SoldAt || after.Sale.RecordedAt != before.Sale.RecordedAt ||
		after.Sale.Customer.FirstName != before.Sale.Customer.FirstName || after.Sale.Customer.LastName != before.Sale.Customer.LastName ||
		after.Sale.AmountCents != before.Sale.AmountCents || after.Sale.PlatformFeeCents != before.Sale.PlatformFeeCents ||
		after.Sale.FeeIVACents != before.Sale.FeeIVACents || after.Sale.NetProceedsCents != before.Sale.NetProceedsCents ||
		after.Sale.TicketCount != before.Sale.TicketCount {
		t.Errorf("the Sale changed beyond its address:\nbefore %+v\nafter  %+v", before.Sale, after.Sale)
	}
	if before.Sale.ReversalWindowClosesAt == nil || after.Sale.ReversalWindowClosesAt == nil ||
		*after.Sale.ReversalWindowClosesAt != *before.Sale.ReversalWindowClosesAt {
		t.Errorf("the Reversal Window moved from %v to %v; it is not restarted", before.Sale.ReversalWindowClosesAt, after.Sale.ReversalWindowClosesAt)
	}
	if got := readPaymentSnapshot(t, env, s.clientTransactionID); got != payment {
		t.Errorf("the Payment row changed from %+v to %+v; the snapshot the provider was given stays", payment, got)
	}
	if got := consentRecordCount(t, env, "ana.lopes@example.com"); got != ghostConsents {
		t.Errorf("the ghost's consent records went from %d to %d; consent does not follow", ghostConsents, got)
	}

	// THE OPERATOR LOOKUP SHOWS THE ACCEPTED RECORD AND NO PENDING ONE.
	if after.ReAddressing.Pending != nil {
		t.Errorf("re_addressing.pending = %+v after acceptance, want null", after.ReAddressing.Pending)
	}
	if len(after.ReAddressing.Accepted) != 1 {
		t.Fatalf("re_addressing.accepted = %+v, want the one record", after.ReAddressing.Accepted)
	}
	rec := after.ReAddressing.Accepted[0]
	if rec.ID != pendingID || rec.Status != "accepted" || rec.PreviousEmail != "ana.lopes@example.com" ||
		rec.CorrectedEmail == nil || *rec.CorrectedEmail != "ana.lopez@example.com" ||
		rec.Operator != "operator@example.com" || rec.Note == nil || *rec.Note != "buyer wrote in from the support address" ||
		rec.RequestedAt == "" || rec.AcceptedAt == nil || *rec.AcceptedAt != env.fixedClock.UTC().Format(time.RFC3339) || rec.WithdrawnAt != nil {
		t.Errorf("accepted record = %+v, want old address, new address, operator, requested-at, accepted-at and note", rec)
	}

	// THE ORGANIZATION SEES THE OUTCOME: the Sales list and the export carry the
	// corrected address now.
	resp, body = env.get(t, "/api/v1/staff/events/"+s.eventID+"/sales", authHeader(orgSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	listed := salesList(t, body).Data
	if len(listed) != 1 || listed[0].CustomerEmail != "ana.lopez@example.com" ||
		listed[0].CustomerFirstName != "Ana" || listed[0].CustomerLastName != "Lopez" || listed[0].ConfirmationRef != s.ref {
		t.Errorf("after acceptance the Sales list shows %+v, want the corrected address on the same Sale", listed)
	}
	_, exportAfter := downloadSalesExport(t, env, orgSession, s.eventID, "")
	sheet := openSalesExport(t, exportAfter)
	sheet.rowOf(t, "ana.lopez@example.com")
	for _, email := range sheet.column(t, "customer_email") {
		if email == "ana.lopes@example.com" {
			t.Error("the export still carries the wrong address after acceptance")
		}
	}

	// THE VIEW AFTER THE CLICK says so, for a page opened twice.
	if again := viewReAddressingLinkOK(t, payphoneEnv, s.token); again.AcceptedAt == nil {
		t.Error("the view after acceptance does not say the Sale has been accepted")
	}
}

// TestReAddressingCarriesTheGhostsFactsOnlyIntoANamelessCustomer: name, Tax ID
// and phone travel from the ghost into a Customer nobody has named; an
// existing Customer's own facts win, and the Sale's snapshot keeps what was
// typed either way (ADR 0058).
func TestReAddressingCarriesTheGhostsFactsOnlyIntoANamelessCustomer(t *testing.T) {
	env := setupTest(t)

	// Beatriz is a Customer in her own right, with her own name and phone. The
	// ghost's Tax ID is the checkout's cédula; hers is none — and none is what
	// she keeps, because a fact about her is hers to state.
	bea := customerSignIn(t, env, "bea@example.com")
	resp, body := env.patch(t, "/api/v1/customer/profile", map[string]any{
		"first_name": "Beatriz", "last_name": "Silva", "phone": "+593987654321",
	}, authHeader(bea))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("profile status=%d error=%+v", resp.StatusCode, body.Error)
	}
	s := strandSale(t, env, "Facts Fest", "readdress-facts-fest", "ana.lopes@example.com", "bea@example.com", 1)

	accepted, session := acceptAndSignIn(t, payphoneEnv, s.token)
	if accepted.TicketSaleID != s.saleID {
		t.Fatalf("accepted = %+v", accepted)
	}
	me := readSessionFacts(t, env, session)
	if me.Email != "bea@example.com" || me.FirstName != "Beatriz" || me.LastName != "Silva" ||
		me.Phone == nil || *me.Phone != "+593987654321" || me.TaxIDType != nil {
		t.Errorf("the existing Customer reads %+v after accepting; her own facts win over the ghost's", me)
	}
	// THE SALE'S SNAPSHOT KEEPS WHAT WAS TYPED.
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.Sale.Customer.Email != "bea@example.com" || found.Sale.Customer.FirstName != "Ana" || found.Sale.Customer.LastName != "Lopez" {
		t.Errorf("the Sale reads %+v; the address moves and the snapshot name stays", found.Sale.Customer)
	}
	// The carry INTO a nameless Customer — name and Tax ID — is asserted on
	// the tracer, whose corrected address is nobody's until the click, and the
	// slot-by-slot rule for a nameless Customer who has already stated a fact
	// is the test below.
}

// customerFacts is the whole of what a Customer asserts about themselves,
// read off the row: the carry rule is stated per slot, so it is asserted per
// slot, on both sides of the move.
type customerFacts struct {
	firstName, lastName           string
	taxIDType, taxIDNumber, phone sql.NullString
}

func readCustomerFacts(t *testing.T, env *testEnv, email string) customerFacts {
	t.Helper()
	var f customerFacts
	if err := env.db.QueryRow(`
		SELECT first_name, last_name, tax_id_type, tax_id_number, phone FROM customers WHERE email = $1
	`, email).Scan(&f.firstName, &f.lastName, &f.taxIDType, &f.taxIDNumber, &f.phone); err != nil {
		t.Fatalf("read the facts of Customer %s: %v", email, err)
	}
	return f
}

// TestANamelessCorrectedCustomerKeepsTheFactsTheyStatedAndTakesTheGhostsIntoEmptySlots
// pins the rule slot by slot (#438): a Customer nobody has named takes the
// ghost's name whole, but a Tax ID and a phone carry only into an empty slot.
// Here the corrected address has a phone of its own and no Tax ID, so after
// the click it keeps its phone, gains the ghost's cédula, and is called what
// the ghost was called — and the ghost keeps every fact it had.
func TestANamelessCorrectedCustomerKeepsTheFactsTheyStatedAndTakesTheGhostsIntoEmptySlots(t *testing.T) {
	env := setupTest(t)

	// The ghost has a phone of its own, so that "keeps their own phone" is a
	// choice between two numbers rather than the absence of one.
	ghostPhone := "+593991112233"
	ghost := customerSignIn(t, env, "ana.lopes@example.com")
	resp, body := env.patch(t, "/api/v1/customer/profile", map[string]any{
		"first_name": "Ana", "last_name": "Lopez", "phone": ghostPhone,
	}, authHeader(ghost))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ghost profile status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The corrected address has signed in once and never been named — the
	// profile editor refuses a blank name, so the phone it once stated is
	// placed on the row directly. A nameless Customer with a phone is a state
	// the schema permits and the rule is written for.
	ownPhone := "+593987654321"
	customerSignIn(t, env, "ana.lopez@example.com")
	if _, err := env.db.Exec(`UPDATE customers SET phone = $2 WHERE email = $1`, "ana.lopez@example.com", ownPhone); err != nil {
		t.Fatalf("give the nameless Customer a phone: %v", err)
	}
	if before := readCustomerFacts(t, env, "ana.lopez@example.com"); before.firstName != "" || before.lastName != "" || before.taxIDType.Valid {
		t.Fatalf("the corrected Customer reads %+v before the click, want nameless with no Tax ID", before)
	}

	s := strandSale(t, env, "Slots Fest", "readdress-slots-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)
	ghostBefore := readCustomerFacts(t, env, "ana.lopes@example.com")
	if !ghostBefore.taxIDType.Valid || ghostBefore.taxIDType.String != "cedula" || !ghostBefore.phone.Valid || ghostBefore.phone.String != ghostPhone {
		t.Fatalf("the ghost reads %+v before the click, want the checkout's cédula and its own phone", ghostBefore)
	}

	accepted, session := acceptAndSignIn(t, payphoneEnv, s.token)
	if accepted.TicketSaleID != s.saleID {
		t.Fatalf("accepted = %+v", accepted)
	}

	// THE NAME CARRIES, THE TAX ID FILLS THE EMPTY SLOT, THE PHONE STAYS THEIRS.
	after := readCustomerFacts(t, env, "ana.lopez@example.com")
	if after.firstName != "Ana" || after.lastName != "Lopez" {
		t.Errorf("the nameless Customer reads %q %q after the click, want the ghost's name carried whole", after.firstName, after.lastName)
	}
	if !after.taxIDType.Valid || after.taxIDType.String != "cedula" || !after.taxIDNumber.Valid || after.taxIDNumber.String != validCedula {
		t.Errorf("the corrected Customer's Tax ID reads %+v/%+v after the click, want the ghost's cédula carried into the empty slot", after.taxIDType, after.taxIDNumber)
	}
	if !after.phone.Valid || after.phone.String != ownPhone {
		t.Errorf("the corrected Customer's phone reads %+v after the click, want their own %s kept over the ghost's %s", after.phone, ownPhone, ghostPhone)
	}
	// The same, read the way the person reads it.
	if me := readSessionFacts(t, env, session); me.FirstName != "Ana" || me.TaxIDType == nil || *me.TaxIDType != "cedula" ||
		me.Phone == nil || *me.Phone != ownPhone {
		t.Errorf("the corrected Customer's own view reads %+v", me)
	}

	// THE GHOST KEEPS ITS OWN FACTS: the carry copies, it does not move.
	if ghostAfter := readCustomerFacts(t, env, "ana.lopes@example.com"); ghostAfter != ghostBefore {
		t.Errorf("the ghost reads %+v after the click, want %+v untouched", ghostAfter, ghostBefore)
	}
}

// TestAcceptingTheReAddressingLinkTwiceRewritesNothing: people click twice
// and mail clients prefetch. The second click lands the buyer on the same
// Sale, signed in, keeps the first acceptance's instant and sends no second
// Confirmation.
func TestAcceptingTheReAddressingLinkTwiceRewritesNothing(t *testing.T) {
	env := setupTest(t)
	s := strandSale(t, env, "Twice Fest", "readdress-twice-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)

	first, _ := acceptAndSignIn(t, payphoneEnv, s.token)
	if len(env.email.Confirmations()) != 1 {
		t.Fatalf("captured %d Sale Confirmations after the first click, want 1", len(env.email.Confirmations()))
	}

	// The clock moves, so a rewritten accepted_at would be a DIFFERENT value.
	later := env.fixedClock.Add(48 * time.Hour)
	payphoneApp.SalesService.WithClock(func() time.Time { return later })
	payphoneApp.CustomersService.WithClock(func() time.Time { return later })
	defer payphoneApp.SalesService.WithClock(func() time.Time { return env.fixedClock })
	defer payphoneApp.CustomersService.WithClock(func() time.Time { return env.fixedClock })

	second, session := acceptAndSignIn(t, payphoneEnv, s.token)
	if second.TicketSaleID != first.TicketSaleID || second.AcceptedAt == nil || first.AcceptedAt == nil || *second.AcceptedAt != *first.AcceptedAt {
		t.Errorf("the second click reads %+v, want the first acceptance %+v kept", second, first)
	}
	if session == "" {
		t.Error("the second click minted no session; a link in an inbox must land its reader signed in every time")
	}
	if got := len(env.email.Confirmations()); got != 1 {
		t.Errorf("captured %d Sale Confirmations after the second click, want still 1", got)
	}
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if len(found.ReAddressing.Accepted) != 1 || found.ReAddressing.Accepted[0].AcceptedAt == nil ||
		*found.ReAddressing.Accepted[0].AcceptedAt != env.fixedClock.UTC().Format(time.RFC3339) {
		t.Errorf("accepted history = %+v, want one record stamped at the first click", found.ReAddressing.Accepted)
	}
	var customers int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM customers WHERE email = 'ana.lopez@example.com'`).Scan(&customers); err != nil || customers != 1 {
		t.Errorf("customers for the corrected address = %d (%v), want exactly 1", customers, err)
	}
}

// TestTheReAddressingLinkRefusesATamperedToken: a forged, truncated, retyped or
// cross-purpose token opens nothing, on either verb, and tells its holder
// nothing beyond that. A blank token is a malformed request, not a bad link.
func TestTheReAddressingLinkRefusesATamperedToken(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	enableTicketAssignmentThroughPayPhone(t)
	s := strandSale(t, env, "Tamper Fest", "readdress-tamper-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)

	payload, mac, _ := strings.Cut(s.token, ".")
	bad := map[string]string{
		"a truncated token":   s.token[:len(s.token)-6],
		"a token with no MAC": payload,
		"a swapped signature": payload + "." + strings.Repeat("A", len(mac)),
		"an invented token":   "srl1.notatoken",
		"somebody's words":    "hello",
	}
	// An Assignment Link is a different token with a different power, and
	// fails here cryptographically rather than by a check.
	ghost := buyerSession(t, env, "ana.lopes@example.com")
	ticketID := listBuyerTickets(t, env, ghost, s.saleID)[0].TicketID
	assignTicketOK(t, env, ghost, s.saleID, ticketID, "carla@example.com")
	bad["an Assignment Link token"] = assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	env.email.Reset()

	for name, token := range bad {
		resp, body := viewReAddressingLink(t, payphoneEnv, token)
		assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_INVALID")
		resp, body = acceptReAddressingLink(t, payphoneEnv, token)
		assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_INVALID")
		if body.Error != nil && strings.Contains(strings.ToLower(body.Error.Message), name) {
			t.Errorf("%s produced a refusal that describes it", name)
		}
	}
	for _, blank := range []string{"", "   "} {
		resp, body := acceptReAddressingLink(t, payphoneEnv, blank)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("a blank token answered %d on POST, want 400: %+v", resp.StatusCode, body.Error)
		}
		resp, body = viewReAddressingLink(t, payphoneEnv, blank)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("a blank token answered %d on GET, want 400: %+v", resp.StatusCode, body.Error)
		}
	}

	// Nothing moved and nobody was mailed.
	if len(env.email.Confirmations()) != 0 {
		t.Error("a refused token sent a Sale Confirmation")
	}
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.Sale.Customer.Email != "ana.lopes@example.com" || found.ReAddressing.Pending == nil {
		t.Errorf("after refused tokens the sale reads %+v pending=%v, want untouched", found.Sale.Customer, found.ReAddressing.Pending)
	}
	if _, exists := readHolderCustomer(t, env, "ana.lopez@example.com"); exists {
		t.Error("a refused token minted a Customer")
	}
}

// TestTheReAddressingLinkIsNoLongerValidOnceTheRecordOrTheSaleHasEnded: a link
// that was real once and is not now — the record withdrawn, the Sale reversed,
// the Event started — refuses with its own code, distinct from a forgery, and
// says which in details.reason. Nothing moves, nobody is mailed, and the
// lookup shows no pending record.
func TestTheReAddressingLinkIsNoLongerValidOnceTheRecordOrTheSaleHasEnded(t *testing.T) {
	cases := []struct {
		name, reason string
		end          func(t *testing.T, env *testEnv, s strandedSale)
	}{
		{"a reversed Sale", "sale_reversed", func(t *testing.T, env *testEnv, s strandedSale) {
			operatorReverseOK(t, payphoneEnv, s.operator, s.ref, operatorReversalBody{
				RefundedAmountCents: intPtr(paidRefund(1)),
				PlatformFeeKept:     boolPtr(true),
			})
		}},
		{"a started Event", "event_started", func(t *testing.T, env *testEnv, s strandedSale) {
			setEventStart(t, env, s.eventID, env.fixedClock.Add(-time.Minute))
		}},
		{"a withdrawn record", "withdrawn", func(t *testing.T, env *testEnv, s strandedSale) {
			withdrawReAddressOK(t, payphoneEnv, s.operator, s.ref)
		}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := setupTest(t)
			slug := "readdress-ended-fest-" + string(rune('a'+i))
			s := strandSale(t, env, "Ended Fest", slug, "ana.lopes@example.com", "ana.lopez@example.com", 1)
			// The link opened before the end.
			viewReAddressingLinkOK(t, payphoneEnv, s.token)
			tc.end(t, env, s)
			// The corrected address is told nothing by the ending itself
			// (#424): an Operator Reversal's void notice goes to the address
			// the Sale was sold to, and a withdrawal mails nobody.
			assertNothingWentToTheWrongAddress(t, env, "ana.lopez@example.com")
			env.email.Reset()

			resp, body := viewReAddressingLink(t, payphoneEnv, s.token)
			assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
			resp, body = acceptReAddressingLink(t, payphoneEnv, s.token)
			assertRefused(t, resp, body, http.StatusUnauthorized, "RE_ADDRESSING_LINK_NO_LONGER_VALID")
			if body.Error == nil || answerDetail(body, "reason") != tc.reason {
				t.Errorf("details.reason = %q, want %q", answerDetail(body, "reason"), tc.reason)
			}

			if len(env.email.Confirmations()) != 0 {
				t.Error("a refused click sent a Sale Confirmation")
			}
			if _, exists := readHolderCustomer(t, env, "ana.lopez@example.com"); exists {
				t.Error("a refused click minted a Customer")
			}
			found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
			if found.Sale.Customer.Email != "ana.lopes@example.com" || found.ReAddressing.Pending != nil || len(found.ReAddressing.Accepted) != 0 {
				t.Errorf("the sale reads %+v pending=%v accepted=%v, want untouched with nothing pending",
					found.Sale.Customer, found.ReAddressing.Pending, found.ReAddressing.Accepted)
			}
			if got := payphoneStub.reverseCount(); tc.reason != "sale_reversed" && got != 0 {
				t.Errorf("PayPhone was asked to reverse %d times", got)
			}
		})
	}
}

// TestAcceptingAReAddressingGrantsNoConsent: the click is about a purchase,
// not a newsletter. The Customer it mints has agreed to nothing — which is
// exactly why the sign-in it returns goes through the consent gate rather than
// around it — and neither Customer's consent records move.
func TestAcceptingAReAddressingGrantsNoConsent(t *testing.T) {
	env := setupTest(t)
	s := strandSale(t, env, "Consent Fest", "readdress-consent-fest", "ana.lopes@example.com", "ana.lopez@example.com", 1)
	ghostConsents := consentRecordCount(t, env, "ana.lopes@example.com")
	if ghostConsents == 0 {
		t.Fatal("the fixture's checkout recorded no consent for the ghost; the test needs some to prove they stay")
	}

	accepted := acceptReAddressingLinkOK(t, payphoneEnv, s.token)
	// A person who has never accepted the Policy gets the consent step and no
	// session — the passcode's own terms (ADR 0035), and the proof that the
	// click itself granted nothing.
	if accepted.SessionID != "" || accepted.ConsentRequired == nil || accepted.ConsentRequired.PendingConsentToken == "" {
		t.Fatalf("a fresh Customer's accept returned session=%q consent_required=%+v; want the consent step", accepted.SessionID, accepted.ConsentRequired)
	}
	if got := consentRecordCount(t, env, "ana.lopez@example.com"); got != 0 {
		t.Errorf("accepting wrote %d consent record(s) for somebody who was never asked", got)
	}
	if got := consentRecordCount(t, env, "ana.lopes@example.com"); got != ghostConsents {
		t.Errorf("the ghost's consent records went from %d to %d", ghostConsents, got)
	}
	var digestEnabled bool
	if err := env.db.QueryRow(`SELECT digest_enabled FROM customers WHERE email = 'ana.lopez@example.com'`).Scan(&digestEnabled); err != nil {
		t.Fatalf("read digest flag: %v", err)
	}
	if digestEnabled {
		t.Error("accepting subscribed the corrected address to the Follow Digest")
	}
	// The Sale moved regardless: the consent step withholds only the
	// credential, never what was already proven.
	found, _ := lookUpSaleWithReAddressing(t, env, s.operator, s.ref)
	if found.Sale.Customer.Email != "ana.lopez@example.com" {
		t.Errorf("the sale reads %q after an accept that met the consent gate", found.Sale.Customer.Email)
	}
}

// TestTheFreshSaleConfirmationIsWrittenInTheSalesLocale: the receipt the
// corrected address gets is the Sale's own, in the language the Sale was made
// in, as the original was (ADR 0033, ADR 0058).
func TestTheFreshSaleConfirmationIsWrittenInTheSalesLocale(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	_, gaID := publishEventStarting(t, env, sessionID, "Locale Fest", "readdress-conf-locale-fest",
		env.fixedClock.Add(72*time.Hour), "America/Guayaquil", feeTestBaseCents, 10)
	ref, _ := buyOnlineThroughPayPhoneInLocale(t, "readdress-conf-locale-fest", gaID, "ana.lopes@example.com", 1, "es")
	operatorSessionID := operatorSession(t, env, "operator@example.com")
	env.email.Reset()
	reAddressOK(t, payphoneEnv, operatorSessionID, ref, reAddressBody{Email: "ana.lopez@example.com"})
	token := reAddressingTokenFrom(t, reAddressingMailFor(t, env, "ana.lopez@example.com"))
	env.email.Reset()

	acceptReAddressingLinkOK(t, payphoneEnv, token)
	confirmations := env.email.Confirmations()
	if len(confirmations) != 1 || confirmations[0].To != "ana.lopez@example.com" || confirmations[0].Reference != ref {
		t.Fatalf("captured Sale Confirmations = %+v", confirmations)
	}
	if confirmations[0].Locale != platform.LocaleES {
		t.Errorf("the fresh Confirmation's locale = %q, want es — the Sale's own language", confirmations[0].Locale)
	}
}
