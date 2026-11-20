package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Platform Trends' Events Published (#814, spec #808): each Event counted
// once, on the platform day it first became `published`. It is one series,
// keyed "all", like the other counts, but it has a recording-began day: the
// day migration 137 ran, before which an Event is counted on its creation
// day. The fixed clock is 2026-07-07T12:00Z, so today is 2026-07-07 on the
// platform clock and a 7d span is 2026-06-30 to 2026-07-06, set beside
// 2026-06-23 to 2026-06-29.

const eventsPublishedMigration = "137_events_first_published_at.sql"

// eventsPublishedSeries returns the one series of Events Published, failing
// when the figure is not recorded, states no recording-began day, or is not
// one series.
func eventsPublishedSeries(t *testing.T, trends platformTrends) platformTrendsSeries {
	t.Helper()
	figure := trends.Figures.EventsPublished
	if !figure.Recorded || figure.ExactSince == nil {
		t.Fatalf("events_published recorded=%v exact_since=%v; want recorded with a recording-began day", figure.Recorded, figure.ExactSince)
	}
	if len(figure.Series) != 1 || figure.Series[0].Key != "all" {
		t.Fatalf("events_published series = %+v; want the one series \"all\"", figure.Series)
	}
	return figure.Series[0]
}

// publishingAt moves the catalog clock, which stamps a publication, for the
// length of do, and puts the fixed clock back.
func publishingAt(t *testing.T, env *testEnv, at time.Time, do func()) {
	t.Helper()
	sharedApp.CatalogService.WithClock(func() time.Time { return at })
	defer sharedApp.CatalogService.WithClock(func() time.Time { return env.fixedClock })
	do()
}

// publishEventAt publishes a ticketed Event at an instant.
func publishEventAt(t *testing.T, env *testEnv, sessionID, name, slug string, at time.Time) (eventID string) {
	t.Helper()
	publishingAt(t, env, at, func() {
		eventID, _ = publishCheckoutEvent(t, env, sessionID, name, slug, 1000, 10)
	})
	return eventID
}

func postEventOK(t *testing.T, env *testEnv, sessionID, eventID, action string) {
	t.Helper()
	resp, body := env.post(t, "/api/v1/staff/events/"+eventID+"/"+action, nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s event status=%d error=%+v", action, resp.StatusCode, body.Error)
	}
}

// setEventCreatedAt restages when an Event was created. Creation is stamped
// by the database's own clock and no route backdates it, so the suite moves
// it in SQL, as it moves an Organization's creation.
func setEventCreatedAt(t *testing.T, env *testEnv, eventID string, at time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE events SET created_at = $1 WHERE id = $2`, at, eventID)
	if err != nil {
		t.Fatalf("move creation of Event %s: %v", eventID, err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving the creation of Event %s affected %d rows, want 1", eventID, n)
	}
}

// platformDayNow is today on the platform clock by the wall clock, which is
// the clock a migration's now() reads.
func platformDayNow(t *testing.T) string {
	t.Helper()
	location, err := time.LoadLocation(platform.EcuadorTimeZone)
	if err != nil {
		t.Fatalf("load the platform clock: %v", err)
	}
	return time.Now().In(location).Format("2006-01-02")
}

// TestPlatformTrendsCountsAnEventOnTheDayItWasPublished: an Event counts on
// the platform day it was published and stays there when cancelled; a
// `draft` counts nowhere, however old; an Event with External Registration
// counts like any other; and the span is set beside the one before it.
func TestPlatformTrendsCountsAnEventOnTheDayItWasPublished(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	publishEventAt(t, env, sessionID, "Sunday Fest", "sunday-fest", time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC))
	cancelled := publishEventAt(t, env, sessionID, "Monday Fest", "monday-fest", time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC))
	publishEventAt(t, env, sessionID, "Earlier Fest", "earlier-fest", time.Date(2026, 6, 27, 15, 0, 0, 0, time.UTC))
	publishEventAt(t, env, sessionID, "Today Fest", "today-fest", env.fixedClock)

	// An Event with External Registration, published on 2026-07-06 too.
	resp, body := env.post(t, "/api/v1/staff/events", map[string]any{
		"name": "Meetup", "slug": "meetup",
		"registration_mode": "external", "registration_url": "https://lu.ma/my-meetup",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create external event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var external struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body.Data, &external); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	patchCheckoutEvent(t, env, sessionID, external.ID, "Meetup", "meetup", false)
	publishingAt(t, env, time.Date(2026, 7, 6, 20, 0, 0, 0, time.UTC), func() {
		postEventOK(t, env, sessionID, external.ID, "publish")
	})

	// A draft created inside the span, and never published.
	draft := createDraftEvent(t, env, sessionID, "Old Draft", "old-draft")
	setEventCreatedAt(t, env, draft, time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC))

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	read := func() (platformTrendsFigure, platformTrendsSeries) {
		trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
		return trends.Figures.EventsPublished, eventsPublishedSeries(t, trends)
	}
	assert := func(when string) {
		t.Helper()
		figure, series := read()
		for day, want := range map[string]int{"2026-07-02": 0, "2026-07-05": 1, "2026-07-06": 2} {
			if got := bucketOn(t, series, day).Value; got != want {
				t.Fatalf("%s: %s = %d, want %d", when, day, got, want)
			}
		}
		if got := todayValue(t, series); got != 1 {
			t.Fatalf("%s: today = %d, want the 1 Event published today", when, got)
		}
		// 3 in the span, today's in no total, against 1 in the span before.
		if figure.Total == nil || *figure.Total != 3 || series.Total != 3 {
			t.Fatalf("%s: total = %v (series %d), want 3", when, figure.Total, series.Total)
		}
		if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 1 || series.ComparisonTotal == nil || *series.ComparisonTotal != 1 {
			t.Fatalf("%s: comparison_total = %v (series %v), want 1", when, figure.ComparisonTotal, series.ComparisonTotal)
		}
		// 2026-06-23..2026-06-29 begins before the first publication.
		if figure.Change != nil || series.Change != nil {
			t.Fatalf("%s: change = %v (series %v); want none against a comparison span the data covers in part", when, shown(figure.Change), shown(series.Change))
		}
		assertDataSince(t, "events_published", figure, "2026-06-27")
		if bucket := comparisonOn(t, series, "2026-06-26"); bucket != nil {
			t.Fatalf("%s: comparison bucket on 2026-06-26 = %+v, want it absent before the first publication", when, bucket)
		}
		if bucket := comparisonOn(t, series, "2026-06-27"); bucket == nil || bucket.Value != 1 {
			t.Fatalf("%s: comparison bucket on 2026-06-27 = %+v, want 1", when, bucket)
		}

		// 2026-07-02..2026-07-06 against 2026-06-27..2026-07-01, which
		// begins on the day of the first publication.
		covered := readPlatformTrends(t, env, operatorSessionID, "?span=custom&from=2026-07-02&to=2026-07-06")
		coveredFigure := covered.Figures.EventsPublished
		if coveredFigure.Total == nil || *coveredFigure.Total != 3 || coveredFigure.ComparisonTotal == nil || *coveredFigure.ComparisonTotal != 1 {
			t.Fatalf("%s: covered totals = %v against %v, want 3 against 1", when, shown(coveredFigure.Total), shown(coveredFigure.ComparisonTotal))
		}
		wantChange(t, when+": covered", coveredFigure.Change, 2)
		wantChange(t, when+": covered series", eventsPublishedSeries(t, covered).Change, 2)
	}
	assert("published")

	// Cancelling rewrites nothing: the Event stays on the day it was put up.
	postEventOK(t, env, sessionID, cancelled, "cancel")
	assert("after a cancellation")
}

// TestPlatformTrendsPutsAPublicationOnItsPlatformDay: platform midnight is
// 05:00Z. An Event held in Madrid, where both instants are the same morning,
// counts on the platform's day either side of it.
func TestPlatformTrendsPutsAPublicationOnItsPlatformDay(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	startsAt := env.fixedClock.Add(72 * time.Hour)

	publishingAt(t, env, time.Date(2026, 7, 5, 4, 59, 59, 0, time.UTC), func() {
		publishEventStarting(t, env, sessionID, "Late Fest", "late-fest", startsAt, "Europe/Madrid", 1000, 10)
	})
	publishingAt(t, env, time.Date(2026, 7, 5, 5, 0, 0, 0, time.UTC), func() {
		publishEventStarting(t, env, sessionID, "Early Fest", "early-fest", startsAt, "Europe/Madrid", 1000, 10)
		publishEventStarting(t, env, sessionID, "Early Fest Two", "early-fest-two", startsAt, "Europe/Madrid", 1000, 10)
	})

	series := eventsPublishedSeries(t, readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d"))
	if got := bucketOn(t, series, "2026-07-04").Value; got != 1 {
		t.Fatalf("2026-07-04 = %d, want the 1 Event published at 23:59:59 in Guayaquil", got)
	}
	if got := bucketOn(t, series, "2026-07-05").Value; got != 2 {
		t.Fatalf("2026-07-05 = %d, want the 2 Events published at 00:00 in Guayaquil", got)
	}
}

// TestTheEventsPublishedMigrationStandsInCreationForWhatIsPublished: run
// over Events that carry no publication instant, as every Event did before
// it, migration 137 gives a `published` Event its creation instant and
// leaves a `draft` and a `cancelled` one empty; it writes down the day it
// ran, which the read states; a `draft` published afterwards takes the real
// instant; and a replay moves nothing.
func TestTheEventsPublishedMigrationStandsInCreationForWhatIsPublished(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)

	published := publishEventAt(t, env, sessionID, "Standing Fest", "standing-fest", env.fixedClock)
	cancelled := publishEventAt(t, env, sessionID, "Gone Fest", "gone-fest", env.fixedClock)
	postEventOK(t, env, sessionID, cancelled, "cancel")
	draft := createDraftEvent(t, env, sessionID, "Coming Fest", "coming-fest")
	patchCheckoutEvent(t, env, sessionID, draft, "Coming Fest", "coming-fest", false)
	createTicketTypeWithCapacity(t, env, sessionID, draft, "GA", 1000, 10)

	// The platform as the migration found it, staged in SQL because no route
	// can reach it: no Event carries a publication instant, each was created
	// on a day of its own, and no recording-began day is written down. The
	// day the harness's own startup recorded is put back afterwards, since
	// that table outlives a test.
	setEventCreatedAt(t, env, published, time.Date(2026, 7, 3, 15, 0, 0, 0, time.UTC))
	setEventCreatedAt(t, env, cancelled, time.Date(2026, 7, 2, 15, 0, 0, 0, time.UTC))
	setEventCreatedAt(t, env, draft, time.Date(2026, 7, 1, 15, 0, 0, 0, time.UTC))
	if _, err := env.db.Exec(`UPDATE events SET first_published_at = NULL`); err != nil {
		t.Fatalf("clear publication instants: %v", err)
	}
	var recorded string
	if err := env.db.QueryRow(`SELECT began_on::text FROM events_published_recording`).Scan(&recorded); err != nil {
		t.Fatalf("read the recorded day: %v", err)
	}
	t.Cleanup(func() {
		if _, err := env.db.Exec(`UPDATE events_published_recording SET began_on = $1::date`, recorded); err != nil {
			t.Errorf("restore the recorded day: %v", err)
		}
	})
	if _, err := env.db.Exec(`DELETE FROM events_published_recording`); err != nil {
		t.Fatalf("clear the recorded day: %v", err)
	}

	dayBefore := platformDayNow(t)
	executeMigration(t, env, eventsPublishedMigration)
	dayAfter := platformDayNow(t)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	assertBackfilled := func(when string) platformTrendsFigure {
		t.Helper()
		trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
		series := eventsPublishedSeries(t, trends)
		if got := bucketOn(t, series, "2026-07-03").Value; got != 1 {
			t.Fatalf("%s: 2026-07-03 = %d, want 1: a published Event stands on its creation day", when, got)
		}
		// A cancelled Event and a draft stand nowhere, so the data begins on
		// 2026-07-03 and the days before it were not counted (#842).
		for i, bucket := range series.Buckets {
			if day := trends.Span.Buckets[i].FirstDay; day < "2026-07-03" && bucket != nil {
				t.Fatalf("%s: %s = %+v, want null before the data began", when, day, bucket)
			}
		}
		if got := todayValue(t, series); got != 0 {
			t.Fatalf("%s: today = %d, want 0: the real publications of today were before the migration", when, got)
		}
		return trends.Figures.EventsPublished
	}
	figure := assertBackfilled("after the migration")
	if figure.Total == nil || *figure.Total != 1 {
		t.Fatalf("total = %v, want the 1 Event published when the migration ran", figure.Total)
	}
	assertDataSince(t, "events_published", figure, "2026-07-03")
	if got := *figure.ExactSince; got != dayBefore && got != dayAfter {
		t.Fatalf("exact_since = %s, want the platform day the migration ran (%s)", got, dayAfter)
	}

	// The recorded day is kept, never inferred again: moved in SQL to a day
	// of its own, since a migration only ever runs on the wall clock's day, a
	// replay leaves it where it is.
	if _, err := env.db.Exec(`UPDATE events_published_recording SET began_on = '2026-07-04'`); err != nil {
		t.Fatalf("move the recorded day: %v", err)
	}

	// A draft published after the migration takes the real instant, not its
	// creation day.
	publishingAt(t, env, time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC), func() {
		postEventOK(t, env, sessionID, draft, "publish")
	})

	assertPublished := func(when string) {
		t.Helper()
		figure := assertBackfilled(when)
		if got := bucketOn(t, figure.Series[0], "2026-07-06").Value; got != 1 {
			t.Fatalf("%s: 2026-07-06 = %d, want the draft on the day it was really published", when, got)
		}
		if figure.Total == nil || *figure.Total != 2 {
			t.Fatalf("%s: total = %v, want 2", when, figure.Total)
		}
		if got := *figure.ExactSince; got != "2026-07-04" {
			t.Fatalf("%s: exact_since = %s, want the recorded day 2026-07-04", when, got)
		}
	}
	assertPublished("after a publication")

	// A replay moves neither a real instant nor the recorded day.
	executeMigration(t, env, eventsPublishedMigration)
	assertPublished("after a replay")
}

// TestPlatformTrendsEventsPublishedWithNoHistoryHasNoComparison: with no
// Event ever published the figure totals zero over null buckets, with nothing
// to compare against, and still says from which day it is exact.
func TestPlatformTrendsEventsPublishedWithNoHistoryHasNoComparison(t *testing.T) {
	env := setupTest(t)
	createDraftEvent(t, env, orgAdminSession(t, env), "Draft", "draft")

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=7d")
	figure, series := trends.Figures.EventsPublished, eventsPublishedSeries(t, trends)
	if figure.Total == nil || *figure.Total != 0 || len(series.Buckets) != 7 {
		t.Fatalf("total = %v over %d buckets, want 0 over 7", figure.Total, len(series.Buckets))
	}
	if figure.DataSince != nil || figure.ComparisonTotal != nil || figure.Change != nil {
		t.Fatalf("data_since=%v comparison_total=%v change=%v; want all absent", figure.DataSince, figure.ComparisonTotal, figure.Change)
	}
	for i, bucket := range series.Buckets {
		if bucket != nil {
			t.Fatalf("bucket %d = %+v; want null, no Event was ever published (#842)", i, bucket)
		}
	}
}
