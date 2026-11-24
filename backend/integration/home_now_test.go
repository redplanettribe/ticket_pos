package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Home now (#859, spec #858, ADR 0081): what stands now across the Active
// Member's Organization. The fixed clock is 2026-07-07 12:00 UTC, which is
// 07:00 on the platform clock (America/Guayaquil, UTC-5), so today began at
// 2026-07-07 05:00 UTC.

const homeNowPath = "/api/v1/staff/organization/home/now"

type homeNextEvent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	StartsAt    time.Time `json:"starts_at"`
	Timezone    string    `json:"timezone"`
	TicketsSold int       `json:"tickets_sold"`
	Ongoing     bool      `json:"ongoing"`
}

type homeNow struct {
	Timezone     string         `json:"timezone"`
	Today        string         `json:"today"`
	Currency     string         `json:"currency"`
	HasEvents    bool           `json:"has_events"`
	ActiveEvents int            `json:"active_events"`
	NextEvent    *homeNextEvent `json:"next_event"`
	TodaySoFar   struct {
		Partial      bool `json:"partial"`
		TicketsSold  int  `json:"tickets_sold"`
		TakingsCents *int `json:"takings_cents"`
	} `json:"today_so_far"`
	PayableBalanceCents *int `json:"payable_balance_cents"`
}

// readHomeNow reads Home now as the session's Active Member.
func readHomeNow(t *testing.T, env *testEnv, sessionID string) homeNow {
	t.Helper()
	resp, body := env.get(t, homeNowPath, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("home now status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var out homeNow
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode home now: %v", err)
	}
	return out
}

// setEventRuntime restages an Event's start and end, which no route moves
// into the past.
func setEventRuntime(t *testing.T, env *testEnv, eventID string, startsAt time.Time, endsAt *time.Time) {
	t.Helper()
	if _, err := env.db.Exec(`UPDATE events SET starts_at = $2, ends_at = $3, timezone = 'America/Guayaquil' WHERE id = $1`,
		eventID, startsAt, endsAt); err != nil {
		t.Fatalf("set event %s runtime: %v", eventID, err)
	}
}

// cancelEvent cancels an Event through the staff route.
func cancelHomeEvent(t *testing.T, env *testEnv, sessionID, eventID string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/cancel", nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// reverseSaleSoldAt reverses the one Ticket Sale of an Event sold at soldAt.
//
// SQL, for reverseSale's reason (sales_summary_test.go): these sales are
// imported with a backdated sold_at, and no endpoint reverses an imported
// sale on its own; Home and Organization Trends are about the status, however
// the sale came to carry it. It finds the sale by its Event and sold_at
// because these tests import their sales as batch rows (importSale) and hold
// no confirmation ref for any of them.
func reverseSaleSoldAt(t *testing.T, env *testEnv, eventID string, soldAt time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE ticket_sales SET status = 'reversed' WHERE event_id = $1 AND sold_at = $2`, eventID, soldAt)
	if err != nil {
		t.Fatalf("reverse sale: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("reversing the sale sold at %s affected %d rows, want 1", soldAt, n)
	}
}

// TestHomeNowPicksAnOngoingEventOverALaterOne: an Event running now is the
// next Event, ahead of one still to start, and neither a draft nor a
// cancelled Event is ever next, however early they start. Active Events
// counts the draft and leaves out the cancelled and the over.
func TestHomeNowPicksAnOngoingEventOverALaterOne(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	later, _ := publishCheckoutEvent(t, env, admin, "Later Fest", "later-fest", feeTestBaseCents, 100)

	ongoing, ongoingGA := publishCheckoutEvent(t, env, admin, "Running Fest", "running-fest", feeTestBaseCents, 100)
	setEventRuntime(t, env, ongoing, time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC), new(time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)))
	commitBatch(t, env, admin, ongoing, "running", []map[string]any{
		importSale("ana@example.com", ongoingGA, 3, "2026-07-01T15:00:00Z"),
		importSale("bea@example.com", ongoingGA, 4, "2026-07-02T15:00:00Z"),
	})
	reverseSaleSoldAt(t, env, ongoing, time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC))

	// A draft that started even earlier and runs on.
	draft := createDraftEvent(t, env, admin, "Draft Fest", "draft-fest")
	setEventRuntime(t, env, draft, time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC), new(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)))

	// A cancelled Event as early, also running.
	cancelled, _ := publishCheckoutEvent(t, env, admin, "Called Off", "called-off", feeTestBaseCents, 100)
	setEventRuntime(t, env, cancelled, time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC), new(time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)))
	cancelHomeEvent(t, env, admin, cancelled)

	// A published Event that is over: started with no end.
	over, _ := publishCheckoutEvent(t, env, admin, "Over Fest", "over-fest", feeTestBaseCents, 100)
	setEventRuntime(t, env, over, time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC), nil)

	got := readHomeNow(t, env, admin)
	if !got.HasEvents {
		t.Fatalf("has_events = false, want true")
	}
	if got.ActiveEvents != 3 {
		t.Fatalf("active_events = %d, want 3 (later, running, draft)", got.ActiveEvents)
	}
	if got.NextEvent == nil || got.NextEvent.ID != ongoing {
		t.Fatalf("next_event = %+v, want the running Event %s", got.NextEvent, ongoing)
	}
	if !got.NextEvent.Ongoing || got.NextEvent.Name != "Running Fest" || got.NextEvent.Timezone != "America/Guayaquil" {
		t.Fatalf("next_event = %+v, want Running Fest, ongoing, in America/Guayaquil", got.NextEvent)
	}
	if !got.NextEvent.StartsAt.Equal(time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_event.starts_at = %s", got.NextEvent.StartsAt)
	}
	if got.NextEvent.TicketsSold != 3 {
		t.Fatalf("next_event.tickets_sold = %d, want 3: the reversed sale is left out", got.NextEvent.TicketsSold)
	}

	// Once the running Event is cancelled, the later one is next, and not
	// running.
	cancelHomeEvent(t, env, admin, ongoing)
	got = readHomeNow(t, env, admin)
	if got.NextEvent == nil || got.NextEvent.ID != later || got.NextEvent.Ongoing {
		t.Fatalf("next_event = %+v, want the later Event %s, not ongoing", got.NextEvent, later)
	}
	if got.ActiveEvents != 2 {
		t.Fatalf("active_events = %d, want 2 (later, draft)", got.ActiveEvents)
	}
}

// TestHomeNowCountsTodaysSalesOnThePlatformClock: today's Tickets Sold and
// Takings count the sales made today on the platform clock, by the instant
// they were made, and leave out a reversed sale. A sale made late yesterday
// evening in Ecuador, though already today in UTC, is not today's.
func TestHomeNowCountsTodaysSalesOnThePlatformClock(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	_, onlineGA := publishCheckoutEvent(t, env, admin, "Online Fest", "online-fest", feeTestBaseCents, 100)
	// Sold now, at the fixed clock, by checkout: today.
	begun := beginCheckoutOK(t, env, testOrgSlug, "online-fest", checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(onlineGA, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	importEvent := createDraftEvent(t, env, admin, "Import Fest", "import-fest")
	importGA := createTicketTypeWithCapacity(t, env, admin, importEvent, "GA", importedPriceCents, 100)
	commitBatch(t, env, admin, importEvent, "history", []map[string]any{
		// 01:00 today in Ecuador.
		importSale("bea@example.com", importGA, 2, "2026-07-07T06:00:00Z"),
		// 23:30 yesterday in Ecuador, already today in UTC.
		importSale("cris@example.com", importGA, 3, "2026-07-07T04:30:00Z"),
		// Today, and reversed.
		importSale("dani@example.com", importGA, 4, "2026-07-07T10:00:00Z"),
		// A week ago, imported today.
		importSale("eli@example.com", importGA, 5, "2026-07-01T15:00:00Z"),
	})
	reverseSaleSoldAt(t, env, importEvent, time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC))

	got := readHomeNow(t, env, admin)
	if got.Today != "2026-07-07" || got.Timezone != "America/Guayaquil" || got.Currency != "USD" {
		t.Fatalf("today=%q timezone=%q currency=%q", got.Today, got.Timezone, got.Currency)
	}
	if !got.TodaySoFar.Partial {
		t.Fatalf("today_so_far.partial = false, want true")
	}
	if got.TodaySoFar.TicketsSold != 3 {
		t.Fatalf("today_so_far.tickets_sold = %d, want 3 (1 online + 2 imported)", got.TodaySoFar.TicketsSold)
	}
	if got.TodaySoFar.TakingsCents == nil {
		t.Fatalf("today_so_far.takings_cents is absent for an Org Admin")
	}
	if want := feeTestBaseCents + 2*importedPriceCents; *got.TodaySoFar.TakingsCents != want {
		t.Fatalf("today_so_far.takings_cents = %d, want %d", *got.TodaySoFar.TakingsCents, want)
	}
}

// TestHomeNowWithholdsMoneyByRole: Event Staff are handed no Takings, and
// only an Org Admin the Payable Balance, which is the figure Payouts states.
// Every count reaches every role.
func TestHomeNowWithholdsMoneyByRole(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")

	_, ga := publishCheckoutEvent(t, env, admin, "Money Fest", "money-fest", feeTestBaseCents, 100)
	// Yesterday's sale, recorded yesterday, has cleared into the Payable
	// Balance; today's has not.
	ref := buyOnlineOn(t, env, testOrgSlug, "money-fest", ga, "ana@example.com", 2, time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC))
	if _, err := env.db.Exec(`UPDATE ticket_sales SET created_at = sold_at WHERE confirmation_ref = $1`, ref); err != nil {
		t.Fatalf("move the sale's recording to yesterday: %v", err)
	}
	begun := beginCheckoutOK(t, env, testOrgSlug, "money-fest", checkoutBody("bea@example.com", "Bea", "Ng", cartLine(ga, 1)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	resp, body := env.get(t, "/api/v1/staff/organization/payouts", authHeader(admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("payouts status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var payouts struct {
		PayableBalanceCents int `json:"payable_balance_cents"`
	}
	if err := json.Unmarshal(body.Data, &payouts); err != nil {
		t.Fatalf("decode payouts: %v", err)
	}
	if payouts.PayableBalanceCents != 2*feeTestBaseCents {
		t.Fatalf("payouts payable = %d, want %d", payouts.PayableBalanceCents, 2*feeTestBaseCents)
	}

	asAdmin := readHomeNow(t, env, admin)
	if asAdmin.TodaySoFar.TakingsCents == nil || *asAdmin.TodaySoFar.TakingsCents != feeTestBaseCents {
		t.Fatalf("admin takings = %v, want %d", intOrNil(asAdmin.TodaySoFar.TakingsCents), feeTestBaseCents)
	}
	if asAdmin.PayableBalanceCents == nil || *asAdmin.PayableBalanceCents != payouts.PayableBalanceCents {
		t.Fatalf("admin payable = %v, want %d as Payouts states it", intOrNil(asAdmin.PayableBalanceCents), payouts.PayableBalanceCents)
	}

	asOwner := readHomeNow(t, env, owner)
	if asOwner.TodaySoFar.TakingsCents == nil || *asOwner.TodaySoFar.TakingsCents != feeTestBaseCents {
		t.Fatalf("owner takings = %v, want %d", intOrNil(asOwner.TodaySoFar.TakingsCents), feeTestBaseCents)
	}
	if asOwner.PayableBalanceCents != nil {
		t.Fatalf("owner payable = %d, want absent", *asOwner.PayableBalanceCents)
	}

	// Event Staff: read the raw payload, so absent is told from zero.
	resp, body = env.get(t, homeNowPath, authHeader(staff))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("staff home now status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var raw struct {
		TodaySoFar          map[string]json.RawMessage `json:"today_so_far"`
		PayableBalanceCents json.RawMessage            `json:"payable_balance_cents"`
	}
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode staff home now: %v", err)
	}
	if takings, ok := raw.TodaySoFar["takings_cents"]; ok && string(takings) != "null" {
		t.Fatalf("staff takings_cents = %s, want absent", takings)
	}
	if len(raw.PayableBalanceCents) > 0 && string(raw.PayableBalanceCents) != "null" {
		t.Fatalf("staff payable_balance_cents = %s, want absent", raw.PayableBalanceCents)
	}
	asStaff := readHomeNow(t, env, staff)
	if asStaff.TodaySoFar.TicketsSold != 1 || asStaff.ActiveEvents != 1 || asStaff.NextEvent == nil || asStaff.NextEvent.TicketsSold != 3 {
		t.Fatalf("staff counts = %+v, want every count: 1 sold today, 1 Active Event, next with 3 sold", asStaff)
	}
}

// TestHomeNowReadsOnlyTheMembersOrganization: another Organization's Events
// and sales never reach the reading.
func TestHomeNowReadsOnlyTheMembersOrganization(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)

	euro, euroGA := euroOrganization(t, env)
	begun := beginCheckoutOK(t, env, "euro-org", "euro-fest", checkoutBody("ana@example.com", "Ana", "Lopez", cartLine(euroGA, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")

	got := readHomeNow(t, env, admin)
	if got.HasEvents || got.ActiveEvents != 0 || got.NextEvent != nil {
		t.Fatalf("home now = %+v, want no Events: the other Organization's never appear", got)
	}
	if got.TodaySoFar.TicketsSold != 0 || got.TodaySoFar.TakingsCents == nil || *got.TodaySoFar.TakingsCents != 0 {
		t.Fatalf("today_so_far = %+v, want nothing sold", got.TodaySoFar)
	}
	if got.Currency != "USD" || got.PayableBalanceCents == nil || *got.PayableBalanceCents != 0 {
		t.Fatalf("currency=%q payable=%v, want USD and 0", got.Currency, intOrNil(got.PayableBalanceCents))
	}

	theirs := readHomeNow(t, env, euro)
	if !theirs.HasEvents || theirs.ActiveEvents != 1 || theirs.TodaySoFar.TicketsSold != 2 || theirs.Currency != "EUR" {
		t.Fatalf("the other Organization's own reading = %+v", theirs)
	}
}
