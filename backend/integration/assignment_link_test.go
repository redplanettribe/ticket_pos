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
		Answer   *struct {
			Text    *string `json:"text"`
			Number  *string `json:"number"`
			Date    *string `json:"date"`
			Checked *bool   `json:"checked"`
		} `json:"answer"`
	} `json:"questions"`
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
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": "  ", "last_name": "Díaz",
	})
	assertAPIError(t, resp, body, http.StatusBadRequest, "INVALID_HOLDER_NAME")
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

	assignTicketOK(t, env, f.ana, f.anaSaleID, ticketID, "carla@example.com")

	// WHILE MERELY `assigned`, THE OLD DOOR IS STILL OPEN. Nothing is bricked by
	// a mail nobody clicked.
	forwarded := answerLinkToken(t, ticketID)
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
