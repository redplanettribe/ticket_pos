package integration

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// Platform Trends comparison (#811, spec #808): every figure is set beside a
// comparison span of the same length, the span before it by default or the
// span 52 weeks earlier, aligned bucket to bucket. The fixed clock makes today
// 2026-07-07, so the default 30-day span is 2026-06-07..2026-07-06 and the
// span before it 2026-05-08..2026-06-06.

// importedSales is one Event the comparison tests import their sales into, so
// a test states only the days and quantities it is about.
type importedSales struct {
	env       *testEnv
	sessionID string
	eventID   string
	gaID      string
	count     int
}

func newImportedSales(t *testing.T, env *testEnv) *importedSales {
	t.Helper()
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Compare Fest", "compare-fest")
	gaID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 100000, 100)
	return &importedSales{env: env, sessionID: sessionID, eventID: eventID, gaID: gaID}
}

// sell imports quantity tickets sold at 15:00 UTC (10:00 in Guayaquil) on day.
func (s *importedSales) sell(t *testing.T, day string, quantity int) {
	t.Helper()
	s.count++
	key := fmt.Sprintf("sale-%d", s.count)
	commitBatch(t, s.env, s.sessionID, s.eventID, key, []map[string]any{
		importSale(fmt.Sprintf("buyer-%d@example.com", s.count), s.gaID, quantity, day+"T15:00:00Z"),
	})
}

func bucketDays(t *testing.T, bucket platformTrendsBucket) int {
	t.Helper()
	first, err := time.Parse("2006-01-02", bucket.FirstDay)
	if err != nil {
		t.Fatalf("bucket first_day %q: %v", bucket.FirstDay, err)
	}
	last, err := time.Parse("2006-01-02", bucket.LastDay)
	if err != nil {
		t.Fatalf("bucket last_day %q: %v", bucket.LastDay, err)
	}
	return int(last.Sub(first).Hours()/24) + 1
}

func wantChange(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-9 {
		t.Fatalf("%s change = %v, want %v", label, got, want)
	}
}

func intOrNil(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// TestPlatformTrendsComparesWithThePreviousSpan: sales in two adjacent 30-day
// spans give both totals and the signed change between them, growth and
// decline alike, on the figure and on the series that sold.
func TestPlatformTrendsComparesWithThePreviousSpan(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2026-05-08", 4) // the previous span's first day
	sales.sell(t, "2026-06-20", 6)
	// Today, 01:00 in Guayaquil: drawn as partial, in neither total.
	commitBatch(t, env, sales.sessionID, sales.eventID, "today", []map[string]any{
		importSale("today@example.com", sales.gaID, 50, "2026-07-07T06:00:00Z"),
	})

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	trends := readPlatformTrends(t, env, operatorSessionID, "")
	if trends.Comparison.Kind != "previous" || trends.Comparison.FirstDay != "2026-05-08" || trends.Comparison.LastDay != "2026-06-06" {
		t.Fatalf("comparison = %+v; want the previous span 2026-05-08..2026-06-06 by default", trends.Comparison)
	}
	figure := trends.Figures.TicketsSold
	if figure.DataSince == nil || *figure.DataSince != "2026-05-08" {
		t.Fatalf("data_since = %v, want the first day sold 2026-05-08", figure.DataSince)
	}
	if figure.Total == nil || *figure.Total != 6 || figure.ComparisonTotal == nil || *figure.ComparisonTotal != 4 {
		t.Fatalf("totals = %v against %v; want 6 against 4", intOrNil(figure.Total), intOrNil(figure.ComparisonTotal))
	}
	wantChange(t, "growth", figure.Change, 0.5)
	imported := seriesFor(t, trends, "import")
	if imported.Today == nil || imported.Today.Value != 50 || imported.Total != 6 {
		t.Fatalf("today %+v total %d; want today's 50 drawn apart and a total of 6", imported.Today, imported.Total)
	}
	if imported.ComparisonTotal == nil || *imported.ComparisonTotal != 4 {
		t.Fatalf("import comparison total = %v, want 4", intOrNil(imported.ComparisonTotal))
	}
	wantChange(t, "import growth", imported.Change, 0.5)
	if first := imported.ComparisonBuckets[0]; first == nil || first.FirstDay != "2026-05-08" || first.Value != 4 {
		t.Fatalf("first comparison bucket = %+v; want 2026-05-08 carrying 4", first)
	}

	sales.sell(t, "2026-05-10", 8)
	trends = readPlatformTrends(t, env, operatorSessionID, "?compare=previous")
	figure = trends.Figures.TicketsSold
	if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 12 {
		t.Fatalf("comparison total = %v, want 12", intOrNil(figure.ComparisonTotal))
	}
	wantChange(t, "decline", figure.Change, -0.5)
	wantChange(t, "import decline", seriesFor(t, trends, "import").Change, -0.5)
}

// TestPlatformTrendsComparisonIsAlignedBucketToBucket: for daily and weekly
// spans and for both comparisons, the comparison series has one bucket per
// bucket of the span, each as long as its counterpart, contiguous across the
// comparison span; today has no counterpart.
func TestPlatformTrendsComparisonIsAlignedBucketToBucket(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2024-01-01", 1) // history long before every comparison
	sales.sell(t, "2025-07-07", 2) // the last day 52 weeks before yesterday

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	cases := []struct {
		query string
		kind  string
		first string
		last  string
	}{
		{"?span=7d", "previous", "2026-06-23", "2026-06-29"},
		{"?span=7d&compare=year_earlier", "year_earlier", "2025-07-01", "2025-07-07"},
		{"?span=30d&compare=year_earlier", "year_earlier", "2025-06-08", "2025-07-07"},
		{"?span=90d&compare=previous", "previous", "2026-01-08", "2026-04-07"},
		{"?span=90d&compare=year_earlier", "year_earlier", "2025-04-09", "2025-07-07"},
		{"?span=12m", "previous", "2024-07-09", "2025-07-07"},
		{"?span=12m&compare=year_earlier", "year_earlier", "2024-07-09", "2025-07-07"},
		{"?span=custom&from=2026-03-25&to=2026-07-06&compare=previous", "previous", "2025-12-11", "2026-03-24"},
		{"?span=custom&from=2026-03-25&to=2026-07-06&compare=year_earlier", "year_earlier", "2025-03-26", "2025-07-07"},
	}
	for _, tc := range cases {
		trends := readPlatformTrends(t, env, operatorSessionID, tc.query)
		if trends.Comparison.Kind != tc.kind || trends.Comparison.FirstDay != tc.first || trends.Comparison.LastDay != tc.last {
			t.Fatalf("%q comparison = %+v; want %s %s..%s", tc.query, trends.Comparison, tc.kind, tc.first, tc.last)
		}
		for _, series := range trends.Figures.TicketsSold.Series {
			if len(series.ComparisonBuckets) != len(series.Buckets) {
				t.Fatalf("%q %s has %d comparison buckets for %d buckets", tc.query, series.Key, len(series.ComparisonBuckets), len(series.Buckets))
			}
			for i, compared := range series.ComparisonBuckets {
				if compared == nil {
					t.Fatalf("%q %s comparison bucket %d is absent with data since 2024", tc.query, series.Key, i)
				}
				if compared.Partial {
					t.Fatalf("%q %s comparison bucket %d is partial; only today is", tc.query, series.Key, i)
				}
				if got, want := bucketDays(t, *compared), bucketDays(t, series.Buckets[i]); got != want {
					t.Fatalf("%q %s comparison bucket %d covers %d days against %d", tc.query, series.Key, i, got, want)
				}
				if i > 0 {
					previous, _ := time.Parse("2006-01-02", series.ComparisonBuckets[i-1].LastDay)
					if compared.FirstDay != previous.AddDate(0, 0, 1).Format("2006-01-02") {
						t.Fatalf("%q %s comparison bucket %d starts %s; the comparison buckets skipped a day", tc.query, series.Key, i, compared.FirstDay)
					}
				}
			}
			if got := series.ComparisonBuckets[0].FirstDay; got != tc.first {
				t.Fatalf("%q %s comparison starts %s, want %s", tc.query, series.Key, got, tc.first)
			}
			if got := series.ComparisonBuckets[len(series.ComparisonBuckets)-1].LastDay; got != tc.last {
				t.Fatalf("%q %s comparison ends %s, want %s", tc.query, series.Key, got, tc.last)
			}
		}
		imported := seriesFor(t, trends, "import")
		lastCompared := imported.ComparisonBuckets[len(imported.ComparisonBuckets)-1]
		wantLast := 0
		if tc.last == "2025-07-07" {
			wantLast = 2
		}
		if lastCompared.Value != wantLast {
			t.Fatalf("%q last comparison bucket = %+v, want %d", tc.query, lastCompared, wantLast)
		}
	}
}

// TestPlatformTrendsChangeIsAbsentWithNothingToCompare: a comparison span with
// data but no sales has a zero total and no change; one wholly before the
// first day of data has neither a total, a change nor a single bucket.
func TestPlatformTrendsChangeIsAbsentWithNothingToCompare(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2026-07-01", 3)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	trends := readPlatformTrends(t, env, operatorSessionID, "")
	figure := trends.Figures.TicketsSold
	if figure.DataSince == nil || *figure.DataSince != "2026-07-01" {
		t.Fatalf("data_since = %v, want 2026-07-01", figure.DataSince)
	}
	if figure.ComparisonTotal != nil || figure.Change != nil {
		t.Fatalf("comparison total %v change %v; want both absent before the data begins", intOrNil(figure.ComparisonTotal), figure.Change)
	}
	for _, series := range figure.Series {
		if series.ComparisonTotal != nil || series.Change != nil {
			t.Fatalf("%s comparison total %v change %v; want both absent", series.Key, intOrNil(series.ComparisonTotal), series.Change)
		}
		if len(series.ComparisonBuckets) != 30 {
			t.Fatalf("%s carries %d comparison entries, want one per bucket", series.Key, len(series.ComparisonBuckets))
		}
		for i, compared := range series.ComparisonBuckets {
			if compared != nil {
				t.Fatalf("%s comparison bucket %d = %+v; missing history is absent, never zero", series.Key, i, compared)
			}
		}
	}

	// History that ends before the comparison span: the comparison is a
	// measured zero, which gives no ratio.
	sales.sell(t, "2026-04-01", 5)
	trends = readPlatformTrends(t, env, operatorSessionID, "")
	figure = trends.Figures.TicketsSold
	if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 0 || figure.Change != nil {
		t.Fatalf("comparison total %v change %v; want a zero total and no change", intOrNil(figure.ComparisonTotal), figure.Change)
	}
	for _, series := range figure.Series {
		if series.ComparisonTotal == nil || *series.ComparisonTotal != 0 || series.Change != nil {
			t.Fatalf("%s comparison total %v change %v; want zero and no change", series.Key, intOrNil(series.ComparisonTotal), series.Change)
		}
		for i, compared := range series.ComparisonBuckets {
			if compared == nil || compared.Value != 0 {
				t.Fatalf("%s comparison bucket %d = %+v; a day with data and no sales is a zero", series.Key, i, compared)
			}
		}
	}
}

// TestPlatformTrendsComparisonStartsWhereTheDataDoes: a comparison span that
// straddles the first day of data has buckets from that day on only, and a
// total over them, but no change (#826): the total is of fewer days than the
// span's, and their ratio would state growth that never happened. A
// comparison span that begins on the first day of data is covered whole and
// states its change.
func TestPlatformTrendsComparisonStartsWhereTheDataDoes(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2026-05-20", 2)
	sales.sell(t, "2026-06-10", 1)
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	// Daily: the previous span is 2026-05-08..2026-06-06.
	trends := readPlatformTrends(t, env, operatorSessionID, "?span=30d&compare=previous")
	imported := seriesFor(t, trends, "import")
	for i, compared := range imported.ComparisonBuckets {
		day := imported.Buckets[i].FirstDay
		if wantPresent := i >= 12; (compared != nil) != wantPresent {
			t.Fatalf("comparison bucket %d (beside %s) = %+v; want present=%v, data begins 2026-05-20", i, day, compared, wantPresent)
		}
	}
	if first := imported.ComparisonBuckets[12]; first.FirstDay != "2026-05-20" || first.Value != 2 {
		t.Fatalf("first present comparison bucket = %+v; want 2026-05-20 carrying 2", first)
	}
	if imported.ComparisonTotal == nil || *imported.ComparisonTotal != 2 {
		t.Fatalf("comparison total = %v, want 2", intOrNil(imported.ComparisonTotal))
	}
	if imported.Change != nil {
		t.Fatalf("straddled change = %v; want it absent, 30 days set against the 18 that have data", *imported.Change)
	}
	figure := trends.Figures.TicketsSold
	if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 2 || figure.Change != nil {
		t.Fatalf("figure comparison total %v change %v; want 2 and no change", intOrNil(figure.ComparisonTotal), shown(figure.Change))
	}

	// 2026-06-07..2026-06-24 is compared with 2026-05-20..2026-06-06, which
	// begins on the first day of data: the data covers every day of it.
	trends = readPlatformTrends(t, env, operatorSessionID, "?span=custom&from=2026-06-07&to=2026-06-24&compare=previous")
	if trends.Comparison.FirstDay != "2026-05-20" || trends.Comparison.LastDay != "2026-06-06" {
		t.Fatalf("comparison = %+v; want 2026-05-20..2026-06-06", trends.Comparison)
	}
	imported = seriesFor(t, trends, "import")
	for i, compared := range imported.ComparisonBuckets {
		if compared == nil {
			t.Fatalf("comparison bucket %d is absent in a comparison span that begins with the data", i)
		}
	}
	if imported.Total != 1 || imported.ComparisonTotal == nil || *imported.ComparisonTotal != 2 {
		t.Fatalf("totals = %d against %v; want 1 against 2", imported.Total, intOrNil(imported.ComparisonTotal))
	}
	wantChange(t, "covered", imported.Change, -0.5)
	wantChange(t, "covered figure", trends.Figures.TicketsSold.Change, -0.5)

	// A shorter custom range: 2026-05-27..2026-07-06 is compared with
	// 2026-04-16..2026-05-26, of which only the last seven days have data.
	trends = readPlatformTrends(t, env, operatorSessionID, "?span=custom&from=2026-05-27&to=2026-07-06&compare=previous")
	if trends.Comparison.FirstDay != "2026-04-16" || trends.Comparison.LastDay != "2026-05-26" {
		t.Fatalf("comparison = %+v; want 2026-04-16..2026-05-26", trends.Comparison)
	}
	imported = seriesFor(t, trends, "import")
	present := 0
	for _, compared := range imported.ComparisonBuckets {
		if compared != nil {
			present++
			if compared.FirstDay < "2026-05-20" {
				t.Fatalf("comparison bucket %+v starts before the data", compared)
			}
		}
	}
	if present != 7 || imported.ComparisonBuckets[len(imported.ComparisonBuckets)-7].FirstDay != "2026-05-20" {
		t.Fatalf("comparison buckets = %+v; want the seven days from 2026-05-20", imported.ComparisonBuckets)
	}
	if imported.ComparisonTotal == nil || *imported.ComparisonTotal != 2 || imported.Change != nil {
		t.Fatalf("comparison total %v change %v; want 2 and no change, 41 days set against 7", intOrNil(imported.ComparisonTotal), shown(imported.Change))
	}
}

// TestPlatformTrendsWeeklyComparisonBucketIsCutAtTheFirstDayOfData: in a
// yearly reading, the comparison week holding the first day of data starts on
// that day, and every week before it is absent. The weeks that have data are
// totalled, and no change is stated against them.
func TestPlatformTrendsWeeklyComparisonBucketIsCutAtTheFirstDayOfData(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	// The comparison of 12m is 2024-07-09..2025-07-07, whose weeks run
	// Tuesday to Monday: 2024-12-31..2025-01-06 holds 2025-01-01.
	sales.sell(t, "2025-01-01", 3)
	sales.sell(t, "2026-03-01", 6) // in the span, so there is a total to set against it
	operatorSessionID := operatorSession(t, env, "operator@example.com")

	trends := readPlatformTrends(t, env, operatorSessionID, "?span=12m&compare=year_earlier")
	imported := seriesFor(t, trends, "import")
	if len(imported.ComparisonBuckets) != 52 {
		t.Fatalf("%d comparison entries, want 52", len(imported.ComparisonBuckets))
	}
	cut := -1
	for i, compared := range imported.ComparisonBuckets {
		if compared != nil {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatal("no comparison bucket present")
	}
	first := imported.ComparisonBuckets[cut]
	if first.FirstDay != "2025-01-01" || first.LastDay != "2025-01-06" || first.Value != 3 {
		t.Fatalf("first comparison bucket = %+v; want 2025-01-01..2025-01-06 carrying 3", first)
	}
	if imported.Buckets[cut].LastDay != "2026-01-05" {
		t.Fatalf("it sits beside %+v; want the week ending 2026-01-05", imported.Buckets[cut])
	}
	for i := cut; i < len(imported.ComparisonBuckets); i++ {
		if imported.ComparisonBuckets[i] == nil {
			t.Fatalf("comparison bucket %d is absent after the data began", i)
		}
	}
	if imported.Total != 6 || imported.ComparisonTotal == nil || *imported.ComparisonTotal != 3 || imported.Change != nil {
		t.Fatalf("total %d comparison total %v change %v; want 6, 3 and no change against a year begun in January",
			imported.Total, intOrNil(imported.ComparisonTotal), shown(imported.Change))
	}
}

// TestPlatformTrendsHistoryBeginsAtTheFirstSaleEvenIfReversed: reversing the
// platform's earliest sale leaves the days after it measured, so the
// comparison is a zero there rather than absent.
func TestPlatformTrendsHistoryBeginsAtTheFirstSaleEvenIfReversed(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	first := commitBatch(t, env, sales.sessionID, sales.eventID, "first", []map[string]any{
		importSale("first@example.com", sales.gaID, 2, "2026-04-01T15:00:00Z"),
	})
	undoBatch(t, env, sales.sessionID, sales.eventID, first)
	sales.sell(t, "2026-07-01", 3)

	operatorSessionID := operatorSession(t, env, "operator@example.com")
	figure := readPlatformTrends(t, env, operatorSessionID, "").Figures.TicketsSold
	if figure.DataSince == nil || *figure.DataSince != "2026-04-01" {
		t.Fatalf("data_since = %v, want 2026-04-01, the first sale's day though it was reversed", figure.DataSince)
	}
	if figure.ComparisonTotal == nil || *figure.ComparisonTotal != 0 || figure.Change != nil {
		t.Fatalf("comparison total %v change %v; want a measured zero and no change", intOrNil(figure.ComparisonTotal), figure.Change)
	}
}
