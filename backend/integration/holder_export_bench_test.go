package integration

import (
	"bytes"
	"os"
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// THE HOLDER EXPORT'S CAP, MEASURED END TO END (#530, parent #518, ADR 0065).
//
// ADR 0065 set the fifty-thousand-Ticket cap as an explicit JUDGEMENT and named
// this measurement as the thing that settles it. The cap bounds a SYNCHRONOUS
// generation: the roster is queried, the Answers are assembled and the workbook
// is buffered whole in memory INSIDE the request, so what has to fit is one
// request's worth of all three at once — inside Cloud Run's
// api_request_timeout_seconds (300s, the module default, unoverridden in
// terraform/envs/prod) and, the tighter bound, inside api_memory (512Mi).
//
// IT GOES OVER HTTP, which is the whole point of doing it here rather than only
// in exportfile's BenchmarkHolderExportBuild: the query, the answer assembly, the
// workbook build and the response are all in one number, and that number is what
// the timeout actually measures. The build benchmark next door remains the
// instrument for the SHAPE of the curve, because it can sweep heights in seconds
// where this must seed a database first.
//
// THE FIXTURE IS SEEDED WITH DIRECT SQL and not through the API, and that is a
// deliberate compromise stated rather than hidden. Fifty thousand Tickets through
// checkout or Sale Import would take far longer than the measurement itself and
// would measure the importer. The rows are inserted in the shape the API would
// have left behind — active online sales, two Tickets per line, an accepted
// Holder per Ticket, an Answer per Ticket per question — so every query the
// export runs reads exactly what it reads in production.
//
// IT IS GUARDED BY AN ENVIRONMENT VARIABLE and therefore never part of `make
// test-integration`: seeding alone is over a million rows and the run holds
// several gigabytes. Rerun it with:
//
//	HOLDER_EXPORT_BENCH=1 go test ./integration/ -run TestHolderExportAtTheCap -v -timeout 60m
//
// and, for the peak RSS the memory limit is actually about:
//
//	HOLDER_EXPORT_BENCH=1 /usr/bin/time -v go test ./integration/ -run TestHolderExportAtTheCap -v -timeout 60m
//
// HOLDER_EXPORT_BENCH_ROWS overrides the height (default 50,000) and
// HOLDER_EXPORT_BENCH_RUNS how many exports are timed (default 3), because one
// sample is not a measurement.

// The WORST-CASE-BUT-PLAUSIBLE question shape, matching exportfile's build
// benchmark exactly so the two numbers can be subtracted from one another: eight
// multiple-choice questions of twenty-five Options each — five of them RETIRED,
// which still take columns — and six questions of the kinds that take one column.
//
// The shape is chosen from what an Organization could actually do rather than
// from what the schema permits. A multi-day conference asks a workshop track, a
// dietary requirement, a t-shirt size, an arrival day and a merch choice, and a
// twenty-five-Option list is one of those after two years of additions and
// retirements. A WIDER SHAPE WOULD BE WORSE and nothing measured here covers it.
const (
	benchChoiceQuestions = 8
	benchOptionsPer      = 25
	benchRetiredPer      = 5
	benchPlainQuestions  = 6
	benchTicketsPerSale  = 2
)

// TestHolderExportAtTheCap times and weighs a Holder Export at the row cap.
//
// It asserts nothing about the numbers. A threshold here would be a benchmark
// that fails on a busy laptop and passes on an idle one, and the comparison that
// matters — against a deployed timeout and a deployed memory limit on a machine
// that is neither — is a judgement for the person reading the output, recorded on
// the ticket. What it DOES assert is that the export succeeded and carries the
// rows it was given, so a number can never be reported for a file that was never
// built.
func TestHolderExportAtTheCap(t *testing.T) {
	if os.Getenv("HOLDER_EXPORT_BENCH") != "1" {
		t.Skip("the Holder Export cap benchmark is run by hand: HOLDER_EXPORT_BENCH=1 (#530)")
	}
	rows := benchEnvInt(t, "HOLDER_EXPORT_BENCH_ROWS", 50_000)
	runs := benchEnvInt(t, "HOLDER_EXPORT_BENCH_RUNS", 3)

	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Benchmark Fest", "benchmark-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, rows+1000)

	seedStart := time.Now()
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, rows)
	t.Logf("seeded %d Tickets in %s", rows, time.Since(seedStart).Round(time.Millisecond))

	type sample struct {
		elapsed time.Duration
		heap    uint64
		sys     uint64
		data    []byte
	}
	samples := make([]sample, 0, runs)
	for run := 0; run < runs; run++ {
		// GC first so the heap reading is this export's and not the last one's
		// litter. Sys is never returned to the OS promptly, so it accumulates
		// across runs and is reported as a high-water mark rather than per run.
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		resp, data := downloadHolderExport(t, env, sessionID, eventID, "")
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if resp.StatusCode != 200 {
			t.Fatalf("run %d: status=%d body=%s", run, resp.StatusCode, string(data[:min(len(data), 500)]))
		}
		samples = append(samples, sample{
			elapsed: elapsed,
			heap:    after.HeapAlloc,
			sys:     after.Sys,
			data:    data,
		})
		t.Logf(
			"run %d: %s wall, %.0f MiB heap after, %.0f MiB sys, %.0f MiB allocated, %.1f MiB .xlsx",
			run+1, elapsed.Round(time.Millisecond),
			mib(after.HeapAlloc), mib(after.Sys), mib(after.TotalAlloc-before.TotalAlloc),
			float64(len(data))/(1<<20),
		)
	}

	// The file is real, or the numbers above are about nothing.
	//
	// STREAMED AND NOT read with the suite's openHolderExport helper, which calls
	// GetRows: at eleven million cells that materialises the whole sheet as
	// [][]string a second time, and the VERIFICATION would then cost several times
	// the measurement it is checking. Only the header row's cells are decoded —
	// the width is the dimension this is all about — and the rest are counted.
	wantColumns := 13 + benchChoiceQuestions*benchOptionsPer + benchPlainQuestions
	verifyHolderExportShape(t, samples[len(samples)-1].data, rows, wantColumns)

	sorted := make([]time.Duration, 0, len(samples))
	var peakSys, peakHeap uint64
	for _, s := range samples {
		sorted = append(sorted, s.elapsed)
		peakSys = max(peakSys, s.sys)
		peakHeap = max(peakHeap, s.heap)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	t.Logf(
		"VERDICT INPUT: %d rows x %d columns; wall %s..%s (median %s) against a 300s request timeout; "+
			"peak live heap %.0f MiB, peak Go Sys %.0f MiB against a 512Mi instance limit",
		rows, wantColumns,
		sorted[0].Round(time.Millisecond), sorted[len(sorted)-1].Round(time.Millisecond),
		sorted[len(sorted)/2].Round(time.Millisecond),
		mib(peakHeap), mib(peakSys),
	)
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

func mib(bytes uint64) float64 { return float64(bytes) / (1 << 20) }

func benchEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q is not a positive number", name, raw)
	}
	return n
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
