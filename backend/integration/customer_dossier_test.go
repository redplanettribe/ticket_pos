package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
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
