package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Assignment mail arrives and accepting mints a Customer (#325, parent #322,
// ADR 0046).
//
// WHAT THIS SLICE IS. #324 let a buyer name an address for a Ticket and stopped
// there: no mail, no link, and `accepted` modelled but unreachable. This is what
// makes it reachable. The platform writes to the address, the person clicks, and
// that click — Proof of Email Ownership by the standard ADR 0035 set — turns a
// row into a Verified Customer who gives their own name and answers their own
// Ticket Questions.
//
// THE PROPERTY EVERYTHING ELSE RESTS ON is that the Assignment Link is a token
// DISTINCT from the Answer Link, delivered only to the address. The Answer Link
// is copyable off the buyer's own sale page; if this flow accepted one, the
// buyer could accept on their friend's behalf and the Verified Customer minted
// from it would be a fiction. ADR 0046 rates a leak of this token onto a buyer
// surface as a defect of the same severity as leaking the signing key. That is
// what TestTheAssignmentLinkNeverReachesTheBuyer exists for, and it is the test
// to keep passing.
//
// THE TESTS RUN AT THE HTTP SEAM, per docs/testing.md, and reach for the
// captured mail sender the way a Holder reaches for their inbox: it is the ONLY
// place a token can be obtained, because no API response contains one.

const (
	assignmentLinkPath         = "/api/v1/public/assignment-link"
	assignmentLinkNamePath     = "/api/v1/public/assignment-link/name"
	assignmentLinkQuestionPath = "/api/v1/public/assignment-link/questions/"
)

// assignmentLinkView decodes what accepting an Assignment Link returns.
//
// A NARROW STRUCT, AND THAT IS NOT ENOUGH ON ITS OWN — decoding would silently
// drop a fifth field somebody had started sending, which is exactly the
// regression the disclosure test must catch, so that one asserts on the RAW
// BODY and never on this.
type assignmentLinkView struct {
	EventName       string `json:"event_name"`
	TicketTypeName  string `json:"ticket_type_name"`
	HolderFirstName string `json:"holder_first_name"`
	HolderLastName  string `json:"holder_last_name"`
	Questions       []struct {
		Question ticketQuestion `json:"question"`
		Answer   *holderAnswer  `json:"answer"`
	} `json:"questions"`
}

// holderAnswer is one Answer as the Holder's own page renders it.
//
// IT CARRIES THE SAME SHAPE EVENT STAFF SEE and deliberately not a narrower one:
// the Options a choice Answer recorded, the words each Option SHOWED AT THE TIME
// beside the words it shows now, and when the Answer last changed. Four surfaces
// answer one question through one write (service.answerTicketQuestion), and a
// decoder here that could not see those fields would let the Holder's route
// quietly stop recording them.
//
// THERE IS NO AUTHOR FIELD AND THERE MUST NEVER BE ONE. The platform records
// what the current Answer is and when it changed — never who changed it (ADR
// 0046 rejected the history table for the same reason migration 073 has no
// author column).
type holderAnswer struct {
	Text    *string `json:"text"`
	Number  *string `json:"number"`
	Date    *string `json:"date"`
	Checked *bool   `json:"checked"`
	Options []struct {
		OptionID     string `json:"option_id"`
		Label        string `json:"label"`
		CurrentLabel string `json:"current_label"`
		Retired      bool   `json:"retired"`
	} `json:"options"`
	UpdatedAt time.Time `json:"updated_at"`
}

// assignmentMailFor finds the one Assignment mail sent to an address, failing if
// there is not exactly one.
//
// THE CAPTURED MAIL IS THE ONLY SOURCE OF A TOKEN IN THIS ENTIRE PACKAGE. There
// is no minting helper here, unlike answerLinkToken beside it, and its absence
// is deliberate: a test that could mint its own Assignment Link would be a test
// standing where no buyer, no stranger and no API caller can stand.
func assignmentMailFor(t *testing.T, env *testEnv, address string) platform.TicketAssignment {
	t.Helper()
	var found []platform.TicketAssignment
	for _, mail := range env.email.TicketAssignmentsSent() {
		if mail.To == address {
			found = append(found, mail)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Assignment mails to %s = %d, want exactly 1", address, len(found))
	}
	return found[0]
}

// assignmentTokenFrom pulls the token out of the link a mail carries, the way a
// Holder's browser does when they press it.
func assignmentTokenFrom(t *testing.T, mail platform.TicketAssignment) string {
	t.Helper()
	base, token, found := strings.Cut(mail.AcceptURL, "?token=")
	if !found || token == "" {
		t.Fatalf("the Assignment mail carries no token: %q", mail.AcceptURL)
	}
	// The link lands on the Storefront and never on this API (ADR 0008), at an
	// address with no Locale in it — the middleware puts the reader into their
	// own language, which for this reader is not necessarily the buyer's.
	if !strings.HasSuffix(base, "/accept") {
		t.Fatalf("the Assignment mail points at %q, want the Storefront's /accept page", base)
	}
	return token
}

// acceptAssignment presses the link: the request a Holder's browser makes, with
// no session, no cookie and no header of any kind.
func acceptAssignment(t *testing.T, env *testEnv, token string) (*http.Response, envelope, []byte) {
	t.Helper()
	return answerLinkRequest(t, env, http.MethodPost, assignmentLinkPath, map[string]any{"token": token})
}

// acceptAssignmentOK presses the link and insists it worked.
func acceptAssignmentOK(t *testing.T, env *testEnv, token string) assignmentLinkView {
	t.Helper()
	resp, body, _ := acceptAssignment(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accept status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return decodeAssignmentLinkView(t, body.Data)
}

// answerByAssignmentLink is the Holder writing one of their own Answers: a PUT
// naming the question in the path and carrying the token in the body, with no
// session, no cookie and no Ticket id anywhere.
func answerByAssignmentLink(
	t *testing.T, env *testEnv, token, questionID string, body map[string]any,
) (*http.Response, envelope, []byte) {
	t.Helper()
	withToken := map[string]any{"token": token}
	for key, value := range body {
		withToken[key] = value
	}
	return answerLinkRequest(t, env, http.MethodPut, assignmentLinkQuestionPath+questionID, withToken)
}

// answerByAssignmentLinkOK writes the Answer and insists it worked.
func answerByAssignmentLinkOK(
	t *testing.T, env *testEnv, token, questionID string, body map[string]any,
) assignmentLinkView {
	t.Helper()
	resp, envelope, _ := answerByAssignmentLink(t, env, token, questionID, body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder answer status=%d error=%+v", resp.StatusCode, envelope.Error)
	}
	return decodeAssignmentLinkView(t, envelope.Data)
}

// holderAnswerFor picks one question's Answer off the Holder's page.
func holderAnswerFor(t *testing.T, view assignmentLinkView, questionID string) *holderAnswer {
	t.Helper()
	for _, pair := range view.Questions {
		if pair.Question.ID == questionID {
			return pair.Answer
		}
	}
	t.Fatalf("question %s is not on the Holder's page", questionID)
	return nil
}

func decodeAssignmentLinkView(t *testing.T, data json.RawMessage) assignmentLinkView {
	t.Helper()
	var view assignmentLinkView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatalf("decode assignment link view: %v", err)
	}
	return view
}

// customerRow is what the platform now holds about a person, read straight from
// the table because no API hands a test somebody else's Customer record.
type holderCustomerRow struct {
	id         string
	firstName  string
	lastName   string
	verifiedAt sql.NullTime
	mailLocale string
}

func readHolderCustomer(t *testing.T, env *testEnv, email string) (holderCustomerRow, bool) {
	t.Helper()
	var row holderCustomerRow
	err := env.db.QueryRow(`
		SELECT id, first_name, last_name, verified_at, mail_locale FROM customers WHERE email = $1
	`, email).Scan(&row.id, &row.firstName, &row.lastName, &row.verifiedAt, &row.mailLocale)
	if err == sql.ErrNoRows {
		return holderCustomerRow{}, false
	}
	if err != nil {
		t.Fatalf("read Customer %s: %v", email, err)
	}
	return row, true
}

// assignmentFixture is #324's buyer fixture with assignment opened and Ana
// signed in — every test here starts from a buyer about to name an address.
type assignmentFixture struct {
	buyerAnswersFixture
	ana string
}

func newAssignmentFixture(t *testing.T, env *testEnv) assignmentFixture {
	t.Helper()
	f := newBuyerAnswersFixture(t, env)
	enableTicketAssignment(t)
	return assignmentFixture{buyerAnswersFixture: f, ana: customerSignIn(t, env, "ana@example.com")}
}

// THE WHOLE WALK, IN ONE TEST: the buyer names an address, the platform writes to
// it, the person at that address clicks, and a row becomes a Verified Customer.
//
// This is the acceptance criterion #325 exists for, and every clause of it is
// asserted through a request or through the captured inbox — never through the
// service that produced it.
func TestAssigningMailsTheHolderAndTheClickAcceptsAndMintsAVerifiedCustomer(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// Carla is nobody: no Customer record, no sale, no sign-in. That is the
	// ordinary case, and the whole reason this feature is delicate.
	if _, exists := readHolderCustomer(t, env, "carla@example.com"); exists {
		t.Fatal("the fixture already knows carla@example.com; this test is about a stranger")
	}

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")

	// ONE MAIL, TO THAT ADDRESS, NAMING THE EVENT AND THE TICKET TYPE.
	mail := assignmentMailFor(t, env, "carla@example.com")
	if mail.EventName != "Buyer Fest" || mail.TicketTypeName != "GA" {
		t.Errorf("mail names event=%q ticket type=%q, want Buyer Fest / GA", mail.EventName, mail.TicketTypeName)
	}
	// AND IT NAMES NO BUYER, NO PRICE AND NO REFERENCE — asserted on the words
	// the recipient actually reads, because that is where a widening would land.
	rendered := mail.Subject() + "\n" + mail.Text()
	for _, forbidden := range []string{"Ana", "Lopez", "ana@example.com", f.anaRef} {
		if strings.Contains(rendered, forbidden) {
			t.Errorf("the Assignment mail contains %q.\n"+
				"It names the Event and the Ticket Type and nothing else about the purchase (ADR 0046).", forbidden)
		}
	}

	// THE CLICK. No session, no cookie, no passcode — the token is the whole
	// authority, and pressing a link that only ever travelled to this address is
	// Proof of Email Ownership (ADR 0035).
	view := acceptAssignmentOK(t, env, assignmentTokenFrom(t, mail))
	if view.EventName != "Buyer Fest" || view.TicketTypeName != "GA" {
		t.Errorf("the page shows event=%q ticket type=%q", view.EventName, view.TicketTypeName)
	}
	// Nobody has ever named this person, so the form starts empty rather than
	// with somebody else's guess: the buyer was never asked for a holder name.
	if view.HolderFirstName != "" || view.HolderLastName != "" {
		t.Errorf("a stranger's page is prefilled with %q %q", view.HolderFirstName, view.HolderLastName)
	}
	// The Ticket Questions are hers to answer, which is the point of accepting.
	if len(view.Questions) != 2 {
		t.Fatalf("the page shows %d questions, want the Ticket Type's 2", len(view.Questions))
	}

	// A VERIFIED CUSTOMER NOW EXISTS FOR THAT ADDRESS, minted by a click and by
	// no passcode, no password and no sign-in.
	customer, exists := readHolderCustomer(t, env, "carla@example.com")
	if !exists {
		t.Fatal("accepting created no Customer; the click is what mints one (ADR 0046)")
	}
	if !customer.verifiedAt.Valid {
		t.Error("the Customer minted by accepting is not Verified; the click IS the proof of ownership (ADR 0035)")
	}

	// AND THE TICKET IS `accepted` — the state #324 modelled and could not reach.
	row := readTicketAssignment(t, env, ticketID)
	if !row.acceptedAt.Valid || row.holderCustomerID.String != customer.id {
		t.Fatalf("Ticket row accepted_at=%v holder_customer_id=%v, want the acceptance and Carla's Customer id",
			row.acceptedAt, row.holderCustomerID)
	}

	// The buyer sees it too: their four otherwise identical Tickets now say which
	// one has actually been picked up.
	buyerRow := findBuyerRow(t, listBuyerTickets(t, env, f.ana, f.anaSaleID), ticketID)
	if buyerRow.AssignmentState != "accepted" || buyerRow.AcceptedAt == nil {
		t.Errorf("the buyer's row reads state=%q accepted_at=%v", buyerRow.AssignmentState, buyerRow.AcceptedAt)
	}
}

// THE ASSIGNMENT LINK NEVER REACHES THE BUYER, AND THIS IS THE TEST THIS TICKET
// EXISTS TO KEEP PASSING.
//
// ADR 0046: "The Answer Link is copyable off the buyer's own sale page, so
// reusing it here would mean the click proves nothing and the Verified Customer
// minted from it is a fiction. An Assignment Link appearing on a buyer surface
// or in a response to the buyer is therefore a defect of the same severity as
// leaking the token itself."
//
// So this asserts on the RAW BYTES of every response a buyer can obtain — the
// read, the write, and the Answer Link page they can reach with a link they DO
// hold — and searches them for the token that went to the inbox. A decoded
// struct would miss a field somebody added, which is precisely the change that
// must fail here.
//
// It also asserts the other half of the property: the token the buyer CAN copy —
// the Answer Link — does not accept. The two are separately keyed, so this fails
// cryptographically rather than by a check somebody has to remember.
func TestTheAssignmentLinkNeverReachesTheBuyer(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	resp, body := assignTicket(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign status=%d error=%+v", resp.StatusCode, body.Error)
	}
	writeResponse := string(body.Data)

	mail := assignmentMailFor(t, env, "carla@example.com")
	token := assignmentTokenFrom(t, mail)

	readResp, readBody := env.get(t, buyerTicketsPath(f.anaSaleID), authHeader(f.ana))
	if readResp.StatusCode != http.StatusOK {
		t.Fatalf("buyer tickets status=%d error=%+v", readResp.StatusCode, readBody.Error)
	}

	// The buyer's own Answer Link page: the surface they hold a working token
	// for, and the nearest thing to a place this token could leak into.
	_, _, answerLinkRaw := openAnswerLink(t, env, answerLinkToken(t, ticketID))

	for name, payload := range map[string]string{
		"the buyer's ticket list":        string(readBody.Data),
		"the response to the assignment": writeResponse,
		"the Answer Link page":           string(answerLinkRaw),
	} {
		if strings.Contains(payload, token) {
			t.Errorf("%s contains the Assignment Link token.\n"+
				"ADR 0046 rates this as the same severity as leaking the signing key: the token is\n"+
				"delivered ONLY to the address, and a buyer who can read it can accept on their\n"+
				"friend's behalf, making the Verified Customer minted from it a fiction.", name)
		}
		// The address of the accept page is as good as the token to somebody
		// building a link, so neither half may appear.
		if strings.Contains(payload, "/accept?token=") || strings.Contains(payload, "assignment-link") {
			t.Errorf("%s carries the accept URL", name)
		}
	}

	// THE OTHER HALF: the token the buyer CAN copy does not accept. A build that
	// signed both links with one key, or that let one route verify the other's
	// payload, fails here.
	resp, body, _ = acceptAssignment(t, env, answerLinkToken(t, ticketID))
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")

	// And the reverse, so neither door opens the other: an Assignment Link is not
	// an Answer Link either.
	answerResp, answerBody, _ := openAnswerLink(t, env, token)
	assertAPIError(t, answerResp, answerBody, http.StatusUnauthorized, "ANSWER_LINK_INVALID")
}

// ACCEPTING TWICE IS IDEMPOTENT, which is an acceptance criterion and is also
// simply what a link in an inbox demands: people click twice, and mail clients
// prefetch. The second click lands on the Holder's own page, keeps the first
// acceptance's instant, and mints no second Customer.
func TestAcceptingTwiceIsIdempotent(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	acceptAssignmentOK(t, env, token)
	first := readTicketAssignment(t, env, ticketID)

	// The clock moves, so that a rewritten accepted_at would be a DIFFERENT
	// value. Under the fixed clock alone, "kept" and "rewritten to the same
	// instant" are indistinguishable.
	later := env.fixedClock.Add(72 * time.Hour)
	sharedApp.CatalogService.WithClock(func() time.Time { return later })
	defer sharedApp.CatalogService.WithClock(func() time.Time { return env.fixedClock })

	view := acceptAssignmentOK(t, env, token)
	if view.EventName != "Buyer Fest" {
		t.Fatalf("the second click landed somewhere else: %+v", view)
	}

	second := readTicketAssignment(t, env, ticketID)
	if !second.acceptedAt.Time.Equal(first.acceptedAt.Time) {
		t.Errorf("accepted_at moved from %v to %v on a second click.\n"+
			"The moment a row became a person is a fact worth not overwriting.",
			first.acceptedAt.Time, second.acceptedAt.Time)
	}
	if second.holderCustomerID.String != first.holderCustomerID.String {
		t.Error("the second click attached a different Customer to the Ticket")
	}
}

// A KNOWN CUSTOMER'S NAME IS PREFILLED — AND ONLY AFTER THE CLICK.
//
// The second half is the security property. A page reachable WITHOUT the click
// that showed a known Customer's name would be an oracle for whether an address
// is registered, which ADR 0035 is explicit about avoiding: anybody could type
// an address and learn whether this platform holds a person for it. So the
// prefill exists on exactly one route, and that route accepts.
//
// The Holder may correct it, because a name captured at some door sale years ago
// is theirs to fix.
func TestAKnownCustomersNameIsPrefilledOnlyAfterTheClick(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// Bruno already exists, with a name, because he bought a ticket of his own.
	if bruno, exists := readHolderCustomer(t, env, "bruno@example.com"); !exists || bruno.firstName != "Bruno" {
		t.Fatalf("the fixture's Bruno is %+v, want a Customer already named", bruno)
	}

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "bruno@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "bruno@example.com"))

	// BEFORE THE CLICK there is no route that reports anything about the address.
	// The buyer's own list is the only surface that names it at all, and it names
	// only what the buyer typed — never whether the platform knows the person.
	buyerRow := findBuyerRow(t, listBuyerTickets(t, env, f.ana, f.anaSaleID), ticketID)
	if buyerRow.HolderEmail != "bruno@example.com" {
		t.Fatalf("the buyer's row holder_email = %q", buyerRow.HolderEmail)
	}
	buyerRaw, _ := json.Marshal(buyerRow)
	for _, forbidden := range []string{"Bruno", "Diaz", "first_name", "holder_first_name"} {
		if strings.Contains(string(buyerRaw), forbidden) {
			t.Errorf("the buyer's row leaks %q about the person they named.\n"+
				"Whether an address is registered is not a question any pre-click surface may answer (ADR 0035).",
				forbidden)
		}
	}

	// AFTER THE CLICK the name is his own, shown back to him.
	view := acceptAssignmentOK(t, env, token)
	if view.HolderFirstName != "Bruno" || view.HolderLastName != "Diaz" {
		t.Errorf("prefill = %q %q, want Bruno Diaz — a Holder should not retype what the platform already knows",
			view.HolderFirstName, view.HolderLastName)
	}

	// And he may correct it, which writes to his Customer record as his current
	// asserted name.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "  Bruno Carlos ", "last_name": "Díaz",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("name status=%d error=%+v", resp.StatusCode, body.Error)
	}
	named := decodeAssignmentLinkView(t, body.Data)
	if named.HolderFirstName != "Bruno Carlos" || named.HolderLastName != "Díaz" {
		t.Errorf("after naming, the page reads %q %q", named.HolderFirstName, named.HolderLastName)
	}
	stored, _ := readHolderCustomer(t, env, "bruno@example.com")
	if stored.firstName != "Bruno Carlos" || stored.lastName != "Díaz" {
		t.Errorf("the Customer record reads %q %q; the Holder's own word is what the Organization sees",
			stored.firstName, stored.lastName)
	}

	// HALF A NAME IS NOT A NAME. Both halves are stored separately (ADR 0005) and
	// both are required, because half a name is half a person on a guest list.
	// Refused by the HANDLER as the standard validation envelope (#336): the
	// token names the Ticket, so there is no id here for a 400 to leak.
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "  ", "last_name": "Díaz",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank first name status=%d, want 400 (error=%+v)", resp.StatusCode, body.Error)
	}
	fields := fieldErrorsByName(t, body)
	if got, ok := fields["first_name"]; !ok || got.Code != "REQUIRED" {
		t.Errorf("blank first name field error = %+v, want first_name REQUIRED", fields)
	}
	if _, ok := fields["last_name"]; ok {
		t.Errorf("last_name was given and should not be refused; got %+v", fields)
	}

	// AND A HALF THAT OVERFLOWS ITS COLUMN IS NOT A NAME EITHER — the same 100
	// the Customer's own name fields are held to at checkout.
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": strings.Repeat("z", 101), "last_name": "Díaz",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("overlong first name status=%d, want 400 (error=%+v)", resp.StatusCode, body.Error)
	}
	fields = fieldErrorsByName(t, body)
	if got, ok := fields["first_name"]; !ok || got.Code != "TOO_LONG" {
		t.Errorf("overlong first name field error = %+v, want first_name TOO_LONG", fields)
	}
}

// A HOLDER IS NEVER ASKED FOR A TAX ID, and cannot be given one through this
// door even by a caller who tries.
//
// It is a fact about the SALE'S BUYER — a national identity number in this
// market — and demanding it of somebody accepting a gift would turn a favour
// into a registration. The page never asks; this proves the API never takes it.
func TestAHolderIsNeverAskedForATaxID(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	_, body, raw := acceptAssignment(t, env, token)
	if body.Error != nil {
		t.Fatalf("accept failed: %+v", body.Error)
	}
	// The page has no field to draw one in.
	for _, forbidden := range []string{"tax_id", "cedula", "ruc", "phone"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Errorf("the Holder's page carries %q; a Holder is asked for a name and their questions and nothing else", forbidden)
		}
	}

	// And a request that smuggles them in changes nothing about the record.
	resp, _, _ := answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "Carla", "last_name": "Ruiz",
		"tax_id_type": "cedula", "tax_id_number": "0912345678", "phone": "+593987654321",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("name status=%d", resp.StatusCode)
	}
	var taxIDType, taxIDNumber, phone sql.NullString
	if err := env.db.QueryRow(`
		SELECT tax_id_type, tax_id_number, phone FROM customers WHERE email = 'carla@example.com'
	`).Scan(&taxIDType, &taxIDNumber, &phone); err != nil {
		t.Fatalf("read Carla: %v", err)
	}
	if taxIDType.Valid || taxIDNumber.Valid || phone.Valid {
		t.Errorf("accepting wrote tax_id=%v/%v phone=%v onto the Holder's record.\n"+
			"repository.UpdateHolderName writes the name and nothing else, precisely so this cannot happen.",
			taxIDType, taxIDNumber, phone)
	}
}

// ACCEPTING GRANTS NO MARKETING CONSENT, AND NO CONSENT OF ANY KIND.
//
// A Customer minted this way has agreed to nothing beyond holding a ticket
// (ADR 0046). "They clicked, so they must want our newsletter" is consent
// manufactured from an act that was about a t-shirt size, and it is exactly what
// the platform's whole consent apparatus exists to refuse.
func TestAcceptingGrantsNoConsent(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	customer, exists := readHolderCustomer(t, env, "carla@example.com")
	if !exists {
		t.Fatal("no Customer was minted")
	}

	var consents int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM consent_records WHERE customer_id = $1
	`, customer.id).Scan(&consents); err != nil {
		t.Fatalf("count consent records: %v", err)
	}
	if consents != 0 {
		t.Errorf("accepting wrote %d consent record(s) for somebody who was never asked", consents)
	}

	var digestEnabled bool
	if err := env.db.QueryRow(`
		SELECT digest_enabled FROM customers WHERE id = $1
	`, customer.id).Scan(&digestEnabled); err != nil {
		t.Fatalf("read digest flag: %v", err)
	}
	if digestEnabled {
		t.Error("accepting a ticket subscribed the Holder to the Follow Digest")
	}
}

// THE HOLDER ANSWERS FOR THEMSELVES, AND THE ANSWER LINK STOPS OPENING.
//
// This is what accepting buys, and it is the one thing accepting takes away.
// Both doors stand open while a Ticket is merely `assigned`, so an ignored mail
// degrades to exactly ADR 0044's behaviour and a mistyped address bricks
// nothing. The moment somebody PROVES the address, the unauthenticated door is
// retired in their favour: a link still sitting in a group chat cannot overwrite
// what the Holder said about their own body.
//
// The buyer and Event Staff keep their routes throughout — ADR 0044's
// three-party rule — so a wrong Answer stays fixable.
func TestAcceptingClosesTheAnswerLinkAndTheHolderAnswersForThemselves(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// DOOR ONE, ON AN `unassigned` TICKET. Most Tickets will be here for a long
	// time, and ADR 0046 keeps the Answer Link precisely so they are answerable:
	// "It remains the route for every `unassigned` Ticket."
	forwarded := answerLinkToken(t, ticketID)
	if resp, body, _ := openAnswerLink(t, env, forwarded); resp.StatusCode != http.StatusOK {
		t.Fatalf("the Answer Link does not open on an unassigned Ticket: status=%d error=%+v",
			resp.StatusCode, body.Error)
	}

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")

	// WHILE MERELY `assigned`, THE OLD DOOR IS STILL OPEN. Nothing is bricked by
	// a mail nobody clicked.
	if resp, body, _ := openAnswerLink(t, env, forwarded); resp.StatusCode != http.StatusOK {
		t.Fatalf("the Answer Link stopped opening on a merely assigned Ticket: status=%d error=%+v",
			resp.StatusCode, body.Error)
	}

	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, token)

	// The Holder answers her own question, through her own link.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut,
		assignmentLinkQuestionPath+f.sizeQuestion.ID, map[string]any{"token": token, "text": "S"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	view := decodeAssignmentLinkView(t, body.Data)
	var answered bool
	for _, pair := range view.Questions {
		if pair.Question.ID == f.sizeQuestion.ID && pair.Answer != nil && pair.Answer.Text != nil && *pair.Answer.Text == "S" {
			answered = true
		}
	}
	if !answered {
		t.Fatalf("the Holder's own answer is not on her page: %+v", view.Questions)
	}

	// AND THE FORWARDED LINK IS DEAD — both to read and to write, identically,
	// because a write that refused differently would confirm the Ticket exists
	// to somebody the read had just told nothing.
	resp, body, _ = openAnswerLink(t, env, forwarded)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ANSWER_LINK_INVALID")
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut,
		answerLinkQuestionPath+f.sizeQuestion.ID, map[string]any{"token": forwarded, "text": "XXL"})
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ANSWER_LINK_INVALID")

	// AND THE BUYER IS NO LONGER HANDED THAT DEAD LINK. A copy button that
	// forwards a link opening nothing is worse than no button: the buyer pastes
	// it into a group chat and believes the job done. What they are NOT given
	// instead is the Assignment Link — that token reaches the address and nobody
	// else, which is the property the whole feature rests on.
	afterAccept := findBuyerRow(t, listBuyerTickets(t, env, f.ana, f.anaSaleID), ticketID)
	if afterAccept.AnswerLink != "" {
		t.Errorf("the buyer is still offered an Answer Link (%q) for a Ticket whose Holder accepted; it opens nothing",
			afterAccept.AnswerLink)
	}

	// THE BUYER KEEPS THEIR ROUTE. ADR 0044's three-party rule holds: an Answer
	// stays fixable by the buyer and by Event Staff even after a Holder accepts.
	fixResp, fixBody := env.put(t, buyerAnswerPath(f.anaSaleID, ticketID, f.sizeQuestion.ID),
		map[string]any{"text": "M"}, authHeader(f.ana))
	if fixResp.StatusCode != http.StatusOK {
		t.Fatalf("the buyer lost their own route once a Holder accepted: status=%d error=%+v",
			fixResp.StatusCode, fixBody.Error)
	}
}

// THE LINK STOPS OPENING WHEN THE TICKET IS REASSIGNED, and the reader is never
// told why.
//
// "Your friend gave your ticket to somebody else" is a fact about the buyer's
// decisions, and CONTEXT.md says the disclosure rule holds even in the error
// state — so a reassigned link answers exactly as a forged one does.
//
// The enforcement is in the signature, not in a check: the token is signed over
// the moment the address was named, and reassignment moves it.
func TestTheAssignmentLinkDiesWhenTheTicketIsReassigned(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	carlasToken := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	// Carla drops out; the ticket goes to Elena, who is mailed her own link.
	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "elena@example.com")

	resp, body, _ := acceptAssignment(t, env, carlasToken)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")
	// The refusal says nothing about a buyer, an event or a person.
	if strings.Contains(string(body.Error.Message), "Ana") {
		t.Error("the refusal names the buyer")
	}

	elena := assignmentMailFor(t, env, "elena@example.com")
	view := acceptAssignmentOK(t, env, assignmentTokenFrom(t, elena))
	if view.EventName != "Buyer Fest" {
		t.Fatalf("Elena's own link did not open: %+v", view)
	}

	// And the Ticket belongs to Elena, not to Carla — who was never accepted and
	// whose Customer record was never minted.
	if _, exists := readHolderCustomer(t, env, "carla@example.com"); exists {
		t.Error("a Customer was minted for an address that never accepted")
	}
	elenaCustomer, _ := readHolderCustomer(t, env, "elena@example.com")
	row := readTicketAssignment(t, env, ticketID)
	if row.holderCustomerID.String != elenaCustomer.id {
		t.Errorf("the Ticket names Customer %v, want Elena's %s", row.holderCustomerID, elenaCustomer.id)
	}
}

// THE LINK STOPS OPENING ON A REVERSED SALE, AND EXPIRES WHEN THE EVENT STARTS.
//
// The two refusals are told apart, and only these two are. A reversed Sale is
// ASSIGNMENT_LINK_INVALID — indistinguishable from a forgery, because "your
// friend cancelled the purchase" is a fact about somebody else's money. The
// Event having started is told apart, because an Event's start is already
// published on the Storefront, so saying so explains a deadline rather than
// implying a forgery.
func TestTheAssignmentLinkDiesOnAReversedSaleAndExpiresAtTheDoors(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	carla := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	diego := assignmentTokenFrom(t, assignmentMailFor(t, env, "diego@example.com"))

	// The doors open. The window closes for everybody, and this is also when
	// #322's purge takes an unaccepted address — so a link that opened past it
	// would be minting a Customer from a fact the platform is about to forget.
	setEventStart(t, env, f.eventID, env.fixedClock.Add(-time.Hour))
	resp, body, _ := acceptAssignment(t, env, carla)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_EXPIRED")

	// Put the Event back in the future, and reverse the Sale instead.
	setEventStart(t, env, f.eventID, env.fixedClock.Add(30*24*time.Hour))
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.anaSaleID,
	); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}
	resp, body, _ = acceptAssignment(t, env, diego)
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")
	if strings.Contains(strings.ToLower(body.Error.Message), "revers") {
		t.Error("the refusal tells the reader the Sale was reversed; that is a fact about somebody else's money")
	}
}

// THE EVENT APPEARS IN THE HOLDER'S CUSTOMER AREA, which is the way back that
// does not depend on keeping the mail.
//
// And it appears WITHOUT the purchase: no price, no Sale Confirmation reference,
// no Tax ID and no Undo. A Holder holds the ticket and nothing else — the money,
// the Sale and the Reversal Window all stayed with the buyer.
func TestTheEventAppearsInTheHoldersCustomerArea(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAssignmentOK(t, env, assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com")))

	// Carla signs in for the first time — the Customer accepting minted is the
	// Customer a passcode reaches, because both go through one write.
	carla := customerSignIn(t, env, "carla@example.com")
	resp, body := env.get(t, "/api/v1/customer/ticket-sales", authHeader(carla))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("customer area status=%d error=%+v", resp.StatusCode, body.Error)
	}

	var area struct {
		Upcoming []json.RawMessage `json:"upcoming"`
		Past     []json.RawMessage `json:"past"`
		Holding  []struct {
			TicketID       string `json:"ticket_id"`
			TicketTypeName string `json:"ticket_type_name"`
			Event          struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"event"`
			Organization struct {
				Name string `json:"name"`
			} `json:"organization"`
		} `json:"holding"`
	}
	if err := json.Unmarshal(body.Data, &area); err != nil {
		t.Fatalf("decode customer area: %v", err)
	}

	// She bought nothing, so she owns no Ticket Sale — and she is holding one.
	if len(area.Upcoming) != 0 || len(area.Past) != 0 {
		t.Errorf("the Holder's Area lists %d/%d Ticket Sales; the Sale stayed with the buyer",
			len(area.Upcoming), len(area.Past))
	}
	if len(area.Holding) != 1 {
		t.Fatalf("the Holder's Area holds %d Tickets, want the one she accepted", len(area.Holding))
	}
	if area.Holding[0].Event.Name != "Buyer Fest" || area.Holding[0].TicketTypeName != "GA" {
		t.Errorf("the held row reads %+v", area.Holding[0])
	}

	// AND NOTHING ABOUT THE PURCHASE CAME WITH IT.
	raw := string(body.Data)
	for _, forbidden := range []string{f.anaRef, "amount_cents", "confirmation_ref", "tax_id", "reversible"} {
		if strings.Contains(raw, forbidden) {
			t.Errorf("the Holder's Area carries %q.\n"+
				"A Holder is not the party of record: the money, the Sale Confirmation and the\n"+
				"Reversal Window all stayed with the buyer (CONTEXT.md).", forbidden)
		}
	}

	// The buyer's own Area is unaffected — they still own the Sale they paid for.
	resp, body = env.get(t, "/api/v1/customer/ticket-sales", authHeader(f.ana))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body.Data), f.anaRef) {
		t.Fatal("the buyer lost their own sale when a Holder accepted it")
	}
}

// THE MAIL IS WRITTEN IN THE RECIPIENT'S MAIL LOCALE, which is #325's own
// acceptance criterion and the reason ADR 0033 exists.
//
// IT IS THE ONE MAIL WHOSE CHAIN IS READ IN THE OTHER ORDER, and this test is
// where that is stated. Every other message about a sale takes the Sale Locale
// first, because it was collected from the person the mail is addressed to at
// the moment they acted. THIS reader did not buy anything: a Spanish-speaking
// Holder whose friend paid on the English site is exactly the case the feature
// exists to serve, so their own remembered Mail Locale outranks the buyer's.
func TestTheAssignmentMailIsWrittenInTheHoldersOwnLanguage(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	// Diego has read this platform in Spanish; his record remembers it.
	customerSignInWithLocale(t, env, "diego@example.com", "es")

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "diego@example.com")
	mail := assignmentMailFor(t, env, "diego@example.com")
	if mail.Locale != platform.LocaleES {
		t.Errorf("the mail to Diego is in %q, want es — his own Mail Locale, not the buyer's language",
			mail.Locale)
	}
	if !strings.Contains(mail.Subject(), "Tiene una entrada") {
		t.Errorf("subject = %q, want the Spanish", mail.Subject())
	}

	// A stranger the platform has never met has no remembered language, and the
	// floor is English (the fixture's sale carries no Locale).
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "carla@example.com")
	if got := assignmentMailFor(t, env, "carla@example.com").Locale; got != platform.LocaleEN {
		t.Errorf("the mail to a stranger is in %q, want the English floor", got)
	}
}

// RE-SUBMITTING THE ADDRESS A TICKET ALREADY CARRIES MAILS NOBODY.
//
// A buyer who presses save twice, or "corrects" a typo back to what it already
// said, has changed nothing about who holds this Ticket — and the person at that
// address has already been written to once. Without this, a doubled click is a
// doubled mail to somebody who never asked for the first one, from a platform
// they have never heard of.
func TestReassigningToTheSameAddressMailsNobodyAgain(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	first := assignmentMailFor(t, env, "carla@example.com")

	// The same address, typed the way an address book would give it.
	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, " Carla@Example.COM ")

	// assignmentMailFor insists on exactly one, so this fails loudly on a second.
	again := assignmentMailFor(t, env, "carla@example.com")
	if again.AcceptURL != first.AcceptURL {
		t.Error("the second save minted a different link, which means it wrote a new assignment")
	}

	// The buyer is never mailed about their own assignment either. They are
	// looking at the page that just told them.
	for _, mail := range env.email.TicketAssignmentsSent() {
		if mail.To == "ana@example.com" {
			t.Error("the buyer received an Assignment mail about a ticket they gave away")
		}
	}
}

// WHILE THE FLAG IS CLOSED, THIS FEATURE DOES NOT EXIST — no mail, and every
// route answers exactly as an unrouted path does on a build without it
// (ADR 0045).
//
// TICKET_ASSIGNMENT_ENABLED is separate from TICKET_QUESTIONS_ENABLED on
// purpose, so this test opens questions and leaves assignment closed: killing
// assignment must not take Ticket Questions dark.
func TestTheAcceptFlowIsInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	f := newBuyerAnswersFixture(t, env) // questions on, assignment left closed
	ana := customerSignIn(t, env, "ana@example.com")

	resp, body := assignTicket(t, env, ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_ASSIGNMENT_UNAVAILABLE")
	if len(env.email.TicketAssignmentsSent()) != 0 {
		t.Fatal("a closed build mailed somebody")
	}

	// A token from a build where the flag was open would still be refused, and
	// refused as 404 rather than as an invalid link — the address answers as if
	// nothing were routed there.
	for _, call := range []struct {
		method, path string
		body         map[string]any
	}{
		{http.MethodPost, assignmentLinkPath, map[string]any{"token": "anything.at.all"}},
		{http.MethodPut, assignmentLinkNamePath, map[string]any{"token": "x.y", "first_name": "A", "last_name": "B"}},
		{http.MethodPut, assignmentLinkQuestionPath + f.sizeQuestion.ID, map[string]any{"token": "x.y", "text": "M"}},
	} {
		resp, body, _ := answerLinkRequest(t, env, call.method, call.path, call.body)
		assertAPIError(t, resp, body, http.StatusNotFound, "TICKET_ASSIGNMENT_UNAVAILABLE")
	}

	// And Ticket Questions are untouched: the buyer still answers, and the Answer
	// Link still opens. Two flags, one mail flow, and they stay independent.
	answerResp, answerBody := env.put(t, buyerAnswerPath(f.anaSaleID, f.anaTicketIDs[0], f.sizeQuestion.ID),
		map[string]any{"text": "L"}, authHeader(ana))
	if answerResp.StatusCode != http.StatusOK {
		t.Fatalf("closing assignment took Ticket Questions down with it: status=%d error=%+v",
			answerResp.StatusCode, answerBody.Error)
	}
}

// A TAMPERED, TRUNCATED OR INVENTED TOKEN IS REFUSED BEFORE IT REACHES A
// DATABASE LOOKUP, and every failure answers identically.
//
// The signature is checked in constant time before any of the payload is
// trusted, so what fails here is the FORMAT rather than whatever a query happens
// to do with a bad id — and nobody learns which part of their link was wrong.
func TestTheAssignmentLinkRefusesATamperedToken(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	payload, mac, _ := strings.Cut(token, ".")
	for name, bad := range map[string]string{
		"a truncated token":     token[:len(token)-6],
		"a token with no MAC":   payload,
		"a swapped signature":   payload + "." + strings.Repeat("A", len(mac)),
		"an invented token":     "asl1.notatoken",
		"somebody else's words": "hello",
	} {
		resp, body, _ := acceptAssignment(t, env, bad)
		assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")
		if body.Error != nil && strings.Contains(strings.ToLower(body.Error.Message), name) {
			t.Errorf("%s produced a refusal that describes it", name)
		}
	}

	// A blank token is a MALFORMED REQUEST and not an invalid link: the
	// Storefront only ever sends a token it found in the address, so an empty one
	// means the relay is broken rather than that somebody's link is.
	resp, body, _ := acceptAssignment(t, env, "  ")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a blank token answered %d, want 400 VALIDATION_ERROR: %+v", resp.StatusCode, body.Error)
	}
}

// The Holder answers their own Ticket Questions, OF EVERY QUESTION TYPE (#326).
//
// A ROUTE EXISTING IS NOT THE SAME AS SEVEN KINDS WORKING. The Holder's write
// shares one body with the three other answering surfaces — Event Staff, the
// buyer, the Answer Link — so the kinds ought to travel; what this test is for is
// that "ought to" is asserted at the Holder's own seam rather than assumed from
// a neighbour's. Every kind goes in through the Assignment Link and is read back
// off the page the Assignment Link returns.
//
// THE QUESTIONS ARE AUTHORED AFTER THE SALE, which is the ordinary case ADR 0044
// names: "A late-added Ticket Question reaches a holder only if the buyer
// forwards it" — or, now, only if the Holder accepted.
func TestTheHolderAnswersEverySevenKindsThroughTheAssignmentLink(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, token)

	for _, tc := range []struct {
		kind    string
		options []string
		// answer is the body the Holder posts; optionsAt fills option_ids from
		// the created question's own Options for the two choice kinds.
		answer    map[string]any
		optionsAt []int
		// verify reads the Answer back off the Holder's page.
		verify func(t *testing.T, answer *holderAnswer)
	}{
		{
			kind:   "short_text",
			answer: map[string]any{"text": "  S  "},
			verify: func(t *testing.T, a *holderAnswer) {
				// Trimmed and otherwise kept as written: these are the Holder's
				// own words about their own body.
				if a.Text == nil || *a.Text != "S" {
					t.Fatalf("text=%v, want S", a.Text)
				}
			},
		},
		{
			kind:   "long_text",
			answer: map[string]any{"text": "Coeliac, and no shellfish"},
			verify: func(t *testing.T, a *holderAnswer) {
				if a.Text == nil || *a.Text != "Coeliac, and no shellfish" {
					t.Fatalf("text=%v, want the whole sentence", a.Text)
				}
			},
		},
		{
			kind:   "number",
			answer: map[string]any{"number": "4.50"},
			verify: func(t *testing.T, a *holderAnswer) {
				// The trailing zero survives: a NUMERIC column and no float in
				// the path, so nothing the Holder typed rounds.
				if a.Number == nil || *a.Number != "4.50" {
					t.Fatalf("number=%v, want 4.50", a.Number)
				}
			},
		},
		{
			kind:   "date",
			answer: map[string]any{"date": "2026-09-01"},
			verify: func(t *testing.T, a *holderAnswer) {
				if a.Date == nil || *a.Date != "2026-09-01" {
					t.Fatalf("date=%v, want 2026-09-01", a.Date)
				}
			},
		},
		{
			kind:   "checkbox",
			answer: map[string]any{"checked": false},
			verify: func(t *testing.T, a *holderAnswer) {
				// FALSE IS AN ANSWER. A Holder who read the question and said no
				// has said something, and it is not the same as never having
				// been asked — so it must arrive as a present false.
				if a.Checked == nil || *a.Checked {
					t.Fatalf("checked=%v, want a present false", a.Checked)
				}
			},
		},
		{
			kind: "single_choice", options: []string{"S", "M", "L"},
			optionsAt: []int{2},
			verify: func(t *testing.T, a *holderAnswer) {
				if len(a.Options) != 1 || a.Options[0].Label != "L" {
					t.Fatalf("options=%+v, want the one L", a.Options)
				}
			},
		},
		{
			kind: "multi_choice", options: []string{"Vegetarian", "Vegan", "Nuts"},
			optionsAt: []int{1, 2},
			verify: func(t *testing.T, a *holderAnswer) {
				// SEVERAL OPTIONS IN ONE ANSWER — the property this kind exists
				// for, and the one a single_choice code path would silently drop.
				if len(a.Options) != 2 {
					t.Fatalf("options=%d, want 2", len(a.Options))
				}
			},
		},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			body := map[string]any{"label": "Holder question " + tc.kind, "kind": tc.kind}
			if tc.options != nil {
				body["option_labels"] = tc.options
			}
			question := createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, body)

			answerBody := tc.answer
			if tc.optionsAt != nil {
				answerBody = map[string]any{"option_ids": optionIDsAt(question, tc.optionsAt)}
			}

			view := answerByAssignmentLinkOK(t, env, token, question.ID, answerBody)
			answer := holderAnswerFor(t, view, question.ID)
			if answer == nil {
				t.Fatal("the Holder's Answer did not come back on their own page")
			}
			tc.verify(t, answer)
		})
	}
}

// A CHOICE ANSWER RECORDS THE OPTION CHOSEN TOGETHER WITH THE WORDS IT SHOWED AT
// THE TIME, on the Holder's route as on every other (#326).
//
// This is asserted at the staff seam already
// (TestRenamingAnOptionLeavesTheAnswersSnapshotIntact); what is asserted here is
// that the Holder's own write produces the same pair of facts, because the
// snapshot is what makes an Answer mean something a year later. The identity
// keeps the Answer attached across a rename; the snapshot is what the Holder
// actually read when they chose.
func TestAHolderChoiceAnswerRecordsTheOptionAndTheWordsItShowed(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	question := createTicketQuestion(t, env, f.staffSession, f.eventID, f.ticketTypeID, map[string]any{
		"label": "Main course", "kind": "single_choice",
		"option_labels": []string{"Chicken", "Fish"},
	})
	chickenID := question.Options[0].ID

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, token)

	view := answerByAssignmentLinkOK(t, env, token, question.ID,
		map[string]any{"option_ids": []string{chickenID}})
	chosen := holderAnswerFor(t, view, question.ID)
	if len(chosen.Options) != 1 {
		t.Fatalf("options=%+v, want the one the Holder chose", chosen.Options)
	}
	if chosen.Options[0].OptionID != chickenID {
		t.Fatalf("option_id=%q, want %q — the Answer must name the Option itself",
			chosen.Options[0].OptionID, chickenID)
	}
	if chosen.Options[0].Label != "Chicken" {
		t.Fatalf("snapshot=%q, want the words the Holder read", chosen.Options[0].Label)
	}

	// The Organization corrects the wording months later. The Holder chose from
	// a menu that said "Chicken", and no later edit may rewrite what they read.
	resp, body := env.patch(t, optionPath(f.eventID, f.ticketTypeID, question.ID, chickenID),
		map[string]any{"label": "Chicken (halal)"}, authHeader(f.staffSession))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rename status=%d error=%+v", resp.StatusCode, body.Error)
	}

	after := holderAnswerFor(t, acceptAssignmentOK(t, env, token), question.ID)
	if len(after.Options) != 1 || after.Options[0].OptionID != chickenID {
		t.Fatalf("options=%+v — the rename forked the Answer off its Option", after.Options)
	}
	if after.Options[0].Label != "Chicken" {
		t.Fatalf("snapshot=%q — a rename rewrote what the Holder read", after.Options[0].Label)
	}
	if after.Options[0].CurrentLabel != "Chicken (halal)" {
		t.Fatalf("current label=%q, want the correction", after.Options[0].CurrentLabel)
	}
}

// THE HOLDER'S ANSWER OVERWRITES WHAT THE BUYER GUESSED, resolves that Ticket's
// Outstanding Answer, and moves `updated_at` and nothing else (#326).
//
// This is the whole point of the accept step. Ana bought a ticket for Carla and
// guessed a size; Carla is the person wearing the shirt. There is nothing
// special in the code about whose write it is — an Answer belongs to the TICKET
// (ADR 0044), so one row per (Ticket, question) is what "overwrites" means — and
// nothing anywhere records that it was the Holder rather than the buyer.
func TestTheHolderOverwritesWhatTheBuyerGuessedAndResolvesTheOutstandingAnswer(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	// The buyer guesses at the size — the required question, so before she does
	// the Ticket owes an Outstanding Answer.
	if owed := labelsOwedBy(listOutstanding(t, env, f.staffSession, f.eventID), ticketID); len(owed) != 1 {
		t.Fatalf("owed=%v, want the one required question outstanding before anybody answers", owed)
	}
	guessResp, guessBody := env.put(t, buyerAnswerPath(f.anaSaleID, ticketID, f.sizeQuestion.ID),
		map[string]any{"text": "XXL"}, authHeader(f.ana))
	if guessResp.StatusCode != http.StatusOK {
		t.Fatalf("the buyer's guess status=%d error=%+v", guessResp.StatusCode, guessBody.Error)
	}

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))

	// THE GUESS IS ON THE HOLDER'S OWN PAGE. Somebody cannot correct a guess
	// they cannot see, and this is data about the Holder rather than about the
	// purchase.
	accepted := acceptAssignmentOK(t, env, token)
	guessed := holderAnswerFor(t, accepted, f.sizeQuestion.ID)
	if guessed == nil || guessed.Text == nil || *guessed.Text != "XXL" {
		t.Fatalf("the buyer's guess is not shown to the Holder: %+v", guessed)
	}

	// The clock moves so that a real change can be told from a re-submission.
	later := env.fixedClock.Add(time.Hour)
	sharedApp.CatalogService.WithClock(func() time.Time { return later })

	corrected := holderAnswerFor(t,
		answerByAssignmentLinkOK(t, env, token, f.sizeQuestion.ID, map[string]any{"text": "S"}),
		f.sizeQuestion.ID)
	if corrected.Text == nil || *corrected.Text != "S" {
		t.Fatalf("text=%v, want the Holder's own S over the buyer's XXL", corrected.Text)
	}
	// `updated_at` IS KEPT — it is the only history this platform holds.
	if !corrected.UpdatedAt.After(guessed.UpdatedAt) {
		t.Fatalf("updated_at did not move when the Holder corrected the guess: %v then %v",
			guessed.UpdatedAt, corrected.UpdatedAt)
	}

	// AND WHO CHANGED IT IS NOT. One row per (Ticket, question), no earlier
	// version of it, and no column naming an author — ADR 0046 rejected the
	// history table for the same reason migration 073 has no author column.
	var rows int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1 AND ticket_question_id = $2
	`, ticketID, f.sizeQuestion.ID).Scan(&rows); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows=%d, want exactly one Answer per (Ticket, Ticket Question)", rows)
	}
	assertNoAnswerAuthorColumn(t, env)

	// ANSWERING RESOLVES THAT TICKET'S OUTSTANDING ANSWERS. The debt is computed
	// from whether a row exists, so the Holder's route pays it down exactly as
	// the other three do — the Organization stops chasing this Ticket.
	if owed := labelsOwedBy(listOutstanding(t, env, f.staffSession, f.eventID), ticketID); owed != nil {
		t.Fatalf("owed=%v, want nothing outstanding once the Holder has answered", owed)
	}
}

// assertNoAnswerAuthorColumn reads the Answer table's own shape, because the
// property is the ABSENCE of a fact and no API response can show an absence that
// convincingly.
//
// The platform records the current Answer and when it changed. An author column
// would be a permanent record of which of four friends said what about
// somebody's body, and ADR 0046 rejected exactly that store.
func assertNoAnswerAuthorColumn(t *testing.T, env *testEnv) {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'ticket_answers'
	`)
	if err != nil {
		t.Fatalf("read the Answer table's columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		for _, forbidden := range []string{
			"answered_by", "author", "author_id", "written_by", "source", "holder_customer_id",
		} {
			if column == forbidden {
				t.Errorf("ticket_answers.%s records WHO changed an Answer; the platform keeps when, not who", column)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
}

// EVENT STAFF KEEP CORRECTING ANY ANSWER ON ANY TICKET OF THEIR EVENT, including
// one whose Holder has accepted (#326).
//
// ADR 0044's three-party rule survives ADR 0046 intact. What accepting closes is
// the UNAUTHENTICATED door — the forwarded Answer Link that a group chat could
// use to change somebody's size as a joke — and not the two accountable ones. A
// Holder who typed the wrong thing rings the Organization, and somebody there
// must be able to fix it.
func TestEventStaffCorrectAnAnswerOnAnAcceptedTicket(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)
	ticketID := f.anaTicketIDs[0]

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, token)
	answerByAssignmentLinkOK(t, env, token, f.sizeQuestion.ID, map[string]any{"text": "S"})

	// The forwarded door is shut, and this is the same Ticket through it.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut,
		answerLinkQuestionPath+f.sizeQuestion.ID,
		map[string]any{"token": answerLinkToken(t, ticketID), "text": "XXL"})
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ANSWER_LINK_INVALID")

	// Event Staff go through anyway, on their own authenticated route.
	corrected := putAnswer(t, env, f.staffSession, f.eventID, ticketID, f.sizeQuestion.ID,
		map[string]any{"text": "M"})
	if answer := answerFor(t, corrected, f.sizeQuestion.ID); answer.Text == nil || *answer.Text != "M" {
		t.Fatalf("text=%v — Event Staff lost their route once a Holder accepted", answer.Text)
	}

	// And the Holder sees the correction on their own page: one Answer per
	// (Ticket, question), whoever last wrote it.
	if shown := holderAnswerFor(t, acceptAssignmentOK(t, env, token), f.sizeQuestion.ID); shown.Text == nil || *shown.Text != "M" {
		t.Fatalf("the Holder's page shows %v, want the staff correction", shown.Text)
	}
}

// THE HOLDER EDITS UNTIL THE EVENT STARTS, AND NEVER ON A REVERSED SALE (#326).
//
// The accept route's window is asserted next door; this is the WRITE going
// through the same gate, which is the point of there being one gate and not
// three. If the read refused and the write did not, the write would confirm to a
// stale reader that a Ticket the read had told them nothing about still exists.
//
// The deadline is the Event's start as it stands NOW — an Organization that
// moves its Event moves every Holder's deadline with it — read as an instant, so
// the Event's own timezone is what fixes when the doors open.
func TestTheHoldersEditWindowClosesAtTheDoorsAndOnAReversedSale(t *testing.T) {
	env := setupTest(t)
	f := newAssignmentFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	carla := assignmentTokenFrom(t, assignmentMailFor(t, env, "carla@example.com"))
	acceptAssignmentOK(t, env, carla)
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "diego@example.com")
	diego := assignmentTokenFrom(t, assignmentMailFor(t, env, "diego@example.com"))
	acceptAssignmentOK(t, env, diego)

	// While the Event is ahead of them, both may write.
	answerByAssignmentLinkOK(t, env, carla, f.sizeQuestion.ID, map[string]any{"text": "S"})

	// The doors open. The window closes for the Holder too — and this is also
	// when #322's purge takes an unaccepted address.
	setEventStart(t, env, f.eventID, env.fixedClock.Add(-time.Hour))
	resp, body, _ := answerByAssignmentLink(t, env, carla, f.sizeQuestion.ID, map[string]any{"text": "M"})
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_EXPIRED")

	// Put the Event back in the future and reverse the Sale instead. A reversed
	// Sale is indistinguishable from a forgery here: "your friend cancelled the
	// purchase" is a fact about somebody else's money, and the Holder is never
	// told who the buyer is.
	setEventStart(t, env, f.eventID, env.fixedClock.Add(30*24*time.Hour))
	if _, err := env.db.Exec(
		`UPDATE ticket_sales SET status = 'reversed' WHERE id = $1`, f.anaSaleID,
	); err != nil {
		t.Fatalf("reverse the Sale: %v", err)
	}
	resp, body, _ = answerByAssignmentLink(t, env, diego, f.sizeQuestion.ID, map[string]any{"text": "L"})
	assertAPIError(t, resp, body, http.StatusUnauthorized, "ASSIGNMENT_LINK_INVALID")
	if strings.Contains(strings.ToLower(body.Error.Message), "revers") {
		t.Error("the refusal tells the Holder the Sale was reversed; that is a fact about somebody else's money")
	}

	// AND THE ANSWER ALREADY GIVEN SURVIVES THE REVERSAL. What is recorded stays
	// recorded; the window governs writing, not reading.
	var text sql.NullString
	if err := env.db.QueryRow(`
		SELECT text_value FROM ticket_answers WHERE ticket_id = $1 AND ticket_question_id = $2
	`, f.anaTicketIDs[0], f.sizeQuestion.ID).Scan(&text); err != nil {
		t.Fatalf("read the Holder's Answer: %v", err)
	}
	if text.String != "S" {
		t.Fatalf("text=%v — a Sale Reversal erased the Holder's Answer", text)
	}
}
