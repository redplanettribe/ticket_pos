package integration

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

// The Event table of Organization Trends (#865, spec #858, ADR 0081): every
// Event held, loaded or sold in the span, ranked, its rows adding up to the
// span's totals. On the fixed clock of organization_trends_test.go, today is
// 2026-07-07 on the platform clock, so 7d is 2026-06-30 through 2026-07-06.

type orgTrendEvent struct {
	EventID      string     `json:"event_id"`
	Name         string     `json:"name"`
	StartsAt     *time.Time `json:"starts_at"`
	Timezone     string     `json:"timezone"`
	TicketsSold  int        `json:"tickets_sold"`
	TakingsCents *int       `json:"takings_cents"`
	PageViews    int        `json:"page_views"`
}

// readOrganizationTrendEvents reads the Event table of a reading, and the
// reading's figures.
func readOrganizationTrendEvents(t *testing.T, env *testEnv, sessionID, query string) (organizationTrends, []orgTrendEvent) {
	t.Helper()
	reading := readOrganizationTrends(t, env, sessionID, query)
	_, body := env.get(t, organizationTrendsPath+query, authHeader(sessionID))
	var raw struct {
		Events []orgTrendEvent `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode organization trends events: %v", err)
	}
	if raw.Events == nil {
		t.Fatalf("organization trends %s carries no events list", query)
	}
	return reading, raw.Events
}

// eventTableNames is the table's Events by name, in its order.
func eventTableNames(events []orgTrendEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Name)
	}
	return out
}

// eventTableRows is the table as name=tickets/views, in its order.
func eventTableRows(events []orgTrendEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, event.Name+"="+strconv.Itoa(event.TicketsSold)+"/"+strconv.Itoa(event.PageViews))
	}
	return out
}

// seedEventTableOrganization adds, beside the breakdown's three Events, an
// Event held in the span that nothing sold or loaded, an Event only loaded in
// it, an Event that sold today alone, a cancelled Event starting in the span
// and a published Event nothing happened to; only the first two belong in
// the 7d table.
func seedEventTableOrganization(t *testing.T, env *testEnv, admin string) {
	t.Helper()
	seedBreakdownOrganization(t, env, admin)

	held, _ := publishCheckoutEvent(t, env, admin, "Held Fest", "held-fest", feeTestBaseCents, 100)
	setEventStart(t, env, held, time.Date(2026, 7, 4, 20, 0, 0, 0, time.UTC))

	publishCheckoutEvent(t, env, admin, "Viewed Fest", "viewed-fest", feeTestBaseCents, 100)
	recordPageViews(t, env, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC), testOrgSlug, "viewed-fest", "", 2)
	// Today's loads are in no row.
	recordPageViews(t, env, time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC), testOrgSlug, "viewed-fest", "", 7)

	_, lateGA := publishCheckoutEvent(t, env, admin, "Late Fest", "late-fest", feeTestBaseCents, 100)
	buyOnlineOn(t, env, testOrgSlug, "late-fest", lateGA, "hal@example.com", 3, time.Date(2026, 7, 7, 11, 0, 0, 0, time.UTC))

	cancelled, _ := publishCheckoutEvent(t, env, admin, "Called Off", "called-off", feeTestBaseCents, 100)
	postEventOK(t, env, admin, cancelled, "cancel")
	setEventStart(t, env, cancelled, time.Date(2026, 7, 3, 16, 0, 0, 0, time.UTC))

	publishCheckoutEvent(t, env, admin, "Quiet Fest", "quiet-fest", feeTestBaseCents, 100)
}

// assertRowsAddUp checks that the table's rows sum to the reading's span
// totals of Tickets Sold, Page Views and, when handed, Takings.
func assertRowsAddUp(t *testing.T, name string, reading organizationTrends, events []orgTrendEvent) {
	t.Helper()
	tickets, takings, views := 0, 0, 0
	for _, event := range events {
		tickets += event.TicketsSold
		views += event.PageViews
		if event.TakingsCents != nil {
			takings += *event.TakingsCents
		}
	}
	if tickets != reading.Figures.TicketsSold.Total {
		t.Fatalf("%s rows sum to %d Tickets Sold, want the span's %d (%v)", name, tickets, reading.Figures.TicketsSold.Total, eventTableRows(events))
	}
	if views != reading.Figures.PageViews.Total {
		t.Fatalf("%s rows sum to %d Page Views, want the span's %d (%v)", name, views, reading.Figures.PageViews.Total, eventTableRows(events))
	}
	if reading.Figures.Takings != nil && takings != reading.Figures.Takings.Total {
		t.Fatalf("%s rows sum to %d Takings, want the span's %d", name, takings, reading.Figures.Takings.Total)
	}
}

// TestOrganizationTrendsEventTableListsEveryEventHeldLoadedOrSold: the table
// holds every Event held, loaded or sold in the span and no other, ranked by
// Takings for an Org Admin, each with its start, and its rows add up to the
// span's totals.
func TestOrganizationTrendsEventTableListsEveryEventHeldLoadedOrSold(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	seedEventTableOrganization(t, env, admin)

	reading, events := readOrganizationTrendEvents(t, env, admin, "?span=7d")
	// Held, loaded or sold in 2026-06-30..2026-07-06: Late Fest sold today
	// alone, Called Off is cancelled, Quiet Fest did nothing and euro-org's
	// Event is another Organization's.
	want := map[string]bool{"Big Fest": true, "Import Fest": true, "Small Fest": true, "Held Fest": true, "Viewed Fest": true}
	if len(events) != len(want) {
		t.Fatalf("7d table = %v, want exactly %d Events", eventTableRows(events), len(want))
	}
	for _, event := range events {
		if !want[event.Name] {
			t.Fatalf("7d table lists %q: %v", event.Name, eventTableRows(events))
		}
		if event.EventID == "" || event.TakingsCents == nil {
			t.Fatalf("row %+v, want its event_id and the admin's Takings", event)
		}
	}
	for i := 1; i < len(events); i++ {
		if *events[i-1].TakingsCents < *events[i].TakingsCents {
			t.Fatalf("7d table is not ranked by Takings: %v", eventTableRows(events))
		}
	}
	byName := map[string]orgTrendEvent{}
	for _, event := range events {
		byName[event.Name] = event
	}
	if big := byName["Big Fest"]; big.TicketsSold != 5 || big.PageViews != 4 || big.StartsAt == nil ||
		!big.StartsAt.Equal(time.Date(2026, 7, 2, 23, 0, 0, 0, time.UTC)) {
		t.Fatalf("Big Fest row = %+v, want 5 tickets, 4 loads and its start", big)
	}
	if viewed := byName["Viewed Fest"]; viewed.TicketsSold != 0 || viewed.PageViews != 2 || *viewed.TakingsCents != 0 {
		t.Fatalf("Viewed Fest row = %+v, want its 2 loads before today and nothing sold", viewed)
	}
	if held := byName["Held Fest"]; held.TicketsSold != 0 || held.PageViews != 0 {
		t.Fatalf("Held Fest row = %+v, want zeros", held)
	}
	assertRowsAddUp(t, "7d", reading, events)

	// Over 12m, June's sale and loads join their rows, and every row still
	// adds up.
	reading, events = readOrganizationTrendEvents(t, env, admin, "?span=12m")
	byName = map[string]orgTrendEvent{}
	for _, event := range events {
		byName[event.Name] = event
	}
	if byName["Import Fest"].TicketsSold != 10 || byName["Big Fest"].PageViews != 9 {
		t.Fatalf("12m table = %v, want June added in", eventTableRows(events))
	}
	if _, ok := byName["Late Fest"]; ok {
		t.Fatalf("12m table lists Late Fest, which sold today alone: %v", eventTableRows(events))
	}
	assertRowsAddUp(t, "12m", reading, events)

	// A custom range reaching today, cut at yesterday, and all time add up
	// the same way.
	for _, query := range []string{"?span=custom&from=2026-06-11&to=2026-07-07", "?span=all"} {
		reading, events = readOrganizationTrendEvents(t, env, admin, query)
		assertRowsAddUp(t, query, reading, events)
	}
}

// TestOrganizationTrendsEventTableWithholdsTakingsFromEventStaff: an Event
// Owner is handed the admin's table; Event Staff are handed no Takings in any
// row, ranked by Tickets Sold, and the rows still add up to their counts.
func TestOrganizationTrendsEventTableWithholdsTakingsFromEventStaff(t *testing.T) {
	env := setupTest(t)
	admin := orgAdminSession(t, env)
	owner := addMemberSession(t, env, admin, "owner@example.com", "event_owner")
	staff := addMemberSession(t, env, admin, "staff@example.com", "event_staff")
	seedEventTableOrganization(t, env, admin)

	_, adminEvents := readOrganizationTrendEvents(t, env, admin, "?span=7d")
	_, ownerEvents := readOrganizationTrendEvents(t, env, owner, "?span=7d")
	if !equalStrings(eventTableNames(ownerEvents), eventTableNames(adminEvents)) {
		t.Fatalf("owner table = %v, want the admin's %v", eventTableNames(ownerEvents), eventTableNames(adminEvents))
	}
	for i := range ownerEvents {
		if ownerEvents[i].TakingsCents == nil || *ownerEvents[i].TakingsCents != *adminEvents[i].TakingsCents {
			t.Fatalf("owner row %+v, want the admin's Takings", ownerEvents[i])
		}
	}

	reading, events := readOrganizationTrendEvents(t, env, staff, "?span=7d")
	// Tickets Sold first, then Page Views, then name.
	if want := []string{"Big Fest=5/4", "Import Fest=4/0", "Small Fest=1/9", "Viewed Fest=0/2", "Held Fest=0/0"}; !equalStrings(eventTableRows(events), want) {
		t.Fatalf("staff table = %v, want %v", eventTableRows(events), want)
	}
	assertRowsAddUp(t, "staff 7d", reading, events)

	_, body := env.get(t, organizationTrendsPath+"?span=7d", authHeader(staff))
	var raw struct {
		Events []map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode staff events: %v", err)
	}
	for _, row := range raw.Events {
		if takings, ok := row["takings_cents"]; ok && string(takings) != "null" {
			t.Fatalf("staff row carries takings_cents %s: %v", takings, row)
		}
	}
}
