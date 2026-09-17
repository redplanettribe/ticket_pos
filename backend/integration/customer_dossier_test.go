package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Customer Dossier (#638, spec #635; CONTEXT.md "Customer Dossier"):
// everything one Event knows about one Customer, addressed by the Customer's id.
//
// Everything is asserted at the HTTP seam — the Dossier's status and body — with
// the fixtures made through the staff and checkout routes, and the Customer id
// read off the Sales list row that links to the Dossier.

// dossierBody mirrors GET /api/v1/staff/events/{id}/customers/{customerId}/dossier.
type dossierBody struct {
	Customer struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	} `json:"customer"`
	Sales []dossierSale `json:"sales"`
}

type dossierSale struct {
	ID                        string  `json:"id"`
	ConfirmationRef           string  `json:"confirmation_ref"`
	Status                    string  `json:"status"`
	ReversedAt                *string `json:"reversed_at"`
	ReplacedByConfirmationRef *string `json:"replaced_by_confirmation_ref"`
	SoldAt                    string  `json:"sold_at"`
	RecordedAt                string  `json:"recorded_at"`
	Channel                   string  `json:"channel"`
	Source                    *string `json:"source"`
	Origin                    string  `json:"origin"`
	TicketTypes               []struct {
		TicketTypeID   string `json:"ticket_type_id"`
		TicketTypeName string `json:"ticket_type_name"`
		Quantity       int    `json:"quantity"`
	} `json:"ticket_types"`
	AmountCents       int     `json:"amount_cents"`
	Currency          string  `json:"currency"`
	PaymentMethod     *string `json:"payment_method"`
	CustomerFirstName string  `json:"customer_first_name"`
	CustomerLastName  string  `json:"customer_last_name"`
	TaxIDType         *string `json:"tax_id_type"`
	TaxIDNumber       *string `json:"tax_id_number"`
}

func dossierPath(eventID, customerID string) string {
	return "/api/v1/staff/events/" + eventID + "/customers/" + customerID + "/dossier"
}

// readDossier reads a Dossier, failing on any refusal, and returns it with the
// raw payload it was decoded from.
func readDossier(t *testing.T, env *testEnv, sessionID, eventID, customerID string) (dossierBody, json.RawMessage) {
	t.Helper()
	resp, body := env.get(t, dossierPath(eventID, customerID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dossier status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out dossierBody
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode dossier: %v", err)
	}
	return out, body.Data
}

// dossierStatus reads the Dossier's status code alone, for refusals.
func dossierStatus(t *testing.T, env *testEnv, sessionID, eventID, customerID string) (int, string) {
	t.Helper()
	resp, body := env.get(t, dossierPath(eventID, customerID), authHeader(sessionID))
	code := ""
	if body.Error != nil {
		code = body.Error.Code
	}
	return resp.StatusCode, code
}

// customerIDOnSalesList is the Customer id the Sales list row carries for a
// buyer — the id the list links the Dossier by.
func customerIDOnSalesList(t *testing.T, env *testEnv, sessionID, eventID, email, status string) string {
	t.Helper()
	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sales?status="+status, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []struct {
			CustomerID    string `json:"customer_id"`
			CustomerEmail string `json:"customer_email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode sales list: %v", err)
	}
	for _, row := range page.Data {
		if row.CustomerEmail == email {
			if row.CustomerID == "" {
				t.Fatalf("sales list row for %s carries no customer_id", email)
			}
			return row.CustomerID
		}
	}
	t.Fatalf("no %s sale for %s on the sales list", status, email)
	return ""
}

// addOrgMember adds a colleague at the given role and signs them in.
func addOrgMember(t *testing.T, env *testEnv, adminSession, email, role string) string {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
		"email": email, "role": role,
	}, authHeader(adminSession))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
	return verifyOTP(t, env, email)
}

// dossierSaleByRef picks one Sale off a Dossier, failing when it is absent.
func dossierSaleByRef(t *testing.T, dossier dossierBody, ref string) dossierSale {
	t.Helper()
	for _, sale := range dossier.Sales {
		if sale.ConfirmationRef == ref {
			return sale
		}
	}
	t.Fatalf("sale %s is not on the Dossier", ref)
	return dossierSale{}
}

func strOf(p *string) string {
	if p == nil {
		return "<null>"
	}
	return *p
}

// All three roles in one test, as the Holder List's is: the decision is the
// shape of the gate. Org Admin and Event Owner read the same Dossier, Event
// Staff are refused, and a member of another Organization is told the Event
// does not exist.
func TestTheCustomerDossierIsForTheOrgAdminAndTheEventOwner(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Dossier Fest", "dossier-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z"))
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	admin, adminRaw := readDossier(t, env, adminSession, eventID, customerID)
	if len(admin.Sales) != 1 {
		t.Fatalf("Org Admin's Dossier has %d sales, want 1", len(admin.Sales))
	}

	ownerSession := addOrgMember(t, env, adminSession, "owner@example.com", "event_owner")
	_, ownerRaw := readDossier(t, env, ownerSession, eventID, customerID)
	if !bytes.Equal(ownerRaw, adminRaw) {
		t.Errorf("the Event Owner's Dossier differs from the Org Admin's:\nowner %s\nadmin %s", ownerRaw, adminRaw)
	}

	staffSession := addOrgMember(t, env, adminSession, "door@example.com", "event_staff")
	if status, _ := dossierStatus(t, env, staffSession, eventID, customerID); status != http.StatusForbidden {
		t.Errorf("event staff status=%d, want 403", status)
	}

	outsider := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, outsider, "Other Org", "other-org")
	if status, _ := dossierStatus(t, env, outsider, eventID, customerID); status != http.StatusNotFound {
		t.Errorf("another Organization's admin status=%d, want 404", status)
	}
}

// Nothing at this Event is not found, and indistinguishable from nobody: a
// Customer who bought only at another Event of the same Organization, an id
// naming no Customer, and a malformed id all answer the same 404.
func TestTheCustomerDossierIsNotFoundWithoutASaleOnThisEvent(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Here Fest", "here-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	otherEventID := createDraftEvent(t, env, adminSession, "There Fest", "there-fest")
	otherGA := createTicketTypeWithCapacity(t, env, adminSession, otherEventID, "GA", 1000, 50)

	here := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	recordManualSaleOK(t, env, adminSession, otherEventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", otherGA, 3, "transfer", "2026-07-02T10:00:00Z"))
	recordManualSaleOK(t, env, adminSession, otherEventID,
		manualSaleBody("elsewhere@example.com", "Eli", "Vera", otherGA, 1, "cash", "2026-07-02T10:00:00Z"))
	ana := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")
	elsewhere := customerIDOnSalesList(t, env, adminSession, otherEventID, "elsewhere@example.com", "active")

	// Only this Event's Sale is on Ana's Dossier.
	dossier, _ := readDossier(t, env, adminSession, eventID, ana)
	if len(dossier.Sales) != 1 || dossier.Sales[0].ConfirmationRef != here.ConfirmationRef {
		t.Fatalf("Dossier sales = %+v, want only %s", dossier.Sales, here.ConfirmationRef)
	}

	cases := map[string]string{
		"bought only at another Event of the Organization": elsewhere,
		"no such Customer": "7d9f1a52-3c4b-4e1f-9a2b-0c6d8e7f5a31",
		"malformed id":     "not-a-uuid",
	}
	for name, customerID := range cases {
		status, code := dossierStatus(t, env, adminSession, eventID, customerID)
		if status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
			t.Errorf("%s: status=%d code=%s, want 404 CUSTOMER_NOT_FOUND", name, status, code)
		}
	}
}

// The Dossier states what THIS Event was told. A later Sale at another
// Organization, under a different name and Tax ID, rewrites nothing on it.
func TestALaterSaleAtAnotherOrganizationChangesNothingOnTheDossier(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "First Fest", "first-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	body := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z")
	body["customer_tax_id_type"] = "cedula"
	body["customer_tax_id_number"] = validCedula
	recordManualSaleOK(t, env, adminSession, eventID, body)
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")

	_, before := readDossier(t, env, adminSession, eventID, customerID)

	otherSession := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSession, "Other Org", "other-org")
	otherEventID := createDraftEvent(t, env, otherSession, "Other Fest", "other-fest")
	otherGA := createTicketTypeWithCapacity(t, env, otherSession, otherEventID, "GA", 1000, 50)
	later := manualSaleBody("ana@example.com", "Anastasia", "Otra", otherGA, 4, "transfer", "2026-07-05T10:00:00Z")
	later["customer_tax_id_type"] = "ruc"
	later["customer_tax_id_number"] = naturalRUC
	recordManualSaleOK(t, env, otherSession, otherEventID, later)
	if other := customerIDOnSalesList(t, env, otherSession, otherEventID, "ana@example.com", "active"); other != customerID {
		t.Fatalf("the other Organization's sale went to Customer %s, want the same Customer %s — the fixture proves nothing", other, customerID)
	}

	after, afterRaw := readDossier(t, env, adminSession, eventID, customerID)
	if !bytes.Equal(afterRaw, before) {
		t.Errorf("the Dossier changed after a sale at another Organization:\nbefore %s\nafter  %s", before, afterRaw)
	}
	sale := after.Sales[0]
	if sale.CustomerFirstName != "Ana" || sale.CustomerLastName != "Lopez" ||
		strOf(sale.TaxIDType) != "cedula" || strOf(sale.TaxIDNumber) != validCedula {
		t.Errorf("sale identity = %s %s %s:%s, want Ana Lopez cedula:%s",
			sale.CustomerFirstName, sale.CustomerLastName, strOf(sale.TaxIDType), strOf(sale.TaxIDNumber), validCedula)
	}
}

// Two of this Event's Sales under different names and Tax IDs each keep their
// own, newest first — and each Sale carries what the Sales list states for it.
func TestEachSaleOnTheDossierKeepsTheIdentityGivenOnIt(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Two Fest", "two-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)
	vipID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "VIP", 5000, 10)

	personal := manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-01T10:00:00Z")
	personal["customer_tax_id_type"] = "cedula"
	personal["customer_tax_id_number"] = validCedula
	first := recordManualSaleOK(t, env, adminSession, eventID, personal)

	company := manualSaleBody("ana@example.com", "Ana María", "López Vera", vipID, 1, "transfer", "2026-07-03T10:00:00Z")
	company["customer_tax_id_type"] = "ruc"
	company["customer_tax_id_number"] = naturalRUC
	second := recordManualSaleOK(t, env, adminSession, eventID, company)

	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "active")
	dossier, _ := readDossier(t, env, adminSession, eventID, customerID)

	if dossier.Customer.ID != customerID || dossier.Customer.Email != "ana@example.com" {
		t.Errorf("customer = %+v, want %s ana@example.com", dossier.Customer, customerID)
	}
	if len(dossier.Sales) != 2 || dossier.Sales[0].ConfirmationRef != second.ConfirmationRef || dossier.Sales[1].ConfirmationRef != first.ConfirmationRef {
		t.Fatalf("sales = %+v, want %s then %s (newest first)", dossier.Sales, second.ConfirmationRef, first.ConfirmationRef)
	}

	older := dossierSaleByRef(t, dossier, first.ConfirmationRef)
	if older.CustomerFirstName != "Ana" || older.CustomerLastName != "Lopez" || strOf(older.TaxIDType) != "cedula" || strOf(older.TaxIDNumber) != validCedula {
		t.Errorf("first sale identity = %s %s %s:%s", older.CustomerFirstName, older.CustomerLastName, strOf(older.TaxIDType), strOf(older.TaxIDNumber))
	}
	newer := dossierSaleByRef(t, dossier, second.ConfirmationRef)
	if newer.CustomerFirstName != "Ana María" || newer.CustomerLastName != "López Vera" || strOf(newer.TaxIDType) != "ruc" || strOf(newer.TaxIDNumber) != naturalRUC {
		t.Errorf("second sale identity = %s %s %s:%s", newer.CustomerFirstName, newer.CustomerLastName, strOf(newer.TaxIDType), strOf(newer.TaxIDNumber))
	}

	// The rest of the Sale, as the Sales list states it.
	row := saleRowByID(t, env, adminSession, eventID, first.SaleID, "active")
	if older.ID != row.ID || older.Status != "active" || older.ReversedAt != nil ||
		older.SoldAt != row.SoldAt || older.RecordedAt != row.RecordedAt ||
		older.Channel != row.Channel || strOf(older.Source) != strOf(row.Source) || older.Origin != row.Origin ||
		older.AmountCents != row.AmountCents || older.Currency != row.Currency ||
		strOf(older.PaymentMethod) != strOf(row.PaymentMethod) {
		t.Errorf("Dossier sale = %+v\nwant the Sales list's %+v", older, row)
	}
	if older.Origin != "manually_recorded" || older.AmountCents != 2000 {
		t.Errorf("origin=%s amount=%d, want manually_recorded 2000", older.Origin, older.AmountCents)
	}
	if len(older.TicketTypes) != 1 || older.TicketTypes[0].TicketTypeID != gaID || older.TicketTypes[0].TicketTypeName != "GA" || older.TicketTypes[0].Quantity != 2 {
		t.Errorf("ticket types = %+v, want GA ×2", older.TicketTypes)
	}
}

// A reversed Sale stays on the Dossier, marked reversed with its time; a
// corrected one is marked corrected and names its replacement, which is on the
// Dossier too when it went to the same Customer.
func TestReversedAndCorrectedSalesStayOnTheDossierMarked(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSession, "Undo Fest", "undo-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSession, eventID, "GA", 1000, 50)

	reversed := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 1, "cash", "2026-07-01T10:00:00Z"))
	corrected := recordManualSaleOK(t, env, adminSession, eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 2, "cash", "2026-07-02T10:00:00Z"))
	reverseImportedSaleOK(t, env, adminSession, eventID, reversed.SaleID)
	correction := correctImportedSaleOK(t, env, adminSession, eventID, corrected.SaleID,
		correctionBody("ana@example.com", "Ana", "Lopez", gaID, 3, "cash", "2026-07-02T10:00:00Z"))

	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "ana@example.com", "reversed")
	dossier, _ := readDossier(t, env, adminSession, eventID, customerID)
	if len(dossier.Sales) != 3 {
		t.Fatalf("sales = %+v, want the reversed, the corrected and its replacement", dossier.Sales)
	}

	gone := dossierSaleByRef(t, dossier, reversed.ConfirmationRef)
	listed := saleRowByID(t, env, adminSession, eventID, reversed.SaleID, "reversed")
	if gone.Status != "reversed" || gone.ReversedAt == nil || listed.ReversedAt == nil || *gone.ReversedAt != *listed.ReversedAt || gone.ReplacedByConfirmationRef != nil {
		t.Errorf("reversed sale status=%s reversed_at=%s replaced_by=%s, want reversed at %s with no replacement",
			gone.Status, strOf(gone.ReversedAt), strOf(gone.ReplacedByConfirmationRef), strOf(listed.ReversedAt))
	}

	fixed := dossierSaleByRef(t, dossier, corrected.ConfirmationRef)
	if fixed.Status != "corrected" || fixed.ReversedAt == nil || strOf(fixed.ReplacedByConfirmationRef) != correction.ReplacementConfirmationRef {
		t.Errorf("corrected sale status=%s reversed_at=%s replaced_by=%s, want corrected → %s",
			fixed.Status, strOf(fixed.ReversedAt), strOf(fixed.ReplacedByConfirmationRef), correction.ReplacementConfirmationRef)
	}

	replacement := dossierSaleByRef(t, dossier, correction.ReplacementConfirmationRef)
	if replacement.Status != "active" || replacement.Origin != "correction_replacement" {
		t.Errorf("replacement status=%s origin=%s, want active correction_replacement", replacement.Status, replacement.Origin)
	}
}

// The Dossier is about the Event, and the declarations a Customer makes to the
// platform — Terms Acceptance, Adulthood Declaration, Marketing Consent — and
// their avatar are never on it. Asserted on the raw payload, over a buyer who
// made them through a real Online checkout.
func TestTheCustomerDossierCarriesNoPlatformDeclarations(t *testing.T) {
	env := setupTest(t)
	adminSession := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSession, "Online Fest", "online-fest", 1000, 10)
	begun := beginCheckoutOK(t, env, "test-org", "online-fest",
		taxIDCheckoutBody("olga@example.com", "Olga", "Nieto", "cedula", validCedula,
			map[string]any{"ticket_type_id": gaID, "quantity": 1}))
	if confirmed := confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved"); confirmed.Status != "approved" {
		t.Fatalf("confirm status = %q, want approved", confirmed.Status)
	}
	customerID := customerIDOnSalesList(t, env, adminSession, eventID, "olga@example.com", "active")

	dossier, raw := readDossier(t, env, adminSession, eventID, customerID)
	if len(dossier.Sales) != 1 || dossier.Sales[0].Channel != "online" {
		t.Fatalf("sales = %+v, want the one Online Sale", dossier.Sales)
	}

	var customer map[string]json.RawMessage
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("decode raw dossier: %v", err)
	}
	if err := json.Unmarshal(top["customer"], &customer); err != nil {
		t.Fatalf("decode raw customer: %v", err)
	}
	if len(customer) != 2 || customer["id"] == nil || customer["email"] == nil {
		t.Errorf("customer keys = %s, want exactly id and email", top["customer"])
	}
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"terms", "adulthood", "consent", "marketing", "avatar", "picture", "photo"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("the Dossier payload mentions %q: %s", forbidden, raw)
		}
	}
}

// --- Tickets and Holders (#640) ----------------------------------------------
//
// Each Sale's Tickets and the Tickets a Customer holds on other buyers' Sales,
// every assignment field disclosed through catalog.DiscloseHolder (ADR 0047):
// an unaccepted assignment is a state and never a person.

// dossierHolders decodes the #640 part of a Dossier, beside dossierBody.
type dossierHolders struct {
	Sales []struct {
		ConfirmationRef          string          `json:"confirmation_ref"`
		Status                   string          `json:"status"`
		Tickets                  []dossierTicket `json:"tickets"`
		AssignmentReminderSentAt *[]time.Time    `json:"assignment_reminder_sent_at"`
	} `json:"sales"`
	HeldTickets *[]dossierHeldTicket `json:"held_tickets"`
}

type dossierTicket struct {
	TicketID         string `json:"ticket_id"`
	TicketTypeID     string `json:"ticket_type_id"`
	TicketTypeName   string `json:"ticket_type_name"`
	Ordinal          int    `json:"ordinal"`
	AssignmentState  string `json:"assignment_state"`
	NeverAccepted    bool   `json:"never_accepted"`
	SelfHeld         bool   `json:"self_held"`
	HolderCustomerID string `json:"holder_customer_id"`
	HolderFirstName  string `json:"holder_first_name"`
	HolderLastName   string `json:"holder_last_name"`
}

type dossierHeldTicket struct {
	TicketID        string     `json:"ticket_id"`
	TicketTypeName  string     `json:"ticket_type_name"`
	Ordinal         int        `json:"ordinal"`
	TicketSaleID    string     `json:"ticket_sale_id"`
	ConfirmationRef string     `json:"confirmation_ref"`
	SaleStatus      string     `json:"sale_status"`
	BuyerFirstName  string     `json:"buyer_first_name"`
	BuyerLastName   string     `json:"buyer_last_name"`
	AcceptedAt      *time.Time `json:"accepted_at"`
	HolderFirstName string     `json:"holder_first_name"`
	HolderLastName  string     `json:"holder_last_name"`
}

func decodeDossierHolders(t *testing.T, raw json.RawMessage) dossierHolders {
	t.Helper()
	var out dossierHolders
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode dossier tickets: %v", err)
	}
	return out
}

// dossierHolderFixture is one Event with Ticket Assignment open and one
// Manually Recorded Sale of four Tickets by Ana, whose Tickets stand in every
// state an Organization can be told about:
//
//	selfHeld  Ticket 1, Ana's own (ADR 0048)
//	accepted  Ticket 2, accepted by Carla Ruiz
//	assigned  Ticket 3, typed as diego@example.com and never accepted
//	purged    Ticket 4, named, never accepted, address purged (#334)
type dossierHolderFixture struct {
	session  string
	eventID  string
	saleID   string
	saleRef  string
	ana      string
	carla    string
	selfHeld string
	accepted string
	assigned string
	purged   string
}

func newDossierHolderFixture(t *testing.T, env *testEnv) dossierHolderFixture {
	t.Helper()
	enableTicketAssignment(t)
	f := dossierHolderFixture{session: orgAdminSession(t, env)}
	f.eventID = createDraftEvent(t, env, f.session, "Holder Fest", "holder-fest")
	scheduleEvent(t, env, f.session, f.eventID, "Holder Fest", "holder-fest", env.fixedClock.Add(30*24*time.Hour))
	gaID := createTicketTypeWithCapacity(t, env, f.session, f.eventID, "GA", 1000, 50)
	sale := recordManualSaleOK(t, env, f.session, f.eventID,
		manualSaleBody("ana@example.com", "Ana", "Lopez", gaID, 4, "cash", "2026-07-01T10:00:00Z"))
	f.saleID, f.saleRef = sale.SaleID, sale.ConfirmationRef
	tickets := ticketIDsOfSale(t, env, f.saleID)
	if len(tickets) != 4 {
		t.Fatalf("Tickets minted = %d, want 4", len(tickets))
	}
	f.selfHeld, f.accepted, f.assigned, f.purged = tickets[0], tickets[1], tickets[2], tickets[3]

	acceptHolderNamed(t, env, "ana@example.com", f.accepted, "carla@example.com", "Carla", "Ruiz")
	assignTicketOK(t, env, customerSignIn(t, env, "ana@example.com"), f.saleID, f.assigned, "diego@example.com")
	stageHolderAddressPurge(t, env, f.purged)

	f.ana = customerIDOnSalesList(t, env, f.session, f.eventID, "ana@example.com", "active")
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = 'carla@example.com'`).Scan(&f.carla); err != nil {
		t.Fatalf("read Carla's Customer id: %v", err)
	}
	return f
}

func dossierTicketByID(t *testing.T, tickets []dossierTicket, ticketID string) dossierTicket {
	t.Helper()
	for _, ticket := range tickets {
		if ticket.TicketID == ticketID {
			return ticket
		}
	}
	t.Fatalf("Ticket %s is not on the Sale: %+v", ticketID, tickets)
	return dossierTicket{}
}

// A Sale's Tickets carry their Ticket Type, ordinal and assignment state; the
// accepted Holder is named and linkable, the buyer's Self-held Ticket is marked
// theirs, and an unaccepted assignment is a state with nobody behind it —
// nowhere in the body — while a purged one reads never-accepted.
func TestTheDossierListsEachSalesTicketsThroughTheDisclosureRule(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	_, raw := readDossier(t, env, f.session, f.eventID, f.ana)
	dossier := decodeDossierHolders(t, raw)
	if len(dossier.Sales) != 1 {
		t.Fatalf("sales = %+v, want Ana's one", dossier.Sales)
	}
	tickets := dossier.Sales[0].Tickets
	if len(tickets) != 4 {
		t.Fatalf("tickets = %+v, want the Sale's four", tickets)
	}
	for i, ticket := range tickets {
		if ticket.Ordinal != i+1 || ticket.TicketTypeName != "GA" || ticket.TicketTypeID == "" {
			t.Errorf("ticket %d = %+v, want GA ordinal %d in order", i, ticket, i+1)
		}
	}

	if got := dossierTicketByID(t, tickets, f.selfHeld); got.AssignmentState != "accepted" || !got.SelfHeld ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" {
		t.Errorf("the Self-held Ticket reads %+v, want accepted and marked self_held, naming nobody else", got)
	}
	if got := dossierTicketByID(t, tickets, f.accepted); got.AssignmentState != "accepted" || got.SelfHeld ||
		got.HolderCustomerID != f.carla || got.HolderFirstName != "Carla" || got.HolderLastName != "Ruiz" {
		t.Errorf("Carla's Ticket reads %+v, want accepted by Carla Ruiz (%s)", got, f.carla)
	}
	if got := dossierTicketByID(t, tickets, f.assigned); got.AssignmentState != "assigned" || got.NeverAccepted ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" || got.HolderLastName != "" {
		t.Errorf("the unaccepted Ticket reads %+v, want `assigned` and nobody", got)
	}
	if got := dossierTicketByID(t, tickets, f.purged); got.AssignmentState != "assigned" || !got.NeverAccepted ||
		got.HolderCustomerID != "" || got.HolderFirstName != "" {
		t.Errorf("the purged Ticket reads %+v, want `assigned` never_accepted and nobody", got)
	}
	if strings.Contains(strings.ToLower(string(raw)), "diego") {
		t.Errorf("an unaccepted Holder's address reaches the Dossier (ADR 0047): %s", raw)
	}
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 0 {
		t.Errorf("Ana's held_tickets = %v, want [] — her own Ticket belongs under her Sale", dossier.HeldTickets)
	}
	if sent := dossier.Sales[0].AssignmentReminderSentAt; sent == nil || len(*sent) != 0 {
		t.Errorf("assignment_reminder_sent_at = %v, want [] for a Sale nobody was reminded about", sent)
	}
}

// A Customer who bought nothing at this Event but accepted a Ticket there has a
// Dossier: the held Ticket with its buyer's name as given on that Sale. A
// person merely typed as a Holder has none.
func TestAHolderOnlyCustomersDossierListsTheHeldTicket(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	body, raw := readDossier(t, env, f.session, f.eventID, f.carla)
	if body.Customer.ID != f.carla || body.Customer.Email != "carla@example.com" || len(body.Sales) != 0 {
		t.Fatalf("Carla's Dossier = %+v, want her identity and no Sales", body)
	}
	dossier := decodeDossierHolders(t, raw)
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 1 {
		t.Fatalf("held_tickets = %v, want Carla's one", dossier.HeldTickets)
	}
	held := (*dossier.HeldTickets)[0]
	if held.TicketID != f.accepted || held.Ordinal != 2 || held.TicketTypeName != "GA" ||
		held.TicketSaleID != f.saleID || held.ConfirmationRef != f.saleRef || held.SaleStatus != "active" ||
		held.BuyerFirstName != "Ana" || held.BuyerLastName != "Lopez" || held.AcceptedAt == nil ||
		held.HolderFirstName != "Carla" || held.HolderLastName != "Ruiz" {
		t.Errorf("held ticket = %+v, want GA ticket 2 of Ana Lopez's %s, accepted by Carla Ruiz", held, f.saleRef)
	}

	// Diego was typed and never accepted: at this Event he is nobody.
	customerSignIn(t, env, "diego@example.com")
	var diego string
	if err := env.db.QueryRow(`SELECT id FROM customers WHERE email = 'diego@example.com'`).Scan(&diego); err != nil {
		t.Fatalf("read Diego's Customer id: %v", err)
	}
	if status, code := dossierStatus(t, env, f.session, f.eventID, diego); status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
		t.Errorf("an unaccepted Holder's Dossier status=%d code=%s, want 404 CUSTOMER_NOT_FOUND", status, code)
	}
}

// A Holder whose Ticket was on a Sale later reversed is still found and told
// the Sale was reversed; the reversed Sale's own Tickets carry no live state.
func TestAHolderOnAReversedSaleIsFoundAndToldTheSaleWasReversed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)
	reverseImportedSaleOK(t, env, f.session, f.eventID, f.saleID)

	_, raw := readDossier(t, env, f.session, f.eventID, f.carla)
	dossier := decodeDossierHolders(t, raw)
	if dossier.HeldTickets == nil || len(*dossier.HeldTickets) != 1 {
		t.Fatalf("held_tickets = %v, want Carla's Ticket on the reversed Sale", dossier.HeldTickets)
	}
	if held := (*dossier.HeldTickets)[0]; held.TicketID != f.accepted || held.SaleStatus != "reversed" || held.BuyerFirstName != "Ana" {
		t.Errorf("held ticket = %+v, want Ana's Ticket marked sale_status reversed", held)
	}

	_, anaRaw := readDossier(t, env, f.session, f.eventID, f.ana)
	ana := decodeDossierHolders(t, anaRaw)
	if len(ana.Sales) != 1 || ana.Sales[0].Status != "reversed" || len(ana.Sales[0].Tickets) != 4 {
		t.Fatalf("Ana's sales = %+v, want the reversed Sale with its four Tickets", ana.Sales)
	}
	var rawSales struct {
		Sales []struct {
			Tickets []map[string]json.RawMessage `json:"tickets"`
		} `json:"sales"`
	}
	if err := json.Unmarshal(anaRaw, &rawSales); err != nil {
		t.Fatalf("decode raw sales: %v", err)
	}
	for _, ticket := range rawSales.Sales[0].Tickets {
		for key := range ticket {
			switch key {
			case "ticket_id", "ticket_type_id", "ticket_type_name", "ordinal":
			default:
				t.Errorf("a reversed Sale's Ticket carries %q: %v — its Tickets are void", key, ticket)
			}
		}
	}
}

// Each Sale says when an Assignment Reminder was sent about it.
func TestTheDossierStatesWhenAnAssignmentReminderWasSentAboutASale(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentReminderFixture(t, env)
	if result := sweepAssignmentReminders(t, env); result.Sent != 1 {
		t.Fatalf("sweep = %+v, want one reminder sent", result)
	}
	customerID := customerIDOnSalesList(t, env, f.staffSession, f.eventID, "ana@example.com", "active")

	_, raw := readDossier(t, env, f.staffSession, f.eventID, customerID)
	dossier := decodeDossierHolders(t, raw)
	if len(dossier.Sales) != 1 {
		t.Fatalf("sales = %+v, want Ana's one", dossier.Sales)
	}
	sent := dossier.Sales[0].AssignmentReminderSentAt
	if sent == nil || len(*sent) != 1 || !(*sent)[0].Equal(f.sweepAt) {
		t.Errorf("assignment_reminder_sent_at = %v, want the one send at %v", sent, f.sweepAt)
	}
}

// With TICKET_ASSIGNMENT_ENABLED closed the Dossier says nothing about
// assignment — no states, no Holders, no held Tickets, no reminders — while the
// Sales and the identity remain, and a Holder-only Customer is not found.
func TestTheDossierSaysNothingAboutAssignmentWhileTheFlagIsClosed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)
	closeTicketAssignment(t)

	body, raw := readDossier(t, env, f.session, f.eventID, f.ana)
	if body.Customer.Email != "ana@example.com" || len(body.Sales) != 1 || body.Sales[0].CustomerFirstName != "Ana" {
		t.Fatalf("Dossier = %+v, want Ana's identity and Sale", body)
	}
	if tickets := decodeDossierHolders(t, raw).Sales[0].Tickets; len(tickets) != 4 {
		t.Errorf("tickets = %+v, want the Sale's four, types and ordinals only", tickets)
	}
	for _, forbidden := range []string{"assignment_state", "never_accepted", "self_held", "holder_", "held_tickets", "assignment_reminder", "Carla"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("the Dossier carries %q while TICKET_ASSIGNMENT_ENABLED is closed (ADR 0045): %s", forbidden, raw)
		}
	}
	if status, code := dossierStatus(t, env, f.session, f.eventID, f.carla); status != http.StatusNotFound || code != "CUSTOMER_NOT_FOUND" {
		t.Errorf("a Holder-only Customer's Dossier status=%d code=%s, want 404 while assignment is dark", status, code)
	}
}

// The Holder List carries the ids its Dossier links need: every row its buyer's
// Customer id, and an accepted row its Holder's — never an unaccepted one's.
func TestTheHolderListCarriesTheCustomerIdsItsDossierLinksNeed(t *testing.T) {
	env := setupTest(t)
	f := newDossierHolderFixture(t, env)

	resp, body := env.get(t, holderListPath(f.eventID), authHeader(f.session))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder list status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode holder list: %v", err)
	}
	if len(page.Data) != 4 {
		t.Fatalf("rows = %d, want Ana's four Tickets", len(page.Data))
	}
	for _, row := range page.Data {
		var ticketID, buyer, holder string
		_ = json.Unmarshal(row["ticket_id"], &ticketID)
		_ = json.Unmarshal(row["customer_id"], &buyer)
		rawHolder, present := row["holder_customer_id"]
		_ = json.Unmarshal(rawHolder, &holder)
		if buyer != f.ana {
			t.Errorf("row %s customer_id = %q, want Ana's %s", ticketID, buyer, f.ana)
		}
		switch ticketID {
		case f.accepted:
			if holder != f.carla {
				t.Errorf("Carla's row holder_customer_id = %q, want %s", holder, f.carla)
			}
		case f.selfHeld:
			if holder != f.ana {
				t.Errorf("the Self-held row holder_customer_id = %q, want Ana's own %s", holder, f.ana)
			}
		default:
			if present {
				t.Errorf("unaccepted row %s carries holder_customer_id %s (ADR 0047)", ticketID, rawHolder)
			}
		}
	}

	// And both links open.
	readDossier(t, env, f.session, f.eventID, f.carla)
	readDossier(t, env, f.session, f.eventID, f.ana)
}
