package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/migrations"
)

// The Event Recap sweep (spec #884, #891), exercised through the internal
// endpoint with the recording mail sender: what was mailed, to whom, in which
// language and when, never how the sweep queries or what its table holds
// beyond "sent once".
//
// EVERY MOMENT IS ON THE PLATFORM CLOCK and counted from the go-live constant,
// so the cases hold whatever day the Event Recap went live: day 0 is the fifth
// Ecuadorian day after platform.EventRecapWentLiveAt.
//
// Events are set up through the staff API while the clock still stands at the
// harness's fixed moment, months before any of them, and the clock is then
// moved to each run.

// recapSweepResult is the sweep's response envelope, counts only.
type recapSweepResult struct {
	Due        int `json:"due"`
	Recapped   int `json:"recapped"`
	Sent       int `json:"sent"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Unrecorded int `json:"unrecorded"`
}

// recapAt is hour:minute on the platform clock, day days after day 0.
func recapAt(day, hour, minute int) time.Time {
	return platform.StartOfEcuadorDay(platform.EventRecapWentLiveAt.Add(5*24*time.Hour)).
		AddDate(0, 0, day).Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

// sweepEventRecapsAt moves the clock to one moment and hits the sweep as Cloud
// Scheduler would: no body, no parameters, no credential of the application's.
func sweepEventRecapsAt(t *testing.T, env *testEnv, at time.Time) recapSweepResult {
	t.Helper()
	moveClockTo(t, at)
	resp, body := env.post(t, "/api/v1/internal/event-recaps/sweep", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sweep status=%d error=%+v, want 200", resp.StatusCode, body.Error)
	}
	var out recapSweepResult
	if err := json.Unmarshal(body.Data, &out); err != nil {
		t.Fatalf("decode sweep result: %v", err)
	}
	return out
}

// recappedEvents is the names of the Events recapped so far, sorted, one entry
// per mail.
func recappedEvents() []string {
	names := []string{}
	for _, recap := range sharedEmail.EventRecapsSent() {
		names = append(names, recap.EventName)
	}
	sort.Strings(names)
	return names
}

// wantRecapped fails unless exactly these Events' recaps have been mailed so
// far, one mail each.
func wantRecapped(t *testing.T, when string, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := recappedEvents(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s: recapped %q, want %q", when, got, want)
	}
}

// recapFixture is one Organization with one Event Owner, whom every Event the
// fixture makes is assigned to.
type recapFixture struct {
	admin   string
	ownerID string
}

func newRecapFixture(t *testing.T, env *testEnv) recapFixture {
	t.Helper()
	f := recapFixture{admin: orgAdminSession(t, env)}
	f.ownerID, _ = addRecapMember(t, env, f.admin, "owner@example.com", "event_owner")
	return f
}

// addRecapMember adds a Member and signs them in, returning their id and
// their staff session.
func addRecapMember(t *testing.T, env *testEnv, admin, email, role string) (memberID, session string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/members", map[string]string{"email": email, "role": role}, authHeader(admin))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add %s member status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
	var member struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	return member.ID, verifyOTP(t, env, email)
}

// assignToEvent gives a Member an Event assignment with the given role.
func assignToEvent(t *testing.T, env *testEnv, admin, eventID, memberID, role string) {
	t.Helper()
	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/assignments/"+memberID,
		map[string]string{"role": role}, authHeader(admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("assign %s status=%d error=%+v", role, resp.StatusCode, body.Error)
	}
}

// recapSlug is a slug for an Event's name.
func recapSlug(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, " ", "-"))
}

// setEventDates writes an Event's dates through the staff API, which is how
// an Event is dated and how it is rescheduled. A nil end is an Event without
// one.
func setEventDates(t *testing.T, env *testEnv, admin, eventID, name string, startsAt time.Time, endsAt *time.Time) {
	t.Helper()
	form := map[string]any{
		"name":       name,
		"slug":       recapSlug(name),
		"starts_at":  startsAt.Format(time.RFC3339),
		"timezone":   "America/Guayaquil",
		"venue_name": "The Hall",
	}
	if endsAt != nil {
		form["ends_at"] = endsAt.Format(time.RFC3339)
	}
	resp, body := env.patch(t, "/api/v1/staff/events/"+eventID, form, authHeader(admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set dates of %s status=%d error=%+v", name, resp.StatusCode, body.Error)
	}
}

// draftEvent makes a draft with a Ticket Type and the fixture's Event Owner,
// dated when startsAt is not zero.
func (f recapFixture) draftEvent(t *testing.T, env *testEnv, name string, startsAt time.Time, endsAt *time.Time) string {
	t.Helper()
	eventID := createDraftEvent(t, env, f.admin, name, recapSlug(name))
	if !startsAt.IsZero() {
		setEventDates(t, env, f.admin, eventID, name, startsAt, endsAt)
	}
	createTicketType(t, env, f.admin, eventID)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	return eventID
}

// publishedEvent makes a published Event over at overAt: it ends then, having
// started three hours before.
func (f recapFixture) publishedEvent(t *testing.T, env *testEnv, name string, overAt time.Time) string {
	t.Helper()
	eventID := f.draftEvent(t, env, name, overAt.Add(-3*time.Hour), &overAt)
	publishRecapEvent(t, env, f.admin, eventID)
	return eventID
}

func publishRecapEvent(t *testing.T, env *testEnv, admin, eventID string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/publish", nil, authHeader(admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish event status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// The morning after: an Event over late in the evening, or six hours before
// 09:00, is recapped that morning; one over inside the six hours before it
// waits for the next; and an Event without an end is timed from its start.
func TestEventRecapGoesAtTheFirstNineSixHoursAfterTheEventIsOver(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Late Night", recapAt(0, 23, 0))
	f.publishedEvent(t, env, "Small Hours", recapAt(1, 3, 0))
	f.publishedEvent(t, env, "Dawn", recapAt(1, 5, 0))
	f.publishedEvent(t, env, "Morning", recapAt(1, 8, 50))
	// No end: over when it starts, at 22:00, although it started after Late
	// Night did.
	noEnd := f.draftEvent(t, env, "No End", recapAt(0, 22, 0), nil)
	publishRecapEvent(t, env, f.admin, noEnd)
	// No end either, and its start is inside the six hours.
	lateStart := f.draftEvent(t, env, "Late Start", recapAt(1, 4, 0), nil)
	publishRecapEvent(t, env, f.admin, lateStart)

	sweepEventRecapsAt(t, env, recapAt(1, 8, 59))
	wantRecapped(t, "a minute before 09:00")

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapped(t, "at 09:00 the morning after", "Late Night", "No End", "Small Hours")

	// A run made by hand that afternoon is still the morning's run: the
	// Events that had not settled by 09:00 wait for tomorrow's.
	sweepEventRecapsAt(t, env, recapAt(1, 16, 0))
	wantRecapped(t, "that afternoon", "Late Night", "No End", "Small Hours")

	sweepEventRecapsAt(t, env, recapAt(2, 9, 0))
	wantRecapped(t, "at 09:00 the day after", "Dawn", "Late Night", "Late Start", "Morning", "No End", "Small Hours")
}

// Never a draft, never a cancelled Event, never a dateless one, and never an
// Event that was over before the Event Recap went live. An Event that sold
// nothing is recapped all the same.
func TestEventRecapIsOnlyForAPublishedEventOverSinceGoLive(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)

	over := recapAt(0, 23, 0)
	f.publishedEvent(t, env, "Sold Nothing", over)

	started := over.Add(-3 * time.Hour)
	f.draftEvent(t, env, "Draft In The Past", started, &over)
	f.draftEvent(t, env, "Dateless Draft", time.Time{}, nil)

	cancelled := f.publishedEvent(t, env, "Cancelled", over)
	resp, body := env.post(t, "/api/v1/staff/events/"+cancelled+"/cancel", nil, authHeader(f.admin))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel event status=%d error=%+v", resp.StatusCode, body.Error)
	}

	// A published Event cannot lose its dates through the staff API, so the
	// row is written directly: the rule must hold for it all the same.
	dateless := f.publishedEvent(t, env, "Dateless", over)
	if _, err := env.db.Exec(`UPDATE events SET starts_at = NULL, ends_at = NULL WHERE id = $1`, dateless); err != nil {
		t.Fatalf("strip the Event's dates: %v", err)
	}

	f.publishedEvent(t, env, "Before Go Live", platform.EventRecapWentLiveAt.Add(-time.Minute))
	f.publishedEvent(t, env, "At Go Live", platform.EventRecapWentLiveAt)

	// The first run after go-live that the Event over at go-live has settled
	// for, then every morning through the day after the others are over.
	for day := -5; day <= 2; day++ {
		sweepEventRecapsAt(t, env, recapAt(day, 9, 0))
	}
	wantRecapped(t, "after a week of runs", "At Go Live", "Sold Nothing")
}

// One Event Recap per Event, ever: a second sweep sends nothing, whenever it
// runs.
func TestEventRecapIsSentOnce(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))

	if result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0)); result.Due != 1 || result.Recapped != 1 || result.Sent != 1 {
		t.Fatalf("first sweep = %+v, want one Event due, recapped in one mail", result)
	}
	if result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0)); result.Due != 0 || result.Sent != 0 {
		t.Fatalf("second sweep = %+v, want nothing due and nothing sent", result)
	}
	sweepEventRecapsAt(t, env, recapAt(2, 9, 0))
	sweepEventRecapsAt(t, env, recapAt(9, 9, 0))
	wantRecapped(t, "after four sweeps", "Jazz Night")
}

// Rescheduling an Event later before its recap makes the recap wait for the
// new time.
func TestEventRecapWaitsForAnEventRescheduledLaterBeforeItIsSent(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))

	// The evening before it would have been recapped, it is moved a day on.
	moveClockTo(t, recapAt(0, 18, 0))
	newEnd := recapAt(1, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Jazz Night", newEnd.Add(-3*time.Hour), &newEnd)

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapped(t, "the morning after the old date")

	sweepEventRecapsAt(t, env, recapAt(2, 9, 0))
	wantRecapped(t, "the morning after the new date", "Jazz Night")
}

// Rescheduling an Event after its recap never sends a second.
func TestEventRecapIsNotSentAgainForAnEventRescheduledAfterItWasSent(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	wantRecapped(t, "the morning after", "Jazz Night")

	newEnd := recapAt(3, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Jazz Night", newEnd.Add(-3*time.Hour), &newEnd)

	for day := 2; day <= 5; day++ {
		sweepEventRecapsAt(t, env, recapAt(day, 9, 0))
	}
	wantRecapped(t, "after the corrected date came and went", "Jazz Night")
}

// A morning the scheduler missed delays the recap and does not lose it: the
// next run sends it.
func TestEventRecapMissedOneMorningIsSentByTheNextRun(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))

	// No run on day 1.
	sweepEventRecapsAt(t, env, recapAt(2, 9, 0))
	wantRecapped(t, "the run after the missed one", "Jazz Night")
}

// Every Event Owner of the Event receives it, each in their own Staff Locale,
// English for one who stored none; Event Staff, an Event Owner of another
// Event and the Org Admin do not.
func TestEventRecapGoesToEveryEventOwnerInTheirOwnStaffLocale(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID := f.publishedEvent(t, env, "Jazz Night", recapAt(0, 23, 0))

	duenaID, duena := addRecapMember(t, env, f.admin, "duena@example.com", "event_owner")
	setStaffLocale(t, env, duena, "es")
	assignToEvent(t, env, f.admin, eventID, duenaID, "event_owner")

	englishID, english := addRecapMember(t, env, f.admin, "english@example.com", "event_owner")
	setStaffLocale(t, env, english, "en")
	assignToEvent(t, env, f.admin, eventID, englishID, "event_owner")

	// Event Staff on this Event, by assignment.
	staffID, _ := addRecapMember(t, env, f.admin, "door@example.com", "event_staff")
	assignToEvent(t, env, f.admin, eventID, staffID, "event_staff")
	// An Event Owner by Organization role whose assignment here is Event Staff.
	demotedID, _ := addRecapMember(t, env, f.admin, "demoted@example.com", "event_owner")
	assignToEvent(t, env, f.admin, eventID, demotedID, "event_staff")
	// An Event Owner of another Event only.
	elsewhereID, _ := addRecapMember(t, env, f.admin, "elsewhere@example.com", "event_owner")
	other := f.draftEvent(t, env, "Another Night", recapAt(30, 20, 0), nil)
	assignToEvent(t, env, f.admin, other, elsewhereID, "event_owner")
	sharedEmail.Reset()

	if result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0)); result.Recapped != 1 || result.Sent != 3 {
		t.Fatalf("sweep = %+v, want one Event recapped in three mails", result)
	}

	got := map[string]platform.EventRecap{}
	for _, recap := range sharedEmail.EventRecapsSent() {
		if _, twice := got[recap.To]; twice {
			t.Fatalf("%s was written to twice", recap.To)
		}
		got[recap.To] = recap
	}
	if len(got) != 3 {
		t.Fatalf("recaps went to %d addresses, want the three Event Owners: %v", len(got), got)
	}
	for to, want := range map[string]struct {
		locale  platform.Locale
		subject string
	}{
		"owner@example.com":   {platform.LocaleEN, "Event Recap: Jazz Night"},
		"english@example.com": {platform.LocaleEN, "Event Recap: Jazz Night"},
		"duena@example.com":   {platform.LocaleES, "Resumen del evento: Jazz Night"},
	} {
		recap, ok := got[to]
		if !ok {
			t.Fatalf("%s, an Event Owner of the Event, received no recap", to)
		}
		if recap.Locale != want.locale || recap.Subject() != want.subject {
			t.Errorf("%s got locale %q and subject %q, want %q and %q", to, recap.Locale, recap.Subject(), want.locale, want.subject)
		}
	}
}

// The mail names the Event, says when its figures are as of, and links to the
// Event's Sales Trends, Reach and Sales list in the staff app. It names no
// Customer, although somebody bought.
func TestEventRecapNamesTheEventItsInstantAndItsStaffSurfacesAndNoCustomer(t *testing.T) {
	env := setupTest(t)
	f := newRecapFixture(t, env)
	eventID, ticketTypeID := publishCheckoutEvent(t, env, f.admin, "Jazz Night", "jazz-night", 2000, 50)
	assignToEvent(t, env, f.admin, eventID, f.ownerID, "event_owner")
	begun := beginCheckoutOK(t, env, testOrgSlug, "jazz-night", checkoutBody("ana.buyer@example.com", "Anastasia", "Buyerson", cartLine(ticketTypeID, 2)))
	confirmCheckoutOK(t, env, begun.ClientTransactionID, "approved")
	over := recapAt(0, 23, 0)
	setEventDates(t, env, f.admin, eventID, "Jazz Night", over.Add(-3*time.Hour), &over)
	sharedEmail.Reset()

	sweepEventRecapsAt(t, env, recapAt(1, 9, 0))
	recaps := sharedEmail.EventRecapsSent()
	if len(recaps) != 1 {
		t.Fatalf("got %d recaps, want one", len(recaps))
	}
	recap := recaps[0]
	html, err := recap.HTML(platform.MailChrome{})
	if err != nil {
		t.Fatalf("render the HTML part: %v", err)
	}
	text := recap.Text()

	if recap.Subject() != "Event Recap: Jazz Night" {
		t.Errorf("subject = %q, want it to name the Event", recap.Subject())
	}
	asOf := recapAt(1, 9, 0).In(time.FixedZone("-05", -5*60*60)).Format("2 January 2006 at 15:04") + " (Ecuador time)"
	staffEvent := "http://staff.example/events/" + eventID
	for _, part := range []string{text, html} {
		if !strings.Contains(part, asOf) {
			t.Errorf("the mail does not say its figures are as of %q:\n%s", asOf, part)
		}
		for _, url := range []string{staffEvent + "/sales/trends", staffEvent + "/reach", staffEvent + "/sales"} {
			if !strings.Contains(part, url) {
				t.Errorf("the mail does not link to %s", url)
			}
		}
		for _, customer := range []string{"ana.buyer@example.com", "Anastasia", "Buyerson"} {
			if strings.Contains(part, customer) {
				t.Errorf("the mail names a Customer (%q)", customer)
			}
		}
	}
}

// The new table's down migration undoes it, and the migration applies again
// afterwards.
func TestEventRecapMigrationHasADownMigration(t *testing.T) {
	env := setupTest(t)
	down, err := os.ReadFile("../migrations/down/146_event_recaps.sql")
	if err != nil {
		t.Fatalf("read the down migration: %v", err)
	}
	t.Cleanup(func() {
		// Whatever happened above, leave the schema as every other test
		// expects it: the table there, from the migration itself.
		var table *string
		if err := env.db.QueryRow(`SELECT to_regclass('event_recaps')::text`).Scan(&table); err == nil && table == nil {
			if up, err := migrations.Files.ReadFile("146_event_recaps.sql"); err == nil {
				_, _ = env.db.Exec(string(up))
			}
		}
		_, _ = env.db.Exec(`INSERT INTO schema_migrations (version) VALUES ('146_event_recaps') ON CONFLICT DO NOTHING`)
	})
	if _, err := env.db.Exec(string(down)); err != nil {
		t.Fatalf("apply the down migration: %v", err)
	}
	var table *string
	if err := env.db.QueryRow(`SELECT to_regclass('event_recaps')::text`).Scan(&table); err != nil {
		t.Fatalf("look for the table: %v", err)
	}
	if table != nil {
		t.Fatalf("the down migration left %s behind", *table)
	}
	executeMigration(t, env, "146_event_recaps.sql")
	if _, err := env.db.Exec(`INSERT INTO schema_migrations (version) VALUES ('146_event_recaps')`); err != nil {
		t.Fatalf("the down migration left its schema_migrations row behind: %v", err)
	}
	if result := sweepEventRecapsAt(t, env, recapAt(1, 9, 0)); result.Due != 0 {
		t.Fatalf("sweep after re-applying = %+v, want a working, empty sweep", result)
	}
}
