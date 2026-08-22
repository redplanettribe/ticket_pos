package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Answer Link: a signed, stateless link opening ONE Ticket's Ticket
// Questions and nothing else, for whoever ends up holding that ticket (#312,
// ADR 0044).
//
// These run through the public API because that is where the properties live.
// The link is unauthenticated by design and gets forwarded into group chats, so
// what it ANSWERS WITH is the whole of its security, and only an end-to-end
// request can assert on the bytes that actually leave the process.

// answerLinkView decodes what an Answer Link opens.
//
// IT IS DELIBERATELY A NARROW STRUCT AND THAT IS NOT ENOUGH ON ITS OWN. Decoding
// into a type with three fields would silently drop a fourth the API had started
// sending — which is exactly the regression these tests exist to catch — so the
// disclosure test below asserts on the RAW BODY and never on this.
type answerLinkView struct {
	EventName      string `json:"event_name"`
	TicketTypeName string `json:"ticket_type_name"`
	Questions      []struct {
		Question ticketQuestion `json:"question"`
		Answer   *struct {
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
		} `json:"answer"`
	} `json:"questions"`
}

const (
	answerLinkPath         = "/api/v1/public/answer-link"
	answerLinkQuestionPath = "/api/v1/public/answer-link/questions/"
)

// answerLinkToken mints the Answer Link for one Ticket through the same service
// method #315's buyer-facing page will call.
//
// Minting through the REAL signer rather than forging a token in the test is the
// point: it proves the mint and the open agree about the format, and it means
// nothing here knows the signing key — which is how a holder is placed too.
func answerLinkToken(t *testing.T, ticketID string) string {
	t.Helper()
	url, err := sharedApp.CatalogService.AnswerLinkURL(ticketID)
	if err != nil {
		t.Fatalf("mint Answer Link for %s: %v", ticketID, err)
	}
	_, token, found := strings.Cut(url, "?token=")
	if !found || token == "" {
		t.Fatalf("minted Answer Link carries no token: %q", url)
	}
	return token
}

// openAnswerLink opens a link and returns the response and the RAW body.
//
// The raw bytes are returned alongside the decoded envelope because the central
// assertion of this feature is about what the response does NOT contain, and a
// struct can only assert on fields somebody remembered to declare.
func openAnswerLink(t *testing.T, env *testEnv, token string) (*http.Response, envelope, []byte) {
	t.Helper()
	return answerLinkRequest(t, env, http.MethodPost, answerLinkPath, map[string]any{"token": token})
}

func answerLinkRequest(t *testing.T, env *testEnv, method, path string, body any) (*http.Response, envelope, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(method, env.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// NO AUTHORIZATION HEADER ANYWHERE IN THIS FILE, and no cookie jar. Whoever
	// holds an Answer Link has no session, is not a Customer, and never becomes
	// one; a test that signed in first would be testing a different thing.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded envelope
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode envelope from %q: %v", raw, err)
	}
	return resp, decoded, raw
}

func decodeAnswerLinkView(t *testing.T, data json.RawMessage) answerLinkView {
	t.Helper()
	var view answerLinkView
	if err := json.Unmarshal(data, &view); err != nil {
		t.Fatalf("decode answer link view: %v", err)
	}
	return view
}

// answerLinkFixture is a sold Ticket Type whose buyer is made of sentinels.
//
// EVERY VALUE HERE IS UNMISTAKABLE ON PURPOSE — "Buyerkorp", "buyer-secret@",
// "0912345678" — so that the disclosure test can search the response for each of
// them and a hit means what it looks like. A fixture buyer called Ana Lopez
// would make "Ana" a substring of half the alphabet and the assertion a
// coincidence away from passing.
//
// It returns two Tickets because "one Ticket's link never opens another's" needs
// two, and a required question plus an optional one because the page has to show
// both.
func answerLinkFixture(t *testing.T, env *testEnv) (
	sessionID, eventID, ticketTypeID, ticketSaleID, batchID, confirmationRef string,
	ticketIDs []string,
) {
	t.Helper()
	sessionID = orgAdminSession(t, env)
	eventID = createDraftEvent(t, env, sessionID, "Sentinel Fest", "sentinel-fest")
	ticketTypeID = createTicketTypeWithCapacity(t, env, sessionID, eventID, "Backstage Pass", 4321, 50)
	batchID = commitBatch(t, env, sessionID, eventID, "batch-answer-link", []map[string]any{{
		"customer_email":      "buyer-secret@example.com",
		"customer_first_name": "Buyerkorp",
		"customer_last_name":  "Payerson",
		"ticket_type_id":      ticketTypeID,
		"quantity":            2,
		"payment_method":      "cash",
		"sold_at":             "2026-07-01T10:00:00Z",
	}})

	if err := env.db.QueryRow(`
		SELECT id, confirmation_ref FROM ticket_sales WHERE event_id = $1
	`, eventID).Scan(&ticketSaleID, &confirmationRef); err != nil {
		t.Fatalf("read Ticket Sale: %v", err)
	}

	// The Tax ID is written straight onto the Customer, because a Sale Import
	// takes none and the point of the assertion is that the PAGE never shows one
	// however it got there.
	if _, err := env.db.Exec(`
		UPDATE customers SET tax_id_type = 'cedula', tax_id_number = '0912345678'
		WHERE email = 'buyer-secret@example.com'
	`); err != nil {
		t.Fatalf("set buyer Tax ID: %v", err)
	}

	rows, err := env.db.Query(`
		SELECT tk.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = $1
		ORDER BY tk.ordinal ASC
	`, ticketSaleID)
	if err != nil {
		t.Fatalf("read Tickets: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan Ticket: %v", err)
		}
		ticketIDs = append(ticketIDs, id)
	}
	if len(ticketIDs) != 2 {
		t.Fatalf("Tickets minted = %d, want 2", len(ticketIDs))
	}
	return sessionID, eventID, ticketTypeID, ticketSaleID, batchID, confirmationRef, ticketIDs
}

// TestAnswerLinkDisclosesNothingAboutThePurchase is THE test this feature exists
// to keep passing.
//
// ADR 0044: "Answer Links are unauthenticated URLs that answer for a person.
// Their safety rests entirely on disclosing nothing and on expiring at Event
// start. Anyone widening what the page shows — adding the buyer's name 'for
// context', or the confirmation reference 'to help support' — is reversing this
// decision."
//
// So this asserts on the RAW RESPONSE BODY rather than on a decoded struct.
// Decoding would silently drop any field somebody added, which is precisely the
// change this must fail on; a substring search over the bytes that left the
// process cannot be fooled that way. It runs on BOTH routes, because the write
// answers with the same payload and a leak added to one and not the other would
// otherwise ship.
func TestAnswerLinkDisclosesNothingAboutThePurchase(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, ticketSaleID, _, confirmationRef, ticketIDs := answerLinkFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	// Everything the holder must never learn, each named by what it is so a
	// failure reads as a sentence.
	forbidden := map[string]string{
		"the buyer's first name":          "Buyerkorp",
		"the buyer's last name":           "Payerson",
		"the buyer's email":               "buyer-secret@example.com",
		"the buyer's Tax ID":              "0912345678",
		"the price paid, in cents":        "4321",
		"the Sale Confirmation reference": confirmationRef,
		"the Ticket Sale's id":            ticketSaleID,
		// The Ticket's own id: the form posts back with the token, so the page
		// needs none, and an id in the payload is a thing to try elsewhere.
		"this Ticket's id": ticketIDs[0],
		// The SIBLING Ticket is the sharpest one. "The Sale's other Tickets"
		// is the disclosure a group chat would notice.
		"the sibling Ticket's id": ticketIDs[1],
	}

	token := answerLinkToken(t, ticketIDs[0])

	for _, route := range []struct {
		name string
		call func() (*http.Response, envelope, []byte)
	}{
		{"opening the link", func() (*http.Response, envelope, []byte) {
			return openAnswerLink(t, env, token)
		}},
		{"answering through the link", func() (*http.Response, envelope, []byte) {
			return answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
				map[string]any{"token": token, "text": "L"})
		}},
	} {
		t.Run(route.name, func(t *testing.T) {
			resp, body, raw := route.call()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
			}

			for what, secret := range forbidden {
				if strings.Contains(string(raw), secret) {
					t.Errorf("the response discloses %s (%q).\n"+
						"An Answer Link gets forwarded into group chats. See ADR 0044 and\n"+
						"service.AnswerLinkView before widening this payload.\nbody: %s",
						what, secret, raw)
				}
			}

			// And the positive half, so the test cannot pass by the page having
			// gone blank: the three things it IS meant to show are all there.
			view := decodeAnswerLinkView(t, body.Data)
			if view.EventName != "Sentinel Fest" {
				t.Errorf("event_name = %q, want the Event's name", view.EventName)
			}
			if view.TicketTypeName != "Backstage Pass" {
				t.Errorf("ticket_type_name = %q, want the Ticket Type's name", view.TicketTypeName)
			}
			if len(view.Questions) != 1 || view.Questions[0].Question.Label != "T-shirt size" {
				t.Errorf("questions = %+v, want the one Ticket Question", view.Questions)
			}
		})
	}
}

// ONE TICKET'S LINK NEVER OPENS ANOTHER'S — asserted end to end, on two Tickets
// of the SAME Ticket Sale, which is the case where a bug would be invisible: two
// siblings share an Event, a Ticket Type, a buyer and a set of questions, and
// differ only in the Answers that belong to each holder.
func TestAnswerLinkOpensOnlyItsOwnTicket(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	first, second := answerLinkToken(t, ticketIDs[0]), answerLinkToken(t, ticketIDs[1])
	if first == second {
		t.Fatal("two Tickets of one Sale minted the same Answer Link")
	}

	// The first holder answers. The second's page must be untouched by it.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
		map[string]any{"token": first, "text": "XL"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first holder's answer status=%d error=%+v", resp.StatusCode, body.Error)
	}

	_, body, _ = openAnswerLink(t, env, second)
	sibling := decodeAnswerLinkView(t, body.Data)
	if len(sibling.Questions) != 1 {
		t.Fatalf("sibling's questions = %d, want 1", len(sibling.Questions))
	}
	if sibling.Questions[0].Answer != nil {
		t.Fatalf("the sibling Ticket's link shows an Answer given through another Ticket's link: %+v",
			sibling.Questions[0].Answer)
	}

	// And the first holder's own link still reads what they said, so the
	// assertion above is about isolation and not about the write having failed.
	_, body, _ = openAnswerLink(t, env, first)
	mine := decodeAnswerLinkView(t, body.Data)
	if mine.Questions[0].Answer == nil || mine.Questions[0].Answer.Text == nil ||
		*mine.Questions[0].Answer.Text != "XL" {
		t.Fatalf("the answering holder's own link = %+v, want XL", mine.Questions[0].Answer)
	}
}

// A TAMPERED OR TRUNCATED LINK IS REFUSED, and refused identically however it
// was broken. The token format's own tests walk every truncation
// (catalog/answer_link_test.go); this asserts the API's answer, which is the
// part a holder sees.
func TestAnswerLinkRefusesATamperedToken(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	_, _, _, _, _, _, ticketIDs := answerLinkFixture(t, env)

	good := answerLinkToken(t, ticketIDs[0])
	payload, mac, _ := strings.Cut(good, ".")
	// A token minted for the sibling, so the swap below is a real forgery
	// attempt rather than noise.
	siblingPayload, _, _ := strings.Cut(answerLinkToken(t, ticketIDs[1]), ".")

	for name, token := range map[string]string{
		"truncated halfway":                          good[:len(good)/2],
		"truncated to its payload":                   payload,
		"missing its signature":                      payload + ".",
		"the sibling's payload, our mac":             siblingPayload + "." + mac,
		"one character changed in the mac":           payload + "." + strings.Repeat("A", len(mac)),
		"invented":                                   "bm90LWEtdG9rZW4.bm90LWEtbWFj",
		"a Ticket id sent as though it were a token": ticketIDs[0],
	} {
		t.Run(name, func(t *testing.T) {
			resp, body, _ := openAnswerLink(t, env, token)
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status=%d, want 401; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "ANSWER_LINK_INVALID" {
				t.Fatalf("error=%+v, want ANSWER_LINK_INVALID", body.Error)
			}
		})
	}

	// The good one still opens, so the loop above was not passing against a
	// surface that refuses everything.
	resp, body, _ := openAnswerLink(t, env, good)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the untampered link status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// AN ANSWER GIVEN BY LINK REPLACES ONE THE BUYER GAVE, in both directions: the
// holder overwrites the buyer's guess, and Event Staff can still correct the
// holder afterwards. One Answer per (Ticket, question) is the model, so
// "replaces" is what the row means rather than a rule anybody enforces.
func TestAnswerByLinkReplacesWhatWasAlreadyThere(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)
	ticketID := ticketIDs[0]

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})

	// Standing in for the buyer's checkout guess (#311 owns the checkout form
	// itself; the Answer it produces is this same row).
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"text": "S"})

	token := answerLinkToken(t, ticketID)
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
		map[string]any{"token": token, "text": "XL"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("holder's answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	view := decodeAnswerLinkView(t, body.Data)
	if view.Questions[0].Answer == nil || *view.Questions[0].Answer.Text != "XL" {
		t.Fatalf("holder's answer = %+v, want XL", view.Questions[0].Answer)
	}

	// EXACTLY ONE ROW, not two. The staff surface reads the same Answer, which
	// is what "an Answer belongs to the Ticket" means: there is no second copy
	// somewhere keyed on who wrote it.
	var rowCount int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM ticket_answers WHERE ticket_id = $1 AND ticket_question_id = $2
	`, ticketID, question.ID).Scan(&rowCount); err != nil {
		t.Fatalf("count Answers: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("ticket_answers rows = %d, want exactly 1 — an Answer replaces, it does not accumulate", rowCount)
	}

	staffView := decodeTicketAnswers(t, mustGetTicket(t, env, sessionID, eventID, ticketID))
	if answer := answerFor(t, staffView, question.ID); answer == nil || *answer.Text != "XL" {
		t.Fatalf("staff read the Answer as %+v, want the holder's XL", answer)
	}

	// And staff correcting it afterwards is read back through the link, because
	// there is one Answer and three parties who may supply it.
	putAnswer(t, env, sessionID, eventID, ticketID, question.ID, map[string]any{"text": "M"})
	_, body, _ = openAnswerLink(t, env, token)
	corrected := decodeAnswerLinkView(t, body.Data)
	if *corrected.Questions[0].Answer.Text != "M" {
		t.Fatalf("the link reads %+v after a staff correction, want M", corrected.Questions[0].Answer)
	}
}

// ANSWERING CREATES NO CUSTOMER AND NO SESSION. ADR 0044 turns on this: the
// platform collects nothing about whoever holds a link, so a row appearing for
// them would be the decision quietly reversed. Counted rather than inspected,
// because the failure would be a row nobody meant to write.
func TestAnswerLinkCreatesNoCustomerAndNoSession(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)

	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "Dietary requirements", "kind": "long_text", "required": false,
	})

	count := func(table string) int {
		t.Helper()
		var n int
		if err := env.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	customersBefore, sessionsBefore := count("customers"), count("customer_sessions")

	token := answerLinkToken(t, ticketIDs[0])
	resp, _, _ := openAnswerLink(t, env, token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open status=%d", resp.StatusCode)
	}
	// The response must also hand back no credential of any kind.
	if cookies := resp.Cookies(); len(cookies) > 0 {
		t.Fatalf("opening an Answer Link set cookies: %+v", cookies)
	}

	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
		map[string]any{"token": token, "text": "No nuts, please."})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("answer status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if cookies := resp.Cookies(); len(cookies) > 0 {
		t.Fatalf("answering through an Answer Link set cookies: %+v", cookies)
	}

	if got := count("customers"); got != customersBefore {
		t.Errorf("customers = %d, want %d — holding an Answer Link makes nobody a Customer", got, customersBefore)
	}
	if got := count("customer_sessions"); got != sessionsBefore {
		t.Errorf("customer_sessions = %d, want %d — an Answer Link mints no session", got, sessionsBefore)
	}
}

// THE LINK STOPS OPENING ONCE THE EVENT HAS STARTED, in the Event's timezone —
// which is already baked into the stored instant, so this is an instant
// comparison and catalog.AnswerWindow is the one place it happens.
//
// It is the one refusal told apart from ANSWER_LINK_INVALID, because an Event's
// start is already published on the Storefront.
func TestAnswerLinkStopsOpeningOnceTheEventHasStarted(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	token := answerLinkToken(t, ticketIDs[0])

	// An hour before the doors: open.
	setEventStart(t, env, eventID, env.fixedClock.Add(time.Hour))
	if resp, body, _ := openAnswerLink(t, env, token); resp.StatusCode != http.StatusOK {
		t.Fatalf("before the doors: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// EXACTLY AT THE DOORS: late. A half-open interval, like the Reversal
	// Window's closing instant — somebody arriving as the doors open is late.
	setEventStart(t, env, eventID, env.fixedClock)
	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope, []byte)
	}{
		{"opening", func() (*http.Response, envelope, []byte) { return openAnswerLink(t, env, token) }},
		{"answering", func() (*http.Response, envelope, []byte) {
			return answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
				map[string]any{"token": token, "text": "L"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body, _ := tc.call()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status=%d, want 401; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "ANSWER_LINK_EXPIRED" {
				t.Fatalf("error=%+v, want ANSWER_LINK_EXPIRED", body.Error)
			}
		})
	}
}

// THE LINK STOPS OPENING ONCE ITS TICKET SALE IS REVERSED — and says only that
// it is not valid.
//
// The refusal is deliberately indistinguishable from a forgery's. Reporting
// "this sale was reversed" would be telling whoever the link was forwarded to a
// fact about somebody else's money, which is the one category of thing this page
// exists to disclose nothing about. Event Staff, who ARE entitled to know, get
// TICKET_SALE_REVERSED on their own route — and that difference is the whole
// disclosure rule in one pair of error codes.
func TestAnswerLinkStopsOpeningOnAReversedSale(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, batchID, _, ticketIDs := answerLinkFixture(t, env)
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	token := answerLinkToken(t, ticketIDs[0])

	if resp, body, _ := openAnswerLink(t, env, token); resp.StatusCode != http.StatusOK {
		t.Fatalf("before the reversal: status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// The Sale Import undo route, which is a real Sale Reversal.
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports/"+batchID+"/undo",
		map[string]any{"notify_buyers": false}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("undo status=%d error=%+v", resp.StatusCode, body.Error)
	}

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope, []byte)
	}{
		{"opening", func() (*http.Response, envelope, []byte) { return openAnswerLink(t, env, token) }},
		{"answering", func() (*http.Response, envelope, []byte) {
			return answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
				map[string]any{"token": token, "text": "L"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body, raw := tc.call()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status=%d, want 401; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "ANSWER_LINK_INVALID" {
				t.Fatalf("error=%+v, want ANSWER_LINK_INVALID and never a code naming the reversal", body.Error)
			}
			// Nor may the message say so in words.
			for _, leak := range []string{"revers", "Revers", "refund", "Refund"} {
				if strings.Contains(string(raw), leak) {
					t.Errorf("the refusal tells the holder the Sale was reversed (%q): %s", leak, raw)
				}
			}
		})
	}
}

// WHILE THE FLAG IS OFF, THE ANSWER LINK IS NOT THERE. Same 404 and same code
// the staff routes give, so a dark build is indistinguishable from one that
// never had the feature (ADR 0045) — including on a link that is perfectly valid
// and would open the moment the flag flipped.
func TestAnswerLinkIsInvisibleWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	// Enabled only long enough to author a question and mint a link, then closed
	// again — which is what a deployment turning the flag back off looks like.
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)
	question := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "T-shirt size", "kind": "short_text", "required": true,
	})
	token := answerLinkToken(t, ticketIDs[0])
	sharedApp.CatalogService.WithTicketQuestions(false)

	for _, tc := range []struct {
		name string
		call func() (*http.Response, envelope, []byte)
	}{
		{"opening", func() (*http.Response, envelope, []byte) { return openAnswerLink(t, env, token) }},
		{"answering", func() (*http.Response, envelope, []byte) {
			return answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+question.ID,
				map[string]any{"token": token, "text": "L"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body, _ := tc.call()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("status=%d, want 404 while the flag is off; error=%+v", resp.StatusCode, body.Error)
			}
			if body.Error == nil || body.Error.Code != "TICKET_QUESTIONS_UNAVAILABLE" {
				t.Fatalf("error=%+v, want TICKET_QUESTIONS_UNAVAILABLE", body.Error)
			}
		})
	}
}

// An Answer given by link goes through the same catalog.ParseAnswer every other
// route does, so a value that does not fit its question's kind is refused here
// with the same code and the same problem token.
//
// The property is that there is ONE set of rules and not a second, looser one
// behind the unauthenticated door.
func TestAnswerLinkHoldsTheSameAnswerRules(t *testing.T) {
	env := setupTest(t)
	enableTicketQuestions(t)
	sessionID, eventID, ticketTypeID, _, _, _, ticketIDs := answerLinkFixture(t, env)
	number := createTicketQuestion(t, env, sessionID, eventID, ticketTypeID, map[string]any{
		"label": "How many guests", "kind": "number", "required": false,
	})
	token := answerLinkToken(t, ticketIDs[0])

	// Text sent to a number question: a caller believing something untrue about
	// that question, refused rather than coerced.
	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+number.ID,
		map[string]any{"token": token, "text": "three"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "INVALID_ANSWER" {
		t.Fatalf("error=%+v, want INVALID_ANSWER", body.Error)
	}
	if got := answerDetail(body, "problem"); got != "wrong_shape" {
		t.Fatalf("problem = %q, want wrong_shape", got)
	}

	// A question belonging to somebody else's Ticket Type is not on this Ticket,
	// and is refused as no such question — never as "not yours", which would
	// confirm the id exists.
	otherEvent := createDraftEvent(t, env, sessionID, "Other Fest", "other-fest")
	otherType := createTicketTypeWithCapacity(t, env, sessionID, otherEvent, "GA", 1000, 10)
	stranger := createTicketQuestion(t, env, sessionID, otherEvent, otherType, map[string]any{
		"label": "Not your question", "kind": "short_text", "required": false,
	})
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut, answerLinkQuestionPath+stranger.ID,
		map[string]any{"token": token, "text": "L"})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "TICKET_QUESTION_NOT_FOUND" {
		t.Fatalf("error=%+v, want TICKET_QUESTION_NOT_FOUND", body.Error)
	}
}

// setEventStart moves an Event's start directly, because the staff route refuses
// a start in the past and the whole point here is to stand on the far side of
// one.
func setEventStart(t *testing.T, env *testEnv, eventID string, startsAt time.Time) {
	t.Helper()
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $1 WHERE id = $2`, startsAt, eventID); err != nil {
		t.Fatalf("set Event start: %v", err)
	}
}

// mustGetTicket reads a Ticket through the staff route, failing on any refusal.
func mustGetTicket(t *testing.T, env *testEnv, sessionID, eventID, ticketID string) json.RawMessage {
	t.Helper()
	resp, body := env.get(t, ticketPath(eventID, ticketID), authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff read of the Ticket status=%d error=%+v", resp.StatusCode, body.Error)
	}
	return body.Data
}
