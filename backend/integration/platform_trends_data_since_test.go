package integration

import "testing"

// A count figure's main series before its data begins (#842, spec #832): a
// bucket wholly before data_since is null, drawn as a gap, because nothing
// was counted there; a bucket holding data_since starts on it; a bucket on or
// after it with nothing in it is still a zero. The fixed clock makes today
// 2026-07-07, so the default 30-day span is 2026-06-07..2026-07-06.

// TestPlatformTrendsCountIsAGapBeforeItsDataBegins: a span that begins before
// the first sale states null for every bucket before that day, on every
// Sales Channel, and zero for an empty day after it.
func TestPlatformTrendsCountIsAGapBeforeItsDataBegins(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2026-06-20", 3)
	sales.sell(t, "2026-06-25", 5)

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=30d")
	figure := trends.Figures.TicketsSold
	assertDataSince(t, "tickets_sold", figure, "2026-06-20")
	if len(trends.Span.Buckets) != 30 || trends.Span.Buckets[0].FirstDay != "2026-06-07" || trends.Span.Buckets[29].LastDay != "2026-07-06" {
		t.Fatalf("span buckets = %+v; want the 30 days 2026-06-07..2026-07-06", trends.Span.Buckets)
	}
	for _, series := range figure.Series {
		if len(series.Buckets) != 30 {
			t.Fatalf("%s carries %d buckets, want 30", series.Key, len(series.Buckets))
		}
		for i, bucket := range series.Buckets {
			day := trends.Span.Buckets[i].FirstDay
			if before := day < "2026-06-20"; (bucket == nil) != before {
				t.Fatalf("%s bucket on %s = %+v; want null exactly before the data begins on 2026-06-20", series.Key, day, bucket)
			}
			if bucket != nil && (bucket.FirstDay != day || bucket.LastDay != day) {
				t.Fatalf("%s bucket %d = %+v; want the day %s", series.Key, i, bucket, day)
			}
		}
	}
	imported := seriesFor(t, trends, "import")
	if got := bucketOn(t, imported, "2026-06-20").Value; got != 3 {
		t.Fatalf("2026-06-20 = %d, want 3", got)
	}
	if got := bucketOn(t, imported, "2026-06-21").Value; got != 0 {
		t.Fatalf("2026-06-21 = %d, want a zero: counted, and nothing sold", got)
	}
	if imported.Today == nil || imported.Today.Value != 0 {
		t.Fatalf("today = %+v, want a zero beside the span", imported.Today)
	}
}

// TestPlatformTrendsCountTotalsAreUnchangedByTheGap: the totals of a span
// reaching before data_since are of the days with data, as they always were,
// and its comparison, wholly before the data, has no total and no change.
func TestPlatformTrendsCountTotalsAreUnchangedByTheGap(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	sales.sell(t, "2026-06-20", 3)
	sales.sell(t, "2026-06-25", 5)

	for _, query := range []string{"?span=30d&compare=previous", "?span=30d&compare=year_earlier", "?span=12m"} {
		trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), query)
		figure := trends.Figures.TicketsSold
		if figure.Total == nil || *figure.Total != 8 || figure.ComparisonTotal != nil || figure.Change != nil {
			t.Fatalf("%s totals %v against %v change %v; want 8 against nothing", query,
				intOrNil(figure.Total), intOrNil(figure.ComparisonTotal), shown(figure.Change))
		}
		imported := seriesFor(t, trends, "import")
		if imported.Total != 8 || imported.ComparisonTotal != nil || imported.Change != nil {
			t.Fatalf("%s import total %d against %v change %v; want 8 against nothing", query,
				imported.Total, intOrNil(imported.ComparisonTotal), shown(imported.Change))
		}
		for i, compared := range imported.ComparisonBuckets {
			if compared != nil {
				t.Fatalf("%s comparison bucket %d = %+v; the comparison is wholly before the data", query, i, compared)
			}
		}
	}
}

// TestPlatformTrendsWeeklyCountBucketIsCutAtTheFirstDayOfData: in a yearly
// reading the week holding the first day of data starts on that day and
// counts from it, and every week before it is null.
func TestPlatformTrendsWeeklyCountBucketIsCutAtTheFirstDayOfData(t *testing.T) {
	env := setupTest(t)
	sales := newImportedSales(t, env)
	// The 12m span is 2025-07-08..2026-07-06, whose weeks run Tuesday to
	// Monday: 2026-03-03..2026-03-09 holds Thursday 2026-03-05.
	sales.sell(t, "2026-03-05", 4)
	sales.sell(t, "2026-03-09", 2)

	trends := readPlatformTrends(t, env, operatorSession(t, env, "operator@example.com"), "?span=12m")
	imported := seriesFor(t, trends, "import")
	if len(imported.Buckets) != 52 || len(trends.Span.Buckets) != 52 {
		t.Fatalf("%d buckets over %d span buckets, want 52 weeks", len(imported.Buckets), len(trends.Span.Buckets))
	}
	cut := -1
	for i, bucket := range imported.Buckets {
		if bucket != nil {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatal("no bucket present")
	}
	if week := trends.Span.Buckets[cut]; week.FirstDay != "2026-03-03" || week.LastDay != "2026-03-09" {
		t.Fatalf("first present bucket sits on the week %+v; want 2026-03-03..2026-03-09", week)
	}
	first := imported.Buckets[cut]
	if first.FirstDay != "2026-03-05" || first.LastDay != "2026-03-09" || first.Value != 6 {
		t.Fatalf("first present bucket = %+v; want 2026-03-05..2026-03-09 carrying 6", first)
	}
	for i := cut + 1; i < len(imported.Buckets); i++ {
		bucket := imported.Buckets[i]
		if bucket == nil {
			t.Fatalf("bucket %d is null after the data began", i)
		}
		if bucket.FirstDay != trends.Span.Buckets[i].FirstDay || bucket.LastDay != trends.Span.Buckets[i].LastDay {
			t.Fatalf("bucket %d = %+v; want the whole week %+v", i, bucket, trends.Span.Buckets[i])
		}
	}
	if imported.Total != 6 {
		t.Fatalf("total = %d, want 6", imported.Total)
	}
}
