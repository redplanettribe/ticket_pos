package integration

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// THE HOLDER EXPORT'S MEMORY, MEASURED FLAT (#658, parent #655, ADR 0075).
//
// This was the cap benchmark (#530). It found that excelize held the whole
// workbook at roughly 600 B a cell - fifty thousand wide rows took 6.9 GiB
// against an api_memory of 512Mi - which is why the Holder Export was capped at
// 2,000 Tickets. ADR 0075 removed the cap by streaming the file end to end, and
// the claim that has to hold now is not "this height fits" but "memory does not
// grow with the height at all". So the benchmark weighs one export at two
// heights, four times apart, and the two peaks should be level.
//
// IT GOES OVER HTTP against a seeded Postgres, so the cursor, the Answer
// batches, the writer and the response are all in one number. The download is
// written to a file on disk as it arrives, not into memory, because the client
// runs in this same process and a client holding the file would be weighed as
// though the server were.
//
// THE FIXTURE IS SEEDED WITH DIRECT SQL and not through the API, a deliberate
// compromise: two hundred thousand Tickets through checkout or Sale Import would
// take far longer than the measurement and would measure the importer. The rows
// are inserted in the shape the API would have left behind - active online
// sales, two Tickets per line, an accepted Holder per Ticket, an Answer per
// Ticket per question - so every query the export runs reads exactly what it
// reads in production.
//
// IT IS GUARDED BY AN ENVIRONMENT VARIABLE and therefore never part of `make
// test-integration`: seeding the taller height alone is millions of rows.
// Rerun it with:
//
//	HOLDER_EXPORT_BENCH=1 go test ./integration/ -run TestHolderExportMemoryIsFlat -v -timeout 60m
//
// HOLDER_EXPORT_BENCH_HEIGHTS overrides the heights (default "50000,200000").
//
// MEASURED 2026-09-22 on the development machine, at the widest plausible shape
// below (219 columns), with the process as deployed (no GOMEMLIMIT, GOGC=100):
//
//	 50,000 rows (10.95M cells):   7.0s, peak live heap 36 MiB, peak RSS 84 MiB, 27.7 MiB .xlsx
//	200,000 rows (43.8M cells):  33.5s, peak live heap 37 MiB, peak RSS 88 MiB, 110.8 MiB .xlsx
//
// against a 512Mi instance and a 300s request timeout.

// The WORST-CASE-BUT-PLAUSIBLE question shape, the one #530 measured the
// excelize build at, so the two can be compared: eight multiple-choice
// questions of twenty-five Options each - five of them RETIRED, which still
// take columns - and six questions of the kinds that take one column.
//
// The shape is chosen from what an Organization could actually do rather than
// from what the schema permits. A multi-day conference asks a workshop track, a
// dietary requirement, a t-shirt size, an arrival day and a merch choice, and a
// twenty-five-Option list is one of those after two years of additions and
// retirements.
const (
	benchChoiceQuestions = 8
	benchOptionsPer      = 25
	benchRetiredPer      = 5
	benchPlainQuestions  = 6
	benchTicketsPerSale  = 2
)

// TestHolderExportMemoryIsFlat weighs a Holder Export at each height.
//
// It asserts nothing about the numbers. A threshold here would be a benchmark
// that fails on a busy laptop and passes on an idle one; the comparison that
// matters - the two heights against each other, and both against a deployed
// memory limit on a machine that is neither - is a judgement for the person
// reading the output, recorded above. What it DOES assert is that each export
// succeeded and carries the rows it was given, so a number can never be
// reported for a file that was never built.
func TestHolderExportMemoryIsFlat(t *testing.T) {
	if os.Getenv("HOLDER_EXPORT_BENCH") != "1" {
		t.Skip("the Holder Export memory benchmark is run by hand: HOLDER_EXPORT_BENCH=1 (#658)")
	}
	for _, rows := range benchHeights(t) {
		t.Run(strconv.Itoa(rows), func(t *testing.T) {
			env := setupTest(t)
			enableTicketQuestions(t)
			enableTicketAssignment(t)
			sessionID := orgAdminSession(t, env)
			eventID := createDraftEvent(t, env, sessionID, "Benchmark Fest", "benchmark-fest")
			ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, rows+1000)

			seedStart := time.Now()
			seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, rows)
			t.Logf("seeded %d Tickets in %s", rows, time.Since(seedStart).Round(time.Millisecond))

			path := filepath.Join(t.TempDir(), "holders.xlsx")
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)
			resetPeakRSS()
			stop := sampleMemory()
			start := time.Now()
			size := downloadHolderExportToFile(t, env, sessionID, eventID, path)
			elapsed := time.Since(start)
			peakHeap, peakSys := stop()
			rss := peakRSS()

			wantColumns := 13 + benchChoiceQuestions*benchOptionsPer + benchPlainQuestions
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the download back: %v", err)
			}
			verifyHolderExportShape(t, data, rows, wantColumns)

			t.Logf(
				"VERDICT INPUT: %d rows x %d columns (%.2fM cells); %s wall against a 300s request timeout; "+
					"peak live heap %.0f MiB (%.0f MiB before), peak Go Sys %.0f MiB, peak RSS %.0f MiB "+
					"against a 512Mi instance limit; %.1f MiB .xlsx",
				rows, wantColumns, float64(rows*wantColumns)/1e6, elapsed.Round(time.Millisecond),
				mib(peakHeap), mib(before.HeapAlloc), mib(peakSys), mib(rss), float64(size)/(1<<20),
			)
		})
	}
}

// benchHeights is HOLDER_EXPORT_BENCH_HEIGHTS, or the two defaults.
func benchHeights(t *testing.T) []int {
	t.Helper()
	raw := os.Getenv("HOLDER_EXPORT_BENCH_HEIGHTS")
	if raw == "" {
		return []int{50_000, 200_000}
	}
	var heights []int
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 {
			t.Fatalf("HOLDER_EXPORT_BENCH_HEIGHTS=%q is not a list of positive numbers", raw)
		}
		heights = append(heights, n)
	}
	return heights
}

// downloadHolderExportToFile streams the unfiltered export onto disk and
// returns its size, failing on anything but a clean 200.
func downloadHolderExportToFile(t *testing.T, env *testEnv, sessionID, eventID, path string) int64 {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+holderExportPath(eventID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		t.Fatalf("status=%d body=%s", resp.StatusCode, string(body))
	}
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer func() { _ = out.Close() }()
	size, err := io.Copy(out, resp.Body)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	return size
}

// verifyHolderExportShape reads back the downloaded workbook's height and width
// without materialising it, so proving the file is real cannot cost more than
// building it did. See its caller for why GetRows is refused here.
func verifyHolderExportShape(t *testing.T, data []byte, wantRows, wantColumns int) {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	iter, err := f.Rows(holderExportSheet)
	if err != nil {
		t.Fatalf("stream %q: %v", holderExportSheet, err)
	}
	defer func() { _ = iter.Close() }()
	dataRows := 0
	for iter.Next() {
		if dataRows == 0 {
			header, err := iter.Columns()
			if err != nil {
				t.Fatalf("read header: %v", err)
			}
			if len(header) != wantColumns {
				t.Fatalf("export is %d columns wide, want %d", len(header), wantColumns)
			}
		}
		dataRows++
	}
	if err := iter.Error(); err != nil {
		t.Fatalf("stream rows: %v", err)
	}
	if dataRows-1 != wantRows {
		t.Fatalf("export carries %d rows, seeded %d", dataRows-1, wantRows)
	}
}

// seedHolderExportBenchFixture stages the roster the export is measured against:
// the questions and their Options, the sales, the Tickets, and one Answer per
// Ticket per question.
//
// EVERY QUESTION IS ANSWERED ON EVERY ROW, which is both the worst case and the
// realistic one for a required question — an unanswered question writes no cells
// at all, so a half-answered fixture would quietly measure a narrower sheet than
// its header claims. Every Ticket is ACCEPTED by a Holder of its own, so the
// Holder name and address columns are filled rather than blank and no two rows
// share a string the shared-string table could dedupe away.
func seedHolderExportBenchFixture(t *testing.T, env *testEnv, eventID, ticketTypeID string, rows int) {
	t.Helper()
	sales := (rows + benchTicketsPerSale - 1) / benchTicketsPerSale

	var orgID string
	if err := env.db.QueryRow(`SELECT organization_id FROM ticket_types WHERE id = $1`, ticketTypeID).Scan(&orgID); err != nil {
		t.Fatalf("read organization: %v", err)
	}

	exec := func(label, query string, args ...any) {
		t.Helper()
		if _, err := env.db.Exec(query, args...); err != nil {
			t.Fatalf("seed %s: %v", label, err)
		}
	}

	// THE QUESTIONS, approved outright. The review states are somebody else's
	// acceptance criteria (#312); what this fixture needs is questions the export
	// reads, and the export reads approved ones.
	exec("multi-choice questions", `
		INSERT INTO ticket_questions (ticket_type_id, label, kind, required, sort_order, review_status, approved_at, approved_by)
		SELECT $1, 'Multiple choice question ' || g, 'multi_choice', true, g, 'approved', NOW(), 'bench'
		FROM generate_series(1, $2) g
	`, ticketTypeID, benchChoiceQuestions)
	exec("plain questions", `
		INSERT INTO ticket_questions (ticket_type_id, label, kind, required, sort_order, review_status, approved_at, approved_by)
		SELECT $1, 'Plain question ' || k.n, k.kind, true, 100 + k.n, 'approved', NOW(), 'bench'
		FROM (VALUES (1,'short_text'),(2,'short_text'),(3,'long_text'),(4,'number'),(5,'date'),(6,'checkbox'))
		     AS k(n, kind)
	`, ticketTypeID)

	// THE OPTIONS, the last five of each question RETIRED. A retired Option keeps
	// its column — that is why an Option is retired and never deleted — so the
	// retired ones are part of the width being measured, not a subtraction from it.
	exec("options", `
		INSERT INTO ticket_question_options (ticket_question_id, label, sort_order, review_status, approved_at, approved_by, retired_at)
		SELECT q.id, 'Question ' || q.sort_order || ' option ' || o, o, 'approved', NOW(), 'bench',
		       CASE WHEN o > $2 - $3 THEN NOW() ELSE NULL END
		FROM ticket_questions q
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		CROSS JOIN generate_series(1, $2) o
		WHERE tt.id = $1 AND q.kind = 'multi_choice'
	`, ticketTypeID, benchOptionsPer, benchRetiredPer)

	// THE BUYERS. One Customer of record per sale, distinct names and addresses,
	// so the buyer columns hold as many distinct strings as a real roster would.
	exec("buyer customers", `
		INSERT INTO customers (email, first_name, last_name, verified_at)
		SELECT 'bench-buyer-' || g || '@example.com', 'Buyer' || g, 'Surname' || g, NOW()
		FROM generate_series(1, $1) g
	`, sales)
	exec("sales", `
		INSERT INTO ticket_sales (event_id, organization_id, channel, payment_method, customer_email,
		                          customer_first_name, customer_last_name, customer_id,
		                          sold_at, confirmation_ref, status)
		SELECT $1, $2, 'online', 'cash', c.email, c.first_name, c.last_name, c.id,
		       TIMESTAMPTZ '2026-07-01 10:00:00+00' + (c.rn || ' seconds')::interval,
		       'BENCH-' || c.rn, 'active'
		FROM (
			SELECT id, email, first_name, last_name,
			       row_number() OVER (ORDER BY created_at, id) AS rn
			FROM customers WHERE email LIKE 'bench-buyer-%'
		) c
	`, eventID, orgID)
	exec("sale lines", `
		INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents)
		SELECT s.id, $2, $3, 2000
		FROM ticket_sales s WHERE s.event_id = $1
	`, eventID, ticketTypeID, benchTicketsPerSale)

	// THE HOLDERS, one per Ticket, each accepted. `accepted` is the state that
	// fills the most cells and it is the one an Organization running assignment
	// ends up with.
	exec("holder customers", `
		INSERT INTO customers (email, first_name, last_name, verified_at)
		SELECT 'bench-holder-' || g || '@example.com', 'Holder' || g, 'Family' || g, NOW()
		FROM generate_series(1, $1) g
	`, rows)
	exec("tickets", `
		INSERT INTO tickets (ticket_sale_line_id, ordinal, holder_email, holder_customer_id, assigned_at, accepted_at)
		SELECT l.id, o.ordinal, h.email, h.id, NOW(), NOW()
		FROM (
			SELECT l.id, row_number() OVER (ORDER BY l.created_at, l.id) AS rn
			FROM ticket_sale_lines l
			JOIN ticket_sales s ON s.id = l.ticket_sale_id
			WHERE s.event_id = $1
		) l
		CROSS JOIN generate_series(1, $2) AS o(ordinal)
		JOIN (
			SELECT id, email, row_number() OVER (ORDER BY created_at, id) AS rn
			FROM customers WHERE email LIKE 'bench-holder-%'
		) h ON h.rn = (l.rn - 1) * $2 + o.ordinal
	`, eventID, benchTicketsPerSale)

	// THE ANSWERS. A multiple-choice Answer is a row carrying no typed value plus
	// the chosen Option; the other kinds carry their one typed column. One chosen
	// Option per Ticket is not a saving — every Option of an answered question
	// gets a cell, TRUE for the chosen one and FALSE for the rest.
	exec("choice answers", `
		INSERT INTO ticket_answers (ticket_id, ticket_question_id)
		SELECT tk.id, q.id
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		JOIN ticket_questions q ON q.ticket_type_id = l.ticket_type_id
		WHERE s.event_id = $1 AND q.kind = 'multi_choice'
	`, eventID)
	exec("chosen options", `
		INSERT INTO ticket_answer_options (ticket_answer_id, ticket_question_option_id, option_label_snapshot, sort_order)
		SELECT a.id, o.id, o.label, o.sort_order
		FROM ticket_answers a
		JOIN ticket_questions q ON q.id = a.ticket_question_id
		JOIN LATERAL (
			SELECT id, label, sort_order FROM ticket_question_options
			WHERE ticket_question_id = q.id ORDER BY sort_order LIMIT 1
		) o ON TRUE
		WHERE q.kind = 'multi_choice'
	`)
	exec("typed answers", `
		INSERT INTO ticket_answers (ticket_id, ticket_question_id, text_value, number_value, date_value, boolean_value)
		SELECT tk.id, q.id,
		       CASE q.kind WHEN 'short_text' THEN 'Some answer text ' || tk.ordinal
		                   WHEN 'long_text' THEN 'A longer answer, of the length a person types into a text box, ' || tk.ordinal
		       END,
		       CASE q.kind WHEN 'number' THEN tk.ordinal END,
		       CASE q.kind WHEN 'date' THEN DATE '2026-07-01' END,
		       CASE q.kind WHEN 'checkbox' THEN (tk.ordinal % 2 = 0) END
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		JOIN ticket_questions q ON q.ticket_type_id = l.ticket_type_id
		WHERE s.event_id = $1 AND q.kind <> 'multi_choice'
	`, eventID)

	var seeded int
	if err := env.db.QueryRow(`
		SELECT COUNT(*) FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1
	`, eventID).Scan(&seeded); err != nil {
		t.Fatalf("count seeded tickets: %v", err)
	}
	if seeded != rows {
		t.Fatalf("seeded %d Tickets, wanted %d", seeded, rows)
	}
	// ANALYZE, because the planner has just been handed a million rows it has no
	// statistics for, and a measurement taken through a plan production would
	// never choose is not a measurement of production.
	exec("analyze", `ANALYZE tickets, ticket_sale_lines, ticket_sales, ticket_answers, ticket_answer_options, customers`)
}
