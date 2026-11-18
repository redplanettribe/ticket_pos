package integration

import (
	"net/http"
	"testing"
	"time"
)

// Platform Trends (#810, spec #808, ADR 0080): the platform-wide growth
// reading on the Operator Dashboard. Every day here is a day on the platform
// clock, America/Guayaquil, whatever timezone an Event is held in. The fixed
// clock is 2026-07-07T12:00Z, 07:00 in Guayaquil, so today is 2026-07-07 and
// every span ends on 2026-07-06.

const platformTrendsPath = "/api/v1/operator/platform-trends"

// platformTrends mirrors GET /api/v1/operator/platform-trends.
type platformTrends struct {
	Timezone string `json:"timezone"`
	Span     struct {
		Preset   string `json:"preset"`
		FirstDay string `json:"first_day"`
		LastDay  string `json:"last_day"`
	} `json:"span"`
	Comparison struct {
		Kind     string `json:"kind"`
		FirstDay string `json:"first_day"`
		LastDay  string `json:"last_day"`
	} `json:"comparison"`
	Granularity string `json:"granularity"`
	Today       string `json:"today"`
	Figures     struct {
		TicketsSold       platformTrendsFigure `json:"tickets_sold"`
		MoneySold         platformTrendsFigure `json:"money_sold"`
		PlatformFeeEarned platformTrendsFigure `json:"platform_fee_earned"`

		NewVerifiedCustomers platformTrendsFigure `json:"new_verified_customers"`
		NewOrganizations     platformTrendsFigure `json:"new_organizations"`
		PageViews            platformTrendsFigure `json:"page_views"`
		EventsPublished      platformTrendsFigure `json:"events_published"`
	} `json:"figures"`
}

type platformTrendsFigure struct {
	Recorded        bool                   `json:"recorded"`
	ExactSince      *string                `json:"exact_since"`
	DataSince       *string                `json:"data_since"`
	Total           *int                   `json:"total"`
	ComparisonTotal *int                   `json:"comparison_total"`
	Change          *float64               `json:"change"`
	Series          []platformTrendsSeries `json:"series"`
}

type platformTrendsSeries struct {
	Key     string                 `json:"key"`
	Total   int                    `json:"total"`
	Buckets []platformTrendsBucket `json:"buckets"`
	Today   *platformTrendsBucket  `json:"today"`

	ComparisonTotal   *int                    `json:"comparison_total"`
	Change            *float64                `json:"change"`
	ComparisonBuckets []*platformTrendsBucket `json:"comparison_buckets"`
}

type platformTrendsBucket struct {
	FirstDay string `json:"first_day"`
	LastDay  string `json:"last_day"`
	Value    int    `json:"value"`
	Partial  bool   `json:"partial"`
}

func readPlatformTrends(t *testing.T, env *testEnv, sessionID, query string) platformTrends {
	t.Helper()
	var out platformTrends
	operatorGetOK(t, env, sessionID, platformTrendsPath+query, &out)
	return out
}

// seriesFor returns one Sales Channel's series of Tickets Sold.
func seriesFor(t *testing.T, trends platformTrends, channel string) platformTrendsSeries {
	t.Helper()
	for _, series := range trends.Figures.TicketsSold.Series {
		if series.Key == channel {
			return series
		}
	}
	t.Fatalf("tickets_sold has no %q series: %+v", channel, trends.Figures.TicketsSold.Series)
	return platformTrendsSeries{}
}

// bucketOn returns the bucket a day falls in, failing when no bucket covers
// it: a missing day is the zero-fill bug and should be reported as such.
func bucketOn(t *testing.T, series platformTrendsSeries, day string) platformTrendsBucket {
	t.Helper()
	for _, bucket := range series.Buckets {
		if bucket.FirstDay <= day && day <= bucket.LastDay {
			return bucket
		}
	}
	t.Fatalf("no %s bucket covers %s", series.Key, day)
	return platformTrendsBucket{}
}

// moveSaleSoldAt restages when an Online Sale was made. A checkout is sold at
// the fixed clock, which is today; a span is made of closed days, so the
// online half of a test is moved into one.
func moveSaleSoldAt(t *testing.T, env *testEnv, eventID string, soldAt time.Time) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE ticket_sales SET sold_at = $1 WHERE event_id = $2 AND channel = 'online'`, soldAt, eventID)
	if err != nil {
		t.Fatalf("move online sale: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving the online sale affected %d rows, want 1", n)
	}
}

// moveImportToTheDoor puts an imported sale onto the in_person channel, the
// state a POS would leave behind: there is no in-person recording endpoint,
// so the suite stages it in SQL as the Sales Export tests do.
func moveImportToTheDoor(t *testing.T, env *testEnv, batchID string) {
	t.Helper()
	res, err := env.db.Exec(`UPDATE ticket_sales SET channel = 'in_person' WHERE import_batch_id = $1`, batchID)
	if err != nil {
		t.Fatalf("move import to the door: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("moving batch %s in-person affected %d rows, want 1", batchID, n)
	}
}

func importSale(email, ticketTypeID string, quantity int, soldAt string) map[string]any {
	return map[string]any{
		"customer_email": email, "customer_first_name": "Ana", "customer_last_name": "Lopez",
		"ticket_type_id": ticketTypeID, "quantity": quantity, "payment_method": "cash", "sold_at": soldAt,
	}
}

// TestPlatformTrendsStatesTicketsSoldBySalesChannel: one sale on each Sales
// Channel, across two Organizations, on yesterday; each is stated in its own
// series, and the figure's total is all of them.
func TestPlatformTrendsStatesTicketsSoldBySalesChannel(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, adminSessionID, "Channel Fest", "channel-fest", feeTestBaseCents, 100)

	begin := beginCheckoutOK(t, env, "test-org", "channel-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 2}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")
	moveSaleSoldAt(t, env, eventID, time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC))

	commitBatch(t, env, adminSessionID, eventID, "imported", []map[string]any{
		importSale("bea@example.com", gaID, 3, "2026-07-06T15:00:00Z"),
	})

	// A second Organization's door sale: the figure is the whole platform's.
	otherSessionID := verifyOTP(t, env, "other@example.com")
	createOrganization(t, env, otherSessionID, "Other Org", "other-org")
	otherEventID := createDraftEvent(t, env, otherSessionID, "Door Fest", "door-fest")
	otherGAID := createTicketTypeWithCapacity(t, env, otherSessionID, otherEventID, "GA", 1000, 100)
	doorBatch := commitBatch(t, env, otherSessionID, otherEventID, "door", []map[string]any{
		importSale("cris@example.com", otherGAID, 5, "2026-07-06T20:00:00Z"),
	})
	moveImportToTheDoor(t, env, doorBatch)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")

	figure := trends.Figures.TicketsSold
	if !figure.Recorded || figure.ExactSince != nil {
		t.Fatalf("tickets_sold recorded=%v exact_since=%v; want recorded and always exact", figure.Recorded, figure.ExactSince)
	}
	if len(figure.Series) != 3 || figure.Series[0].Key != "online" || figure.Series[1].Key != "in_person" || figure.Series[2].Key != "import" {
		t.Fatalf("series = %+v; want online, in_person, import in that order", figure.Series)
	}
	for channel, want := range map[string]int{"online": 2, "in_person": 5, "import": 3} {
		series := seriesFor(t, trends, channel)
		if got := bucketOn(t, series, "2026-07-06").Value; got != want {
			t.Fatalf("%s on 2026-07-06 = %d, want %d", channel, got, want)
		}
		if series.Total != want {
			t.Fatalf("%s total = %d, want %d", channel, series.Total, want)
		}
	}
	if figure.Total == nil || *figure.Total != 10 {
		t.Fatalf("tickets_sold total = %v, want 10 across every channel and Organization", figure.Total)
	}
}

// TestPlatformTrendsReadsThePlatformDayNotTheEventsDay: two sales a second
// apart either side of midnight in Guayaquil, for an Event held in Madrid,
// where both fall on the same day. They land on the two platform days.
func TestPlatformTrendsReadsThePlatformDayNotTheEventsDay(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSessionID, "Madrid Fest", "madrid-fest")
	setEventTimezone(t, env, eventID, "Europe/Madrid")
	gaID := createTicketTypeWithCapacity(t, env, adminSessionID, eventID, "GA", 1000, 100)

	// Midnight on 2026-07-05 in Guayaquil (UTC-5) is 2026-07-05T05:00Z, which
	// is 07:00 on the 5th in Madrid either way.
	commitBatch(t, env, adminSessionID, eventID, "before-midnight", []map[string]any{
		importSale("late@example.com", gaID, 1, "2026-07-05T04:59:59Z"),
	})
	commitBatch(t, env, adminSessionID, eventID, "after-midnight", []map[string]any{
		importSale("early@example.com", gaID, 4, "2026-07-05T05:00:00Z"),
	})

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	if trends.Timezone != "America/Guayaquil" {
		t.Fatalf("timezone = %q, want the platform clock America/Guayaquil", trends.Timezone)
	}
	imported := seriesFor(t, trends, "import")
	if got := bucketOn(t, imported, "2026-07-04").Value; got != 1 {
		t.Fatalf("2026-07-04 = %d, want the 1 ticket sold at 23:59:59 in Guayaquil", got)
	}
	if got := bucketOn(t, imported, "2026-07-05").Value; got != 4 {
		t.Fatalf("2026-07-05 = %d, want the 4 tickets sold at 00:00 in Guayaquil", got)
	}
}

// TestPlatformTrendsCountsActiveSalesOnTheDaySold: a reversed sale is in no
// bucket, and a sale imported today is counted on the day it says it was
// sold, not on the day it reached the platform.
func TestPlatformTrendsCountsActiveSalesOnTheDaySold(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, adminSessionID, "History Fest", "history-fest")
	gaID := createTicketTypeWithCapacity(t, env, adminSessionID, eventID, "GA", 1000, 100)

	commitBatch(t, env, adminSessionID, eventID, "kept", []map[string]any{
		importSale("keep@example.com", gaID, 2, "2026-07-01T15:00:00Z"),
	})
	undone := commitBatch(t, env, adminSessionID, eventID, "undone", []map[string]any{
		importSale("gone@example.com", gaID, 7, "2026-07-01T16:00:00Z"),
	})
	undoBatch(t, env, adminSessionID, eventID, undone)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=7d")
	imported := seriesFor(t, trends, "import")
	if got := bucketOn(t, imported, "2026-07-01").Value; got != 2 {
		t.Fatalf("2026-07-01 = %d, want 2: the reversed 7 are in no bucket", got)
	}
	if imported.Total != 2 {
		t.Fatalf("import total = %d, want 2", imported.Total)
	}
	// Both batches were recorded today, and today carries none of them.
	if imported.Today == nil || imported.Today.Value != 0 {
		t.Fatalf("today = %+v, want zero: an import counts on its sold day", imported.Today)
	}
}

// TestPlatformTrendsSetsTodayAsideAsPartial: a sale made today is drawn in
// today's bucket, flagged partial, and is in no total; the span's last
// bucket is yesterday.
func TestPlatformTrendsSetsTodayAsideAsPartial(t *testing.T) {
	env := setupTest(t)
	adminSessionID := orgAdminSession(t, env)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "Today Fest", "today-fest", feeTestBaseCents, 100)

	// Sold at the fixed clock: today.
	begin := beginCheckoutOK(t, env, "test-org", "today-fest",
		checkoutBody("ana@example.com", "Ana", "Lopez", map[string]any{"ticket_type_id": gaID, "quantity": 3}))
	confirmCheckoutOK(t, env, begin.ClientTransactionID, "approved")

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "")
	if trends.Today != "2026-07-07" || trends.Span.LastDay != "2026-07-06" {
		t.Fatalf("today=%s last_day=%s; want today 2026-07-07 and a span ending yesterday", trends.Today, trends.Span.LastDay)
	}
	online := seriesFor(t, trends, "online")
	last := online.Buckets[len(online.Buckets)-1]
	if last.FirstDay != "2026-07-06" || last.LastDay != "2026-07-06" || last.Partial {
		t.Fatalf("last bucket = %+v; want yesterday, whole", last)
	}
	if online.Today == nil || *online.Today != (platformTrendsBucket{FirstDay: "2026-07-07", LastDay: "2026-07-07", Value: 3, Partial: true}) {
		t.Fatalf("today = %+v; want 2026-07-07 carrying 3, flagged partial", online.Today)
	}
	if online.Total != 0 || *trends.Figures.TicketsSold.Total != 0 {
		t.Fatalf("totals = %d / %d; today is in no total", online.Total, *trends.Figures.TicketsSold.Total)
	}
}

// TestPlatformTrendsSpans: the default is 30 days; up to 90 days the buckets
// are days and 12 months are weeks, each carrying its first and last day,
// contiguous and zero-filled; a custom range reaching past yesterday is cut
// there.
func TestPlatformTrendsSpans(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		query       string
		preset      string
		first       string
		last        string
		granularity string
		buckets     int
		hasToday    bool
	}{
		{"", "30d", "2026-06-07", "2026-07-06", "day", 30, true},
		{"?span=7d", "7d", "2026-06-30", "2026-07-06", "day", 7, true},
		{"?span=30d", "30d", "2026-06-07", "2026-07-06", "day", 30, true},
		{"?span=90d", "90d", "2026-04-08", "2026-07-06", "day", 90, true},
		{"?span=12m", "12m", "2025-07-08", "2026-07-06", "week", 52, true},
		{"?span=custom&from=2026-06-20&to=2026-07-31", "custom", "2026-06-20", "2026-07-06", "day", 17, true},
		{"?span=custom&from=2026-06-01&to=2026-06-03", "custom", "2026-06-01", "2026-06-03", "day", 3, false},
	}
	for _, tc := range cases {
		trends := readPlatformTrends(t, env, operatorSessionID, tc.query)
		if trends.Span.Preset != tc.preset || trends.Span.FirstDay != tc.first || trends.Span.LastDay != tc.last {
			t.Fatalf("%q span = %+v; want %s %s..%s", tc.query, trends.Span, tc.preset, tc.first, tc.last)
		}
		if trends.Granularity != tc.granularity {
			t.Fatalf("%q granularity = %s, want %s", tc.query, trends.Granularity, tc.granularity)
		}
		for _, series := range trends.Figures.TicketsSold.Series {
			if len(series.Buckets) != tc.buckets {
				t.Fatalf("%q %s carries %d buckets, want %d", tc.query, series.Key, len(series.Buckets), tc.buckets)
			}
			if (series.Today != nil) != tc.hasToday {
				t.Fatalf("%q %s today = %+v; want present=%v", tc.query, series.Key, series.Today, tc.hasToday)
			}
			if series.Buckets[0].FirstDay != tc.first || series.Buckets[len(series.Buckets)-1].LastDay != tc.last {
				t.Fatalf("%q %s buckets run %s..%s, want %s..%s", tc.query, series.Key,
					series.Buckets[0].FirstDay, series.Buckets[len(series.Buckets)-1].LastDay, tc.first, tc.last)
			}
			for i, bucket := range series.Buckets {
				first, _ := time.Parse("2006-01-02", bucket.FirstDay)
				last, _ := time.Parse("2006-01-02", bucket.LastDay)
				length := int(last.Sub(first).Hours()/24) + 1
				if (tc.granularity == "day" && length != 1) || (tc.granularity == "week" && length != 7) {
					t.Fatalf("%q %s bucket %d runs %s..%s; want one %s", tc.query, series.Key, i, bucket.FirstDay, bucket.LastDay, tc.granularity)
				}
				if bucket.Value != 0 || bucket.Partial {
					t.Fatalf("%q %s bucket %+v; a day with no sales is a whole zero", tc.query, series.Key, bucket)
				}
				if i > 0 {
					previous, _ := time.Parse("2006-01-02", series.Buckets[i-1].LastDay)
					if !first.Equal(previous.AddDate(0, 0, 1)) {
						t.Fatalf("%q %s bucket %d starts %s after one ending %s; the buckets skipped a day",
							tc.query, series.Key, i, bucket.FirstDay, series.Buckets[i-1].LastDay)
					}
				}
			}
		}
	}
}

// TestPlatformTrendsRefusesAMalformedSpan: an unknown span, a malformed day
// and a custom range whose start is after its end are each a 400.
func TestPlatformTrendsRefusesAMalformedSpan(t *testing.T) {
	env := setupTest(t)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	cases := []struct {
		query string
		field string
		code  string
	}{
		{"?span=14d", "span", "INVALID_ENUM"},
		{"?span=custom&from=2026-06-01", "to", "REQUIRED"},
		{"?span=custom&from=yesterday&to=2026-06-03", "from", "INVALID_DATE"},
		{"?span=custom&from=2026-06-05&to=2026-06-03", "from", "INVALID_START_AFTER_END"},
		{"?span=custom&from=2026-07-07&to=2026-07-09", "from", "INVALID_START_AFTER_END"},
		{"?span=custom&from=2010-01-01&to=2026-07-01", "from", "TOO_MANY_ITEMS"},
		{"?compare=last_year", "compare", "INVALID_ENUM"},
	}
	for _, tc := range cases {
		resp, body := env.get(t, platformTrendsPath+tc.query, authHeader(operatorSessionID))
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q status=%d, want 400; error=%+v", tc.query, resp.StatusCode, body.Error)
		}
		if got := fieldErrorsByName(t, body)[tc.field]; got.Code != tc.code {
			t.Fatalf("%q %s refusal = %+v, want %s", tc.query, tc.field, got, tc.code)
		}
	}
}
