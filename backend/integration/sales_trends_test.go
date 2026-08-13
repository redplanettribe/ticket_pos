package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Sales Trends (issue #275, spec #274, ADR 0040): the Event's selling life as a
// contiguous run of days, each carrying per Ticket Type how many tickets sold
// and what they earned the Organization.
//
// The money figure is TAKINGS, not Net Proceeds. It is the same single-sourced
// per-line expression the Sales summary sums (sales.LineNetProceedsSQL) with NO
// channel filter: in-person and imported lines carry zero fee snapshots, so the
// expression reduces to the full price on those channels, which is what the
// Event made there. The absence of the filter is the load-bearing decision of
// ADR 0040 and TestSalesTrendsSumsTakingsAcrossSalesChannels is the test that
// fails the day somebody puts one back.

// salesTrends mirrors GET /api/v1/staff/events/{id}/sales/trends.
type salesTrends struct {
	Timezone      string             `json:"timezone"`
	Currency      string             `json:"currency"`
	TicketTypes   []trendsTicketType `json:"ticket_types"`
	Days          []trendsDay        `json:"days"`
	ReversedCount int                `json:"reversed_count"`
}

type trendsTicketType struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

type trendsDay struct {
	Date  string       `json:"date"`
	Lines []trendsLine `json:"lines"`
}

type trendsLine struct {
	TicketTypeID string `json:"ticket_type_id"`
	Quantity     int    `json:"quantity"`
	TakingsCents int    `json:"takings_cents"`
}

func getSalesTrends(t *testing.T, env *testEnv, sessionID, eventID string) (*http.Response, envelope) {
	t.Helper()
	return env.get(t, "/api/v1/staff/events/"+eventID+"/sales/trends", authHeader(sessionID))
}

// salesTrendsOK reads the surface under a session that is allowed it, asserting
// the house envelope on the way through: 200, a null error, and a request id.
func salesTrendsOK(t *testing.T, env *testEnv, sessionID, eventID string) salesTrends {
	t.Helper()
	resp, body := getSalesTrends(t, env, sessionID, eventID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sales trends status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("sales trends error = %+v, want null on success", body.Error)
	}
	if body.RequestID == "" {
		t.Fatal("sales trends carries no request_id")
	}
	var out salesTrends
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sales trends: %v", err)
	}
	return out
}

// dayOn returns the day bucket for a date, failing the test when the span does
// not carry it — a missing day is the zero-fill bug, and it should be reported
// as such rather than as an index panic.
func dayOn(t *testing.T, trends salesTrends, date string) trendsDay {
	t.Helper()
	for _, day := range trends.Days {
		if day.Date == date {
			return day
		}
	}
	t.Fatalf("day %s is absent from the span %s..%s (%d days)",
		date, firstDate(trends), lastDate(trends), len(trends.Days))
	return trendsDay{}
}

// lineFor returns a day's line for one Ticket Type, and whether the day carries
// one at all — a Ticket Type that sold nothing that day is omitted, never sent
// as a zero.
func lineFor(day trendsDay, ticketTypeID string) (trendsLine, bool) {
	for _, line := range day.Lines {
		if line.TicketTypeID == ticketTypeID {
			return line, true
		}
	}
	return trendsLine{}, false
}

func firstDate(trends salesTrends) string {
	if len(trends.Days) == 0 {
		return "(none)"
	}
	return trends.Days[0].Date
}

func lastDate(trends salesTrends) string {
	if len(trends.Days) == 0 {
		return "(none)"
	}
	return trends.Days[len(trends.Days)-1].Date
}

// assertContiguousDays proves the span is a run of calendar days with nothing
// skipped: a quiet fortnight must read as a quiet fortnight rather than as two
// adjacent bars.
func assertContiguousDays(t *testing.T, trends salesTrends) {
	t.Helper()
	for i, day := range trends.Days {
		if _, err := time.Parse("2006-01-02", day.Date); err != nil {
			t.Fatalf("day %d date %q is not a calendar date: %v", i, day.Date, err)
		}
		if day.Lines == nil {
			t.Fatalf("day %s carries a null lines array; a silent day is an empty one", day.Date)
		}
		if i == 0 {
			continue
		}
		previous, _ := time.Parse("2006-01-02", trends.Days[i-1].Date)
		if want := previous.AddDate(0, 0, 1).Format("2006-01-02"); day.Date != want {
			t.Fatalf("day %d is %s, want %s — the span skipped a day", i, day.Date, want)
		}
	}
}

// setEventSchedule stamps an Event's schedule directly. The staff PATCH refuses
// to place an Event in the past, and an Event that has already finished is
// exactly what the span's upper bound is about, so SQL is the only way to stage
// it.
func setEventSchedule(t *testing.T, env *testEnv, eventID string, startsAt, endsAt time.Time) {
	t.Helper()
	if _, err := env.db.Exec(
		`UPDATE events SET starts_at = $1, ends_at = $2 WHERE id = $3`,
		startsAt, endsAt, eventID,
	); err != nil {
		t.Fatalf("set event schedule: %v", err)
	}
}

// TestSalesTrendsSumsTakingsAcrossSalesChannels is the load-bearing test of
// ADR 0040: one Event, one day, one Ticket Type, sold twice over — once online
// (netted of the Platform Fee and its Fee IVA) and once as an imported Direct
// Sale the platform took no cut of. The day's takings_cents is the sum of both.
//
// If somebody adds `FILTER (WHERE ts.channel = 'online')` to the Trends query —
// the filter the Sales summary strip correctly carries — this test fails with
// the imported half missing, which is the whole point of writing it.
func TestSalesTrendsSumsTakingsAcrossSalesChannels(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Takings Fest", "takings-fest", feeTestBaseCents, 50)

	// Two tickets online under pass_on: the buyer paid base + fee + Fee IVA, and
	// the line nets back to the price the Organization set.
	begin := beginCheckoutOK(t, env, "test-org", "takings-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	// Four tickets the Organization sold itself and imported, on the same day in
	// the Event's timezone (America/Guayaquil, UTC-5). No fee was withheld, so
	// the Event made the full price.
	commitBatch(t, env, sessionID, eventID, "door-batch", []map[string]any{
		{"customer_email": "caro@example.com", "customer_first_name": "Caro", "customer_last_name": "Diaz",
			"ticket_type_id": gaID, "quantity": 4, "payment_method": "cash", "sold_at": "2026-07-07T11:00:00Z"},
	})

	trends := salesTrendsOK(t, env, sessionID, eventID)
	if trends.Currency != "USD" || trends.Timezone != "America/Guayaquil" {
		t.Fatalf("trends currency/timezone = %q/%q, want USD/America/Guayaquil", trends.Currency, trends.Timezone)
	}

	// The whole Event sold on one day, so the span is that day alone.
	day := dayOn(t, trends, "2026-07-07")
	line, ok := lineFor(day, gaID)
	if !ok {
		t.Fatalf("2026-07-07 carries no GA line: %+v", day)
	}
	wantTakings := 2*feeTestBaseCents + 4*feeTestBaseCents
	if line.TakingsCents != wantTakings {
		t.Fatalf("takings_cents = %d, want %d — the online %d and the imported %d together. "+
			"A channel filter on the Takings expression is what makes this number small (ADR 0040).",
			line.TakingsCents, wantTakings, 2*feeTestBaseCents, 4*feeTestBaseCents)
	}
	if line.Quantity != 6 {
		t.Fatalf("quantity = %d, want 6 across both channels", line.Quantity)
	}

	// And the discrepancy the ADR ships deliberately: the Sales tab's strip nets
	// only the online half. Both figures are right; only their names differ.
	if summary := salesSummaryOK(t, env, sessionID, eventID); summary.NetProceedsCents != 2*feeTestBaseCents {
		t.Fatalf("net_proceeds_cents = %d, want %d — Net Proceeds stays online-only",
			summary.NetProceedsCents, 2*feeTestBaseCents)
	}
}

// TestSalesTrendsAccess: the Event's money takes the Event's money guard. An
// Org Admin and the Event Owner are served; Event Staff hired for the door are
// refused, exactly as they are refused the Net Proceeds strip.
func TestSalesTrendsAccess(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, _ := publishCheckoutEvent(t, env, sessionID, "Access Trends", "access-trends-fest", feeTestBaseCents, 20)

	addMember := func(email, role string) string {
		t.Helper()
		resp, body := env.post(t, "/api/v1/staff/members", map[string]string{
			"email": email,
			"role":  role,
		}, authHeader(sessionID))
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s status=%d error=%+v", role, resp.StatusCode, body.Error)
		}
		return verifyOTP(t, env, email)
	}

	if resp, body := getSalesTrends(t, env, sessionID, eventID); resp.StatusCode != http.StatusOK {
		t.Fatalf("org admin trends status=%d error=%+v", resp.StatusCode, body.Error)
	}

	ownerSessionID := addMember("owner@example.com", "event_owner")
	if resp, body := getSalesTrends(t, env, ownerSessionID, eventID); resp.StatusCode != http.StatusOK {
		t.Fatalf("event owner trends status=%d error=%+v", resp.StatusCode, body.Error)
	}

	staffSessionID := addMember("doorstaff@example.com", "event_staff")
	resp, body := getSalesTrends(t, env, staffSessionID, eventID)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("event staff trends status=%d, want 403; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil {
		t.Fatal("the refusal carries no error object")
	}
	// Their Sales list is untouched by the refusal, as it is by the strip's.
	if listResp, listBody := env.get(t, "/api/v1/staff/events/"+eventID+"/sales", authHeader(staffSessionID)); listResp.StatusCode != http.StatusOK {
		t.Fatalf("event staff sales list status=%d error=%+v", listResp.StatusCode, listBody.Error)
	}

	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	resp, body = getSalesTrends(t, env, otherSessionID, eventID)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("other-org trends status=%d, want 404; error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error == nil || body.Error.Code != "EVENT_NOT_FOUND" {
		t.Fatalf("other-org error = %+v, want EVENT_NOT_FOUND", body.Error)
	}

	if resp, _ := env.get(t, "/api/v1/staff/events/"+eventID+"/sales/trends", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated trends status=%d, want 401", resp.StatusCode)
	}
}

// TestSalesTrendsBucketsDaysInTheEventTimezone: a sale made late on the 2nd in
// Guayaquil is recorded at 02:00 UTC on the 3rd. It belongs to the day it felt
// like locally, matching the Sales list and the Sales Export, which read the
// same Event timezone through the same helper.
func TestSalesTrendsBucketsDaysInTheEventTimezone(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Zone Fest", "zone-trends-fest")
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	// 2026-07-03T02:00Z is 2026-07-02 21:00 in Guayaquil.
	commitBatch(t, env, sessionID, eventID, "late-night", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 3, "payment_method": "cash", "sold_at": "2026-07-03T02:00:00Z"},
	})

	trends := salesTrendsOK(t, env, sessionID, eventID)
	if trends.Timezone != "America/Guayaquil" {
		t.Fatalf("timezone = %q, want America/Guayaquil", trends.Timezone)
	}
	if firstDate(trends) != "2026-07-02" {
		t.Fatalf("the span starts on %s, want 2026-07-02 — the day the sale was made locally", firstDate(trends))
	}
	if line, ok := lineFor(dayOn(t, trends, "2026-07-02"), gaID); !ok || line.Quantity != 3 {
		t.Fatalf("2026-07-02 = %+v, want 3 tickets; the UTC day 2026-07-03 is not the Event's day", line)
	}
	if line, ok := lineFor(dayOn(t, trends, "2026-07-03"), gaID); ok {
		t.Fatalf("2026-07-03 carries %+v; the sale belongs to the previous day in the Event's timezone", line)
	}
}

// TestSalesTrendsFillsSilentDaysAndRunsToToday: the span is every day from the
// first sale to today, silent days included and carrying an empty lines array.
func TestSalesTrendsFillsSilentDaysAndRunsToToday(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Quiet Fest", "quiet-trends-fest", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "spread-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T15:00:00Z"},
		{"customer_email": "bob@example.com", "customer_first_name": "Bob", "customer_last_name": "Ng",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-03T15:00:00Z"},
	})

	trends := salesTrendsOK(t, env, sessionID, eventID)
	assertContiguousDays(t, trends)

	// The Event starts on the 10th and has not finished, so the span stops at
	// today — 2026-07-07 on the fixed clock, in the Event's own timezone.
	if firstDate(trends) != "2026-07-01" || lastDate(trends) != "2026-07-07" {
		t.Fatalf("span = %s..%s, want 2026-07-01..2026-07-07", firstDate(trends), lastDate(trends))
	}
	if len(trends.Days) != 7 {
		t.Fatalf("span carries %d days, want 7", len(trends.Days))
	}
	if quiet := dayOn(t, trends, "2026-07-02"); len(quiet.Lines) != 0 {
		t.Fatalf("the silent day 2026-07-02 carries %+v, want an empty lines array", quiet.Lines)
	}
	if line, ok := lineFor(dayOn(t, trends, "2026-07-01"), gaID); !ok || line.Quantity != 2 || line.TakingsCents != 2000 {
		t.Fatalf("2026-07-01 = %+v, want 2 tickets and 2000 cents", line)
	}
	if line, ok := lineFor(dayOn(t, trends, "2026-07-03"), gaID); !ok || line.Quantity != 1 || line.TakingsCents != 1000 {
		t.Fatalf("2026-07-03 = %+v, want 1 ticket and 1000 cents", line)
	}
}

// TestSalesTrendsStopsAtTheEventsEnd: a finished Event's span ends when the
// Event did, rather than being padded with empty days forever.
func TestSalesTrendsStopsAtTheEventsEnd(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Past Fest", "past-trends-fest")
	setEventTimezone(t, env, eventID, "America/Guayaquil")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 1000, 100)

	// The Event ran on the 3rd, four days before today on the fixed clock.
	setEventSchedule(t, env, eventID,
		time.Date(2026, 7, 3, 22, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 4, 3, 0, 0, 0, time.UTC)) // 2026-07-03 22:00 in Guayaquil

	commitBatch(t, env, sessionID, eventID, "presale-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 1, "payment_method": "cash", "sold_at": "2026-07-01T15:00:00Z"},
	})

	trends := salesTrendsOK(t, env, sessionID, eventID)
	assertContiguousDays(t, trends)
	if firstDate(trends) != "2026-07-01" || lastDate(trends) != "2026-07-03" {
		t.Fatalf("span = %s..%s, want 2026-07-01..2026-07-03 — the Event ended before today",
			firstDate(trends), lastDate(trends))
	}
}

// TestSalesTrendsOnAnEventThatHasSoldNothing: an empty Event answers with an
// empty span and a well-formed envelope, never a 404 and never a null array.
func TestSalesTrendsOnAnEventThatHasSoldNothing(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Silent Fest", "silent-trends-fest", 1000, 100)

	resp, body := getSalesTrends(t, env, sessionID, eventID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty trends status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil || body.RequestID == "" {
		t.Fatalf("empty trends envelope = error %+v, request_id %q", body.Error, body.RequestID)
	}

	// The raw JSON, so that "empty" is an empty array and not a null the chart
	// would have to defend itself against.
	var raw struct {
		Days        []trendsDay        `json:"days"`
		TicketTypes []trendsTicketType `json:"ticket_types"`
	}
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode empty trends: %v", err)
	}
	if raw.Days == nil || len(raw.Days) != 0 {
		t.Fatalf("days = %+v, want an empty array", raw.Days)
	}
	// The catalog is still stated: the legend is the Event's, not the sales'.
	if len(raw.TicketTypes) != 1 || raw.TicketTypes[0].ID != gaID {
		t.Fatalf("ticket_types = %+v, want the Event's one Ticket Type", raw.TicketTypes)
	}
	trends := salesTrendsOK(t, env, sessionID, eventID)
	if trends.ReversedCount != 0 {
		t.Fatalf("reversed_count = %d on an Event with no sales, want 0", trends.ReversedCount)
	}
}

// TestSalesTrendsReadsSoldAtNotRecordedAt: a Sale Import of last year's history
// lands on last year's days. The upload day — today — stays empty, rather than
// carrying one enormous spike.
func TestSalesTrendsReadsSoldAtNotRecordedAt(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Back Catalogue", "backdated-trends-fest", 1000, 500)

	commitBatch(t, env, sessionID, eventID, "history-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 5, "payment_method": "cash", "sold_at": "2026-06-01T15:00:00Z"},
	})

	// Every row was recorded today, whatever day it was sold on.
	var recordedToday int
	if err := env.db.QueryRow(
		`SELECT COUNT(*) FROM ticket_sales WHERE event_id = $1 AND created_at::date = DATE '2026-07-07'`,
		eventID,
	).Scan(&recordedToday); err != nil {
		t.Fatalf("count recorded-today sales: %v", err)
	}
	if recordedToday != 1 {
		t.Fatalf("%d sales were recorded today, want 1 — the backdating must be in sold_at alone", recordedToday)
	}

	trends := salesTrendsOK(t, env, sessionID, eventID)
	assertContiguousDays(t, trends)
	if firstDate(trends) != "2026-06-01" {
		t.Fatalf("the span starts on %s, want 2026-06-01 — the day the sale was made", firstDate(trends))
	}
	if line, ok := lineFor(dayOn(t, trends, "2026-06-01"), gaID); !ok || line.Quantity != 5 {
		t.Fatalf("2026-06-01 = %+v, want the 5 imported tickets", line)
	}
	if upload := dayOn(t, trends, "2026-07-07"); len(upload.Lines) != 0 {
		t.Fatalf("the upload day carries %+v, want nothing — the import is not a spike on the day it was typed", upload.Lines)
	}
}

// TestSalesTrendsExcludesReversedSalesAndStatesTheCount: a reversed sale is
// absent from its day's figures, as it is from every other aggregate, and the
// Event's whole reversed count is stated so the drop is told rather than
// silently shrinking a bar.
func TestSalesTrendsExcludesReversedSalesAndStatesTheCount(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Undone Fest", "undone-trends-fest", 1000, 100)

	commitBatch(t, env, sessionID, eventID, "keeper-batch", []map[string]any{
		{"customer_email": "keep@example.com", "customer_first_name": "Keep", "customer_last_name": "Active",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-01T15:00:00Z"},
	})
	batchID := commitBatch(t, env, sessionID, eventID, "undone-batch", []map[string]any{
		{"customer_email": "gone@example.com", "customer_first_name": "Gone", "customer_last_name": "Reversed",
			"ticket_type_id": gaID, "quantity": 7, "payment_method": "cash", "sold_at": "2026-07-01T16:00:00Z"},
	})
	undoBatch(t, env, sessionID, eventID, batchID)

	trends := salesTrendsOK(t, env, sessionID, eventID)
	line, ok := lineFor(dayOn(t, trends, "2026-07-01"), gaID)
	if !ok {
		t.Fatal("2026-07-01 carries no line; the surviving sale is still there")
	}
	if line.Quantity != 2 || line.TakingsCents != 2000 {
		t.Fatalf("2026-07-01 = %+v, want 2 tickets and 2000 cents — the reversed 7 are out", line)
	}
	if trends.ReversedCount != 1 {
		t.Fatalf("reversed_count = %d, want 1", trends.ReversedCount)
	}
}

// TestSalesTrendsStatesTheCatalogInDisplayOrder: ticket_types is the Event's
// whole catalog in display order — including a Ticket Type nobody has bought,
// so the legend a chart builds from it is stable as sales arrive. A free Ticket
// Type counts toward stock moved and adds nothing to Takings.
func TestSalesTrendsStatesTheCatalogInDisplayOrder(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Catalog Fest", "catalog-trends-fest", 1000, 100)
	compID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "Comp", 0, 100)
	vipID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "VIP", 5000, 100)

	// VIP is dragged to the top of the catalog, so display order is not creation
	// order and the response cannot pass by accident.
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID+"/ticket-types/"+vipID, map[string]any{
		"name":        "VIP",
		"price_cents": 5000,
		"capacity":    100,
		"sort_order":  -1,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reorder VIP status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// GA sells; the free Comp is claimed; VIP sells nothing at all.
	commitBatch(t, env, sessionID, eventID, "ga-batch", []map[string]any{
		{"customer_email": "ana@example.com", "customer_first_name": "Ana", "customer_last_name": "Lopez",
			"ticket_type_id": gaID, "quantity": 2, "payment_method": "cash", "sold_at": "2026-07-07T11:00:00Z"},
	})
	claimFree(t, env, "catalog-trends-fest", compID, "comp@example.com", 3)

	trends := salesTrendsOK(t, env, sessionID, eventID)
	if len(trends.TicketTypes) != 3 {
		t.Fatalf("ticket_types = %+v, want all three including the VIP that sold nothing", trends.TicketTypes)
	}
	gotOrder := []string{}
	for i, tt := range trends.TicketTypes {
		gotOrder = append(gotOrder, tt.Name)
		if i > 0 && tt.SortOrder < trends.TicketTypes[i-1].SortOrder {
			t.Fatalf("ticket_types are not in sort_order: %+v", trends.TicketTypes)
		}
	}
	if gotOrder[0] != "VIP" {
		t.Fatalf("ticket_types order = %v, want VIP first — the catalog's display order, not creation order", gotOrder)
	}

	day := dayOn(t, trends, "2026-07-07")
	if _, ok := lineFor(day, vipID); ok {
		t.Fatalf("the day carries a VIP line; a Ticket Type that sold nothing is omitted, not zeroed: %+v", day.Lines)
	}
	comp, ok := lineFor(day, compID)
	if !ok {
		t.Fatal("the free Ticket Type is missing from the day; comps are stock moved")
	}
	if comp.Quantity != 3 || comp.TakingsCents != 0 {
		t.Fatalf("comp line = %+v, want 3 tickets and no Takings", comp)
	}
	if ga, ok := lineFor(day, gaID); !ok || ga.Quantity != 2 || ga.TakingsCents != 2000 {
		t.Fatalf("GA line = %+v, want 2 tickets and 2000 cents", ga)
	}
}
