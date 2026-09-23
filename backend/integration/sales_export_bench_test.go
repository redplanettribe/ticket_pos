package integration

import (
	"bytes"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// THE SALES EXPORT'S CAP, WEIGHED END TO END (#661, beside ADR 0075).
//
// The Sales Export keeps its ten-thousand-Sale cap, and its reason is symmetry
// with the Sale Import, not memory: an export can never hand back more sale rows
// than the importer would take. But the generation it bounds is still
// synchronous and still buffered whole, and the cap counts SALES while the
// per-Ticket sheet counts TICKETS. One Ticket Sale of forty Tickets is one row on
// the data sheet and forty on the other, so ten thousand Sales can be a per-Ticket
// sheet as tall as the Holder Export's old fifty-thousand cap - which needed
// 6.9 GiB at this question width, and 556 MiB even with no questions at all,
// against a 512Mi instance (#530). Nobody had ever measured
// whether this cap fits. This is the measurement.
//
// IT GOES OVER HTTP, for the reason the Holder Export's benchmark does: the sales
// query, the Ticket and Answer reads, the answer assembly, the workbook build and
// the response are one request's worth of memory held at once, and that sum is
// what api_memory (512Mi, terraform/envs/prod) has to hold.
//
// THE FIXTURE IS SEEDED WITH DIRECT SQL, in the shape the API would have left
// behind, for the same stated compromise as the Holder benchmark: ten thousand
// Sales through checkout or Sale Import would measure the importer and take far
// longer than the export does.
//
// MEMORY IS SAMPLED WHILE THE REQUEST IS IN FLIGHT, not only read after it. A
// reading taken after the handler returns sees a heap the workbook has already
// been released from, so a sampler polls the Go runtime every few milliseconds
// and keeps the high-water mark. The server runs in this process, so that mark
// is the handler's own; it also carries the client's copy of the response body,
// which is small beside the build and is reported so it can be subtracted.
//
// IT IS GUARDED BY AN ENVIRONMENT VARIABLE and therefore never part of `make
// test-integration`. Rerun it with:
//
//	SALES_EXPORT_BENCH=1 go test ./integration/ -run TestSalesExportAtTheCap -v -timeout 30m
//
// THE PEAK RSS, which is what the memory limit is actually about, is read by the
// test itself rather than by wrapping it in /usr/bin/time: on Linux the kernel's
// high-water mark (VmHWM in /proc/self/status) is reset before each run through
// /proc/self/clear_refs and read after it, so each run reports its own peak
// resident set and not the seeding's or the previous run's. Elsewhere it reports
// zero, and the Go runtime's numbers stand alone.
//
// SALES_EXPORT_BENCH_SALES overrides the height (default 10,000, the cap) and
// SALES_EXPORT_BENCH_RUNS how many exports are weighed (default 3).

// The question shape, the Holder benchmark's exactly - eight multiple-choice
// questions of twenty-five Options, five of them RETIRED and still taking
// columns, and six one-column questions - so the two measurements can be read
// against each other: the per-Ticket sheet here and the Holder sheet there are
// the same width of the same answers, and what differs is the height and what
// else the request holds. See holder_export_bench_test.go for why this shape is
// worst-case-but-plausible rather than a schema maximum.
const (
	salesBenchChoiceQuestions = 8
	salesBenchOptionsPer      = 25
	salesBenchRetiredPer      = 5
	salesBenchPlainQuestions  = 6
)

// salesBenchQuantitySQL is how many Tickets the g-th Sale is for.
//
// A MIX, and a heavy one: one Sale in a hundred is a group booking of forty -
// the ticket's own worst example - and the rest cycle through one to eight, the
// spread of a party buying for itself. Ten thousand Sales come to 48,700
// Tickets, averaging 4.87 per Sale, which puts the per-Ticket sheet at the Holder
// Export's old cap. The shape is a judgement about what a large Event looks like,
// stated here so a reader can disagree with the number and not only with the
// verdict.
const salesBenchQuantitySQL = `CASE WHEN g % 100 = 0 THEN 40 ELSE 1 + g % 8 END`

// TestSalesExportAtTheCap weighs a Sales Export at the row cap.
//
// Like its Holder sibling it asserts nothing about the numbers - a threshold is a
// benchmark that fails on a busy laptop - and asserts everything about the file:
// that it was built, that the data sheet carries every Sale and the per-Ticket
// sheet every Ticket, so no number is ever reported for a file that does not
// exist.
func TestSalesExportAtTheCap(t *testing.T) {
	if os.Getenv("SALES_EXPORT_BENCH") != "1" {
		t.Skip("the Sales Export cap benchmark is run by hand: SALES_EXPORT_BENCH=1 (#661)")
	}
	sales := benchEnvInt(t, "SALES_EXPORT_BENCH_SALES", 10_000)
	runs := benchEnvInt(t, "SALES_EXPORT_BENCH_RUNS", 3)

	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Sales Benchmark Fest", "sales-benchmark-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50*sales)

	seedStart := time.Now()
	tickets := seedSalesExportBenchFixture(t, env, eventID, ticketTypeID, sales)
	t.Logf("seeded %d Sales carrying %d Tickets in %s", sales, tickets, time.Since(seedStart).Round(time.Millisecond))

	type sample struct {
		elapsed  time.Duration
		peakHeap uint64
		peakSys  uint64
		peakRSS  uint64
		data     []byte
	}
	samples := make([]sample, 0, runs)
	for run := 0; run < runs; run++ {
		runtime.GC()
		var before runtime.MemStats
		runtime.ReadMemStats(&before)
		resetPeakRSS()
		stop := sampleMemory()
		start := time.Now()
		resp, data := downloadSalesExport(t, env, sessionID, eventID, "")
		elapsed := time.Since(start)
		peakHeap, peakSys := stop()
		peakRSS := peakRSS()
		if resp.StatusCode != 200 {
			t.Fatalf("run %d: status=%d body=%s", run, resp.StatusCode, string(data[:min(len(data), 500)]))
		}
		var after runtime.MemStats
		runtime.ReadMemStats(&after)
		samples = append(samples, sample{elapsed: elapsed, peakHeap: peakHeap, peakSys: peakSys, peakRSS: peakRSS, data: data})
		t.Logf(
			"run %d: %s wall, peak live heap %.0f MiB (%.0f MiB before), peak Go Sys %.0f MiB, peak RSS %.0f MiB, %.0f MiB allocated, %.1f MiB .xlsx",
			run+1, elapsed.Round(time.Millisecond),
			mib(peakHeap), mib(before.HeapAlloc), mib(peakSys), mib(peakRSS),
			mib(after.TotalAlloc-before.TotalAlloc), float64(len(data))/(1<<20),
		)
	}

	// The file is real, or the numbers above are about nothing. Streamed rather
	// than read with GetRows, for the Holder benchmark's reason: materialising
	// ten million cells to count them would cost more than the build it checks.
	last := samples[len(samples)-1].data
	dataColumns := verifySalesExportSheet(t, last, salesExportSheet, sales)
	answerColumns := verifySalesExportSheet(t, last, salesExportAnswersSheet, tickets)

	sorted := make([]time.Duration, 0, len(samples))
	var peakHeap, peakSys, peakRSS uint64
	for _, s := range samples {
		sorted = append(sorted, s.elapsed)
		peakHeap = max(peakHeap, s.peakHeap)
		peakSys = max(peakSys, s.peakSys)
		peakRSS = max(peakRSS, s.peakRSS)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	t.Logf(
		"VERDICT INPUT: %q %d rows x %d columns (%d cells), %q %d rows x %d columns (%d cells); "+
			"wall %s..%s (median %s) against a 300s request timeout; "+
			"peak live heap %.0f MiB, peak Go Sys %.0f MiB, peak RSS %.0f MiB against a 512Mi instance limit; %.1f MiB .xlsx",
		salesExportSheet, sales, dataColumns, sales*dataColumns,
		salesExportAnswersSheet, tickets, answerColumns, tickets*answerColumns,
		sorted[0].Round(time.Millisecond), sorted[len(sorted)-1].Round(time.Millisecond),
		sorted[len(sorted)/2].Round(time.Millisecond),
		mib(peakHeap), mib(peakSys), mib(peakRSS), float64(len(last))/(1<<20),
	)
}

// verifySalesExportSheet counts one sheet's data rows without materialising it,
// fails unless it carries want of them, and returns the header's width.
func verifySalesExportSheet(t *testing.T, data []byte, sheet string, want int) int {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	iter, err := f.Rows(sheet)
	if err != nil {
		t.Fatalf("stream %q (sheets: %v): %v", sheet, f.GetSheetList(), err)
	}
	defer func() { _ = iter.Close() }()
	rows, width := 0, 0
	for iter.Next() {
		if rows == 0 {
			header, err := iter.Columns()
			if err != nil {
				t.Fatalf("read %q header: %v", sheet, err)
			}
			width = len(header)
		}
		rows++
	}
	if err := iter.Error(); err != nil {
		t.Fatalf("stream %q rows: %v", sheet, err)
	}
	if rows-1 != want {
		t.Fatalf("%q carries %d rows, seeded %d", sheet, rows-1, want)
	}
	return width
}

// seedSalesExportBenchFixture stages the Event the export is weighed against and
// returns how many Tickets it minted: the questions and their Options, the
// Sales at their mixed quantities, a Ticket per unit, an accepted Holder per
// Ticket, and an Answer per Ticket per question.
//
// EVERY CELL THAT CAN BE FILLED IS FILLED, which is the worst case and, for
// required questions and a finished assignment round, the realistic one. Every
// buyer carries a Tax ID, so the data sheet's two Tax ID columns are not quietly
// blank; every Ticket is accepted by a Holder of its own, so the Holder columns
// hold distinct strings the shared-string table cannot dedupe away; and every
// question is answered, because an unanswered one writes no cells and would
// measure a narrower sheet than its header claims.
func seedSalesExportBenchFixture(t *testing.T, env *testEnv, eventID, ticketTypeID string, sales int) int {
	t.Helper()

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

	// THE QUESTIONS AND OPTIONS, approved outright and the last five Options of
	// each question retired, as in the Holder benchmark: the export reads
	// approved questions, and a retired Option keeps its column.
	exec("multi-choice questions", `
		INSERT INTO ticket_questions (ticket_type_id, label, kind, required, sort_order, review_status, approved_at, approved_by)
		SELECT $1, 'Multiple choice question ' || g, 'multi_choice', true, g, 'approved', NOW(), 'bench'
		FROM generate_series(1, $2) g
	`, ticketTypeID, salesBenchChoiceQuestions)
	exec("plain questions", `
		INSERT INTO ticket_questions (ticket_type_id, label, kind, required, sort_order, review_status, approved_at, approved_by)
		SELECT $1, 'Plain question ' || k.n, k.kind, true, 100 + k.n, 'approved', NOW(), 'bench'
		FROM (VALUES (1,'short_text'),(2,'short_text'),(3,'long_text'),(4,'number'),(5,'date'),(6,'checkbox'))
		     AS k(n, kind)
	`, ticketTypeID)
	exec("options", `
		INSERT INTO ticket_question_options (ticket_question_id, label, sort_order, review_status, approved_at, approved_by, retired_at)
		SELECT q.id, 'Question ' || q.sort_order || ' option ' || o, o, 'approved', NOW(), 'bench',
		       CASE WHEN o > $2 - $3 THEN NOW() ELSE NULL END
		FROM ticket_questions q
		CROSS JOIN generate_series(1, $2) o
		WHERE q.ticket_type_id = $1 AND q.kind = 'multi_choice'
	`, ticketTypeID, salesBenchOptionsPer, salesBenchRetiredPer)

	// THE BUYERS AND THEIR SALES. One Customer of record per Sale with a Tax ID
	// of its own, snapshotted onto the Sale as checkout would.
	exec("buyer customers", `
		INSERT INTO customers (email, first_name, last_name, tax_id_type, tax_id_number, verified_at)
		SELECT 'sales-bench-buyer-' || g || '@example.com', 'Buyer' || g, 'Surname' || g,
		       'cedula', lpad(g::text, 10, '0'), NOW()
		FROM generate_series(1, $1) g
	`, sales)
	exec("sales", `
		INSERT INTO ticket_sales (event_id, organization_id, channel, payment_method, customer_email,
		                          customer_first_name, customer_last_name, customer_id,
		                          customer_tax_id_type, customer_tax_id_number,
		                          sold_at, confirmation_ref, status)
		SELECT $1, $2, 'online', 'cash', c.email, c.first_name, c.last_name, c.id,
		       c.tax_id_type, c.tax_id_number,
		       TIMESTAMPTZ '2026-07-01 10:00:00+00' + (c.rn || ' seconds')::interval,
		       'SBENCH-' || c.rn, 'active'
		FROM (
			SELECT id, email, first_name, last_name, tax_id_type, tax_id_number,
			       row_number() OVER (ORDER BY created_at, id) AS rn
			FROM customers WHERE email LIKE 'sales-bench-buyer-%'
		) c
	`, eventID, orgID)
	// The quantity is keyed off the Sale's confirmation reference number, so the
	// mix is a function of the Sale and not of whatever order the rows come back.
	exec("sale lines", `
		INSERT INTO ticket_sale_lines (ticket_sale_id, ticket_type_id, quantity, unit_price_cents)
		SELECT s.id, $2, `+salesBenchQuantitySQL+`, 2000
		FROM (
			SELECT id, substring(confirmation_ref FROM 8)::int AS g
			FROM ticket_sales WHERE event_id = $1
		) s
	`, eventID, ticketTypeID)

	var tickets int
	if err := env.db.QueryRow(`
		SELECT SUM(l.quantity) FROM ticket_sale_lines l
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1
	`, eventID).Scan(&tickets); err != nil {
		t.Fatalf("sum quantities: %v", err)
	}

	// THE HOLDERS, one per Ticket, each accepted - the state that fills the most
	// cells, and the one an Organization running assignment ends up with.
	exec("holder customers", `
		INSERT INTO customers (email, first_name, last_name, verified_at)
		SELECT 'sales-bench-holder-' || g || '@example.com', 'Holder' || g, 'Family' || g, NOW()
		FROM generate_series(1, $1) g
	`, tickets)
	exec("tickets", `
		INSERT INTO tickets (ticket_sale_line_id, ordinal, holder_email, holder_customer_id, assigned_at, accepted_at)
		SELECT u.line_id, u.ordinal, h.email, h.id, NOW(), NOW()
		FROM (
			SELECT l.id AS line_id, o.ordinal,
			       row_number() OVER (ORDER BY l.created_at, l.id, o.ordinal) AS rn
			FROM ticket_sale_lines l
			JOIN ticket_sales s ON s.id = l.ticket_sale_id
			CROSS JOIN LATERAL generate_series(1, l.quantity) AS o(ordinal)
			WHERE s.event_id = $1
		) u
		JOIN (
			SELECT id, email, row_number() OVER (ORDER BY created_at, id) AS rn
			FROM customers WHERE email LIKE 'sales-bench-holder-%'
		) h ON h.rn = u.rn
	`, eventID)

	// THE ANSWERS, as the Holder benchmark writes them: one chosen Option per
	// multiple-choice question, which saves nothing because every Option of an
	// answered question gets a cell, and one typed value per other question.
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
		WHERE q.kind = 'multi_choice' AND q.ticket_type_id = $1
	`, ticketTypeID)
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
	if seeded != tickets {
		t.Fatalf("seeded %d Tickets, the Sales are for %d", seeded, tickets)
	}
	// ANALYZE, so the export's queries run through the plan production would pick
	// for tables this size rather than one chosen with no statistics at all.
	exec("analyze", `ANALYZE tickets, ticket_sale_lines, ticket_sales, ticket_answers, ticket_answer_options, customers`)
	return tickets
}
