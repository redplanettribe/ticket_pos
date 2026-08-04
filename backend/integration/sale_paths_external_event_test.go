package integration

import (
	"net/http"
	"testing"
)

// Sale paths against an externally registered Event (issue #212). Every path
// that would record a Ticket Sale refuses before it resolves a Ticket Type, with
// the dedicated EVENT_IS_EXTERNAL_REGISTRATION code (ADR 0028).
//
// The refusal has to come first, not last. Each of these paths would fail
// anyway — an external Event has no Ticket Types, so every row names one that
// does not exist — but it would fail as a ticket-type-not-found, sending an
// Event Staff member, or an Integration Partner's program, hunting for a data
// problem that is not there. The truth is that this Event does not sell tickets
// and never will, and that is what the code says.
//
// TWO OF THE PATHS THE ISSUE NAMES DO NOT EXIST YET. There is no In-Person Sale
// endpoint (the POS is unbuilt; `in_person` appears only as a Sales Channel
// value on the sales list filter and in the schema CHECK) and no Integration
// Partner endpoint (`internal/integrations` is a package stub, and no route is
// registered for one). Both are Sale Import's neighbours and will record a
// Ticket Sale through the sales service, where the guard lives, rather than
// beside it — so they inherit the refusal when they land, and the test that
// proves it belongs with them. What is testable today is the Sale Import, in
// both its forms and for both Sales Sources, plus the online checkout the issue
// says needs no change.

// importPayload is a one-row Sale Import body naming whatever Ticket Type the
// caller passes. On an external Event the id is necessarily a fiction, which is
// the point: the refusal must not depend on it being one.
func importPayload(source, idempotencyKey, ticketTypeID string) map[string]any {
	return map[string]any{
		"idempotency_key": idempotencyKey,
		"source":          source,
		"sales": []map[string]any{{
			"customer_email":      "ana@example.com",
			"customer_first_name": "Ana",
			"customer_last_name":  "Lopez",
			"ticket_type_id":      ticketTypeID,
			"quantity":            2,
			"payment_method":      "cash",
			"sold_at":             "2026-07-01T10:00:00Z",
		}},
	}
}

const externalImportCSV = "customer_email,customer_first_name,customer_last_name,ticket_type,quantity,payment_method,sold_at,amount\n" +
	"ana@example.com,Ana,Lopez,GA,2,cash,2026-07-01T10:00:00Z,\n"

// assertExternalRegistrationRefusal insists on both halves of the contract: the
// 409 and, more importantly, the code — a TICKET_TYPE_NOT_FOUND here would pass
// any "it was refused" assertion while being exactly the wrong answer.
func assertExternalRegistrationRefusal(t *testing.T, resp *http.Response, body envelope, what string) {
	t.Helper()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("%s status=%d error=%+v; want 409", what, resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_IS_EXTERNAL_REGISTRATION" {
		t.Fatalf("%s error=%+v; want EVENT_IS_EXTERNAL_REGISTRATION", what, body.Error)
	}
	if body.Data != nil && string(body.Data) != "null" {
		t.Fatalf("%s carried data=%s; a refusal carries none", what, string(body.Data))
	}
}

// ticketSaleCount counts every Ticket Sale on an Event, whatever its status —
// the assertion that a refusal wrote nothing at all.
func ticketSaleCount(t *testing.T, env *testEnv, eventID string) int {
	t.Helper()
	var n int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM ticket_sales WHERE event_id = $1`, eventID).Scan(&n); err != nil {
		t.Fatalf("count ticket sales: %v", err)
	}
	return n
}

func TestDirectSaleImportRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports",
		importPayload("direct", "external-direct-1", "00000000-0000-0000-0000-000000000000"),
		authHeader(sessionID))
	assertExternalRegistrationRefusal(t, resp, body, "direct Sale Import")

	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Fatalf("ticket sales after a refused import = %d; want 0", n)
	}
}

// TestExternalPlatformSaleImportRefusedOnAnExternallyRegisteredEvent is the
// ordering test. `external_platform` is not a Sales Source this platform commits
// yet, so a ticketed Event answers it with a VALIDATION_FAILED naming the
// source field — and that answer would be a lie here, because fixing the source
// would not make this import possible. The Event's mode is read before the body
// is judged, so the caller is told the thing that is actually true.
func TestExternalPlatformSaleImportRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports",
		importPayload("external_platform", "external-platform-1", "00000000-0000-0000-0000-000000000000"),
		authHeader(sessionID))
	assertExternalRegistrationRefusal(t, resp, body, "external_platform Sale Import")

	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Fatalf("ticket sales after a refused import = %d; want 0", n)
	}
}

func TestSaleImportFileRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(externalImportCSV),
		map[string]string{"idempotency_key": "external-file-1", "source": "direct"},
		authHeader(sessionID))
	assertExternalRegistrationRefusal(t, resp, body, "file Sale Import")

	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Fatalf("ticket sales after a refused file import = %d; want 0", n)
	}
}

// TestSaleImportPreviewRefusedOnAnExternallyRegisteredEvent covers the step
// before the commit. The preview writes nothing, so nothing is at stake but the
// sentence: without the guard it would hand back a table in which every row is
// flagged for an unmatched Ticket Type, which is the exact wild goose chase the
// dedicated code exists to prevent.
func TestSaleImportPreviewRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(externalImportCSV), nil, authHeader(sessionID))
	assertExternalRegistrationRefusal(t, resp, body, "Sale Import preview")
}

// TestSaleImportTemplateRefusedOnAnExternallyRegisteredEvent covers the step
// before that one: the template is the file the organizer fills in, and its
// sheet is built from the Event's Ticket Types. On an external Event it would
// be a header row and nothing to choose from.
func TestSaleImportTemplateRefusedOnAnExternallyRegisteredEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	resp, body := env.get(t, "/api/v1/staff/events/"+eventID+"/sale-imports/template", authHeader(sessionID))
	assertExternalRegistrationRefusal(t, resp, body, "Sale Import template")
}

// TestTicketedEventStillSellsThroughEverySaleImportForm is the control: the
// guard reads the Event's mode and nothing else, so an ordinary ticketed Event
// is untouched on all four surfaces — including keeping the old, correct
// VALIDATION_FAILED answer for the Sales Source that is not committable yet.
func TestTicketedEventStillSellsThroughEverySaleImportForm(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Ticketed Fest", "ticketed-fest")
	ttID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 50)

	// The template's success body is a spreadsheet rather than an envelope, so
	// its status is read from a raw request.
	req, err := http.NewRequest(http.MethodGet, env.server.URL+"/api/v1/staff/events/"+eventID+"/sale-imports/template", nil)
	if err != nil {
		t.Fatalf("new template request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sessionID)
	templateResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("template request: %v", err)
	}
	_ = templateResp.Body.Close()
	if templateResp.StatusCode != http.StatusOK {
		t.Fatalf("template on a ticketed event status=%d; want 200", templateResp.StatusCode)
	}

	resp, body := postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports/preview",
		"sales.csv", []byte(externalImportCSV), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview on a ticketed event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports",
		importPayload("direct", "ticketed-direct-1", ttID), authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("direct import on a ticketed event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if res := importResult(t, body); res.SaleCount != 1 || res.Status != "committed" {
		t.Fatalf("direct import result = %+v; want one committed sale", res)
	}

	resp, body = postFile(t, env, "/api/v1/staff/events/"+eventID+"/sale-imports",
		"sales.csv", []byte(externalImportCSV),
		map[string]string{"idempotency_key": "ticketed-file-1", "source": "direct"},
		authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("file import on a ticketed event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	if got := soldCount(t, env, sessionID, eventID, ttID); got != 4 {
		t.Fatalf("sold_count = %d; want 4 (two sales of two)", got)
	}

	// The unsupported Sales Source still gets the answer it always got: this is
	// a body the caller could fix, and saying so remains correct on an Event that
	// does sell tickets.
	resp, body = env.post(t, "/api/v1/staff/events/"+eventID+"/sale-imports",
		importPayload("external_platform", "ticketed-platform-1", ttID), authHeader(sessionID))
	if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("external_platform on a ticketed event status=%d error=%+v; want a validation refusal", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("external_platform on a ticketed event error=%+v; want VALIDATION_FAILED", body.Error)
	}
}

// TestOnlineCheckoutOnAnExternallyRegisteredEventHasNothingToBuy verifies the
// issue's claim that online checkout needs no change, rather than assuming it.
//
// The Storefront cannot form a cart for this Event: it is published with zero
// Ticket Types, so there is no line to send. A caller that invents one is the
// only way to reach the endpoint at all, and it is refused with the not-found
// its own invented id earns — which is the honest answer here, unlike on the
// staff and partner paths, because the id came from the caller rather than from
// anything the platform ever published. Nothing is recorded either way.
func TestOnlineCheckoutOnAnExternallyRegisteredEventHasNothingToBuy(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	// An empty cart: the shape the Storefront would necessarily send, since the
	// Event offers nothing to add.
	resp, body := beginCheckout(t, env, "test-org", "external-event",
		checkoutBody("ana@example.com", "Ana", "Lopez"))
	if resp.StatusCode < 400 {
		t.Fatalf("empty-cart checkout status=%d; want a refusal", resp.StatusCode)
	}

	// An invented Ticket Type: the only way to send a non-empty cart.
	resp, body = beginCheckout(t, env, "test-org", "external-event",
		checkoutBody("ana@example.com", "Ana", "Lopez",
			map[string]any{"ticket_type_id": "00000000-0000-0000-0000-000000000000", "quantity": 1}))
	if resp.StatusCode < 400 {
		t.Fatalf("invented-cart checkout status=%d error=%+v; want a refusal", resp.StatusCode, body.Error)
	}
	if body.Error == nil {
		t.Fatalf("invented-cart checkout carried no error envelope")
	}

	if n := ticketSaleCount(t, env, eventID); n != 0 {
		t.Fatalf("ticket sales on an external event = %d; want 0", n)
	}
	var payments int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM payments WHERE event_id = $1`, eventID).Scan(&payments); err != nil {
		t.Fatalf("count payments: %v", err)
	}
	if payments != 0 {
		t.Fatalf("payments on an external event = %d; want 0", payments)
	}
}

// TestExternalEventIsInertOnEveryMoneySurface is the consequence the issue asks
// to be verified rather than implemented: with no Ticket Sale there is no
// Payment, no Platform Fee, no Fee IVA and no Net Proceeds, so the Event never
// enters the Withdrawable Balance, the Payable Balance or a Payout. The zero is
// genuine rather than suppressed, and no payout machinery knows the mode exists.
func TestExternalEventIsInertOnEveryMoneySurface(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	publishExternalRegistrationEvent(t, env, sessionID, "https://lu.ma/my-meetup")

	assertBalances(t, env, sessionID, 0, 0, "an Organization whose only Event registers externally")

	// And so there is nothing to ask for: the Payout Request is refused by the
	// ordinary balance rule, with no special case for the mode.
	resp, body := submitPayoutRequest(t, env, sessionID, requestBody(1000, ""))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("payout request status=%d error=%+v; want 409", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE" {
		t.Fatalf("payout request error=%+v; want PAYOUT_REQUEST_EXCEEDS_PAYABLE_BALANCE", body.Error)
	}
}
