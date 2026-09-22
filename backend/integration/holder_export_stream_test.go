package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// The Holder Export STREAMS AND HAS NO CAP (#658, parent #655, ADR 0075).
//
// The file is the same file; what changed is that it is written from a cursor
// inside one snapshot straight into the response. These tests are about what
// that makes true at the HTTP seam: a roster of any size arrives whole, a change
// made while it downloads cannot reach it, and a download that fails part way
// leaves nothing a spreadsheet program would open.

// withHolderExportPause installs the export's test-only pause point for one test.
func withHolderExportPause(t *testing.T, pause func(ctx context.Context, rowsWritten int)) {
	t.Helper()
	sharedApp.CatalogService.WithHolderExportPause(pause)
	t.Cleanup(func() { sharedApp.CatalogService.WithHolderExportPause(nil) })
}

// holderExportTicketKeys streams a downloaded workbook's data sheet and returns
// every row's confirmation_ref and ticket_ordinal as one key, in file order -
// streamed rather than read with GetRows, so a roster of thousands of wide rows
// is not materialised twice to be checked.
func holderExportTicketKeys(t *testing.T, data []byte) []string {
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
	var keys []string
	refCol, ordinalCol := -1, -1
	for iter.Next() {
		cells, err := iter.Columns()
		if err != nil {
			t.Fatalf("read row: %v", err)
		}
		if refCol < 0 {
			for i, h := range cells {
				switch h {
				case "confirmation_ref":
					refCol = i
				case "ticket_ordinal":
					ordinalCol = i
				}
			}
			if refCol < 0 || ordinalCol < 0 {
				t.Fatalf("header %v lacks confirmation_ref or ticket_ordinal", cells)
			}
			continue
		}
		keys = append(keys, cells[refCol]+"#"+cells[ordinalCol])
	}
	if err := iter.Error(); err != nil {
		t.Fatalf("stream rows: %v", err)
	}
	return keys
}

// seededHolderKeys is every Ticket of the Event as holderExportTicketKeys spells
// it, read from the database, sorted.
func seededHolderKeys(t *testing.T, env *testEnv, eventID string) []string {
	t.Helper()
	rows, err := env.db.Query(`
		SELECT s.confirmation_ref, tk.ordinal FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_sales s ON s.id = l.ticket_sale_id
		WHERE s.event_id = $1 AND s.status = 'active'
	`, eventID)
	if err != nil {
		t.Fatalf("read seeded tickets: %v", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var ref string
		var ordinal int
		if err := rows.Scan(&ref, &ordinal); err != nil {
			t.Fatalf("scan: %v", err)
		}
		keys = append(keys, ref+"#"+strconv.Itoa(ordinal))
	}
	sort.Strings(keys)
	return keys
}

// assertSameTickets fails unless the file carries every expected Ticket exactly
// once and nothing else.
func assertSameTickets(t *testing.T, got, want []string) {
	t.Helper()
	seen := make(map[string]int, len(got))
	for _, key := range got {
		seen[key]++
		if seen[key] == 2 {
			t.Errorf("Ticket %s appears more than once in the file", key)
		}
	}
	for _, key := range want {
		if seen[key] == 0 {
			t.Errorf("Ticket %s is missing from the file", key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("file carries %d rows, want %d", len(got), len(want))
	}
}

// ABOVE THE OLD 2,000, THE WHOLE ROSTER ARRIVES - unfiltered, and under a
// filter and a sort that still match every Ticket. Every seeded Ticket is in the
// file once, and the Info sheet states the rows actually there.
func TestTheHolderExportCarriesEveryTicketWellAboveTheOldCap(t *testing.T) {
	const tickets = 2_600
	env := setupTest(t)
	enableTicketQuestions(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Big Fest", "big-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)
	want := seededHolderKeys(t, env, eventID)

	for _, query := range []string{"", "assignment_state=accepted&sort=holder&dir=desc"} {
		resp, data := downloadHolderExport(t, env, sessionID, eventID, query)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("export %q status=%d body=%s", query, resp.StatusCode, string(data[:min(len(data), 500)]))
		}
		assertSameTickets(t, holderExportTicketKeys(t, data), want)

		f, err := excelize.OpenReader(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		info, err := f.GetRows("Info")
		_ = f.Close()
		if err != nil {
			t.Fatalf("info rows: %v", err)
		}
		found := false
		for _, row := range info {
			if len(row) > 0 && row[0] == fmt.Sprintf("Rows: %d Tickets", tickets) {
				found = true
			}
		}
		if !found {
			t.Errorf("export %q: the Info sheet does not state %d rows: %v", query, tickets, info)
		}
	}
}

// THE FILE IS ONE MOMENT. With the first batch of rows written, a sale is
// committed and two Tickets change Holder in ways that move them across the
// point the export has reached under the holder sort - one from the end to the
// front, one from the front to the end. Read page by page, the first would be
// lost and the second repeated; read inside the snapshot, the file is exactly
// the roster as it stood, and the new sale is not in it.
func TestTheHolderExportIsOneSnapshot(t *testing.T) {
	const tickets = 1_200 // three batches of the cursor
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Snapshot Fest", "snapshot-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)
	before := seededHolderKeys(t, env, eventID)

	reassign := func(holderLastName, toLastName string) {
		t.Helper()
		var customerID string
		if err := env.db.QueryRow(`
			INSERT INTO customers (email, first_name, last_name, verified_at)
			VALUES ($1, 'Moved', $2, NOW()) RETURNING id
		`, "moved-"+toLastName+"@example.com", toLastName).Scan(&customerID); err != nil {
			t.Fatalf("new holder: %v", err)
		}
		res, err := env.db.Exec(`
			UPDATE tickets SET holder_customer_id = $1, holder_email = $2
			WHERE holder_customer_id = (SELECT id FROM customers WHERE last_name = $3)
		`, customerID, "moved-"+toLastName+"@example.com", holderLastName)
		if err != nil {
			t.Fatalf("reassign %s: %v", holderLastName, err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			t.Fatalf("reassign %s touched %d Tickets, want 1", holderLastName, n)
		}
	}

	changed := false
	withHolderExportPause(t, func(_ context.Context, rowsWritten int) {
		if rowsWritten != 500 || changed {
			return
		}
		changed = true
		// "Family999" sorts late and "Family1" first; each is moved to the other
		// end of the order, across the rows already written.
		reassign("Family999", "Aardvark")
		reassign("Family1", "Zzyzx")
		commitBatch(t, env, sessionID, eventID, "mid-export-sale", []map[string]any{{
			"customer_email": "latecomer@example.com", "customer_first_name": "Late", "customer_last_name": "Comer",
			"ticket_type_id": ticketTypeID, "quantity": 2, "payment_method": "cash",
			"sold_at": "2026-07-02T10:00:00Z",
		}})
	})

	resp, data := downloadHolderExport(t, env, sessionID, eventID, "sort=holder")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export status=%d body=%s", resp.StatusCode, string(data[:min(len(data), 500)]))
	}
	if !changed {
		t.Fatal("the pause never fired after the first batch; nothing was changed mid-export")
	}
	assertSameTickets(t, holderExportTicketKeys(t, data), before)
	assertAbsentFromExport(t, data, "latecomer@example.com")
	assertAbsentFromExport(t, data, "moved-Aardvark@example.com")

	// And the change is real: the next export sees it.
	after := holderExportTicketKeys(t, func() []byte {
		_, data := downloadHolderExport(t, env, sessionID, eventID, "sort=holder")
		return data
	}())
	if len(after) != tickets+2 {
		t.Fatalf("the export after the change carries %d rows, want %d", len(after), tickets+2)
	}
}

// A FAILURE AFTER STREAMING BEGINS ABORTS THE RESPONSE. The export's database
// connection is killed with its first batch written: the download does not end
// cleanly, and whatever bytes arrived are not a workbook anybody can open.
func TestTheHolderExportAbortsRatherThanFinishingAShortFile(t *testing.T) {
	const tickets = 1_200
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Abort Fest", "abort-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)

	logs := withCatalogLogger(t)
	killed := false
	withHolderExportPause(t, func(_ context.Context, rowsWritten int) {
		if rowsWritten != 500 {
			return
		}
		var n int
		if err := env.db.QueryRow(`
			SELECT COUNT(pg_terminate_backend(pid)) FROM pg_stat_activity
			WHERE query LIKE 'FETCH FORWARD % FROM holder_export' AND pid <> pg_backend_pid()
		`).Scan(&n); err != nil {
			t.Errorf("terminate the export's connection: %v", err)
		}
		killed = n == 1
	})

	req, err := http.NewRequest(http.MethodGet, env.server.URL+holderExportPath(eventID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sessionID)
	var body []byte
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		body, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%d before the failure; the abort must come after streaming began", resp.StatusCode)
		}
	}
	if !killed {
		t.Fatal("the export's database connection was not found and killed; nothing was injected")
	}
	if err == nil {
		t.Fatalf("the download ended cleanly after a mid-stream database failure (%d bytes)", len(body))
	}
	if f, openErr := excelize.OpenReader(bytes.NewReader(body)); openErr == nil {
		_ = f.Close()
		t.Fatal("the bytes of an aborted download open as a workbook")
	}

	// And the record says so, with its own reason (#659).
	finished := waitForLine(t, logs, "holder export finished")
	if got := finished.arg(t, "outcome"); got != "aborted" {
		t.Errorf("finished outcome = %v, want aborted", got)
	}
	if got := finished.arg(t, "reason"); got != "database_error" {
		t.Errorf("finished reason = %v, want database_error", got)
	}
}

// waitForLine polls the captured log until a line containing fragment appears:
// an aborted export logs from the server's goroutine after the client has gone.
func waitForLine(t *testing.T, logs *captureLogger, fragment string) capturedLine {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range logs.snapshot() {
			if strings.Contains(line.msg, fragment) {
				return line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %q line was logged; log was:\n%s", fragment, logs.rendered())
	return capturedLine{}
}

// A CLIENT THAT GOES AWAY MID-STREAM is recorded as an abort, with the rows
// that had gone and the reason, after a started line that carried the search
// as a boolean and never its term (#659).
func TestTheHolderExportLogsAClientThatWentAway(t *testing.T) {
	const tickets = 1_200
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Gone Fest", "gone-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)
	logs := withCatalogLogger(t)

	// Every buyer's address contains it, so the search narrows nothing and the
	// file streams long enough to be walked away from.
	const term = "bench-buyer"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	paused := make(chan struct{})
	withHolderExportPause(t, func(exportCtx context.Context, rowsWritten int) {
		if rowsWritten != 500 {
			return
		}
		close(paused)
		select {
		case <-exportCtx.Done():
		case <-time.After(10 * time.Second):
			t.Errorf("the export never noticed its client had gone")
		}
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		env.server.URL+holderExportPath(eventID)+"?q="+term, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sessionID)
	done := make(chan error, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		done <- err
	}()
	select {
	case <-paused:
	case <-time.After(30 * time.Second):
		t.Fatal("the export never reached its first batch")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("the download completed although its client walked away")
	}

	finished := waitForLine(t, logs, "holder export finished")
	if got := finished.arg(t, "outcome"); got != "aborted" {
		t.Errorf("finished outcome = %v, want aborted", got)
	}
	if got := finished.arg(t, "row_count"); got != 500 {
		t.Errorf("finished row_count = %v, want the 500 rows sent before the client went", got)
	}
	if got := finished.arg(t, "reason"); got != "client_gone" {
		t.Errorf("finished reason = %v, want client_gone", got)
	}
	lines := logs.snapshot()
	if len(lines) == 0 || lines[0].msg != "holder export started" {
		t.Fatalf("the started line must precede the finished one; log was:\n%s", logs.rendered())
	}
	if got := lines[0].arg(t, "search"); got != true {
		t.Errorf("started search = %v, want the boolean true", got)
	}
	if strings.Contains(logs.rendered(), term) {
		t.Fatalf("the search term reached the log:\n%s", logs.rendered())
	}
}

// AT MOST TWO HOLDER EXPORTS STREAM AT ONCE on an instance (#660, ADR 0075).
// With two held open, a third is refused before any file byte with a retryable
// busy refusal that is not VALIDATION_FAILED, and once one of the two finishes
// a new export succeeds.
func TestTheHolderExportRefusesAThirdAtOnceAndNotAfter(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	f := newHolderFilterFixture(t, env)
	logs := withCatalogLogger(t)

	// Each export pauses as its file begins and hands the test the channel that
	// lets it go on.
	held := make(chan chan struct{}, 4)
	withHolderExportPause(t, func(ctx context.Context, rowsWritten int) {
		if rowsWritten != 0 {
			return
		}
		release := make(chan struct{})
		held <- release
		select {
		case <-release:
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
		}
	})
	download := func() <-chan int {
		result := make(chan int, 1)
		go func() {
			resp, data := downloadHolderExport(t, env, f.sessionID, f.eventID, "")
			if resp.StatusCode == http.StatusOK {
				if _, err := excelize.OpenReader(bytes.NewReader(data)); err != nil {
					t.Errorf("a released export is not a workbook: %v", err)
				}
			}
			result <- resp.StatusCode
		}()
		return result
	}
	awaitHeld := func() chan struct{} {
		t.Helper()
		select {
		case release := <-held:
			return release
		case <-time.After(30 * time.Second):
			t.Fatal("an export never began streaming")
			return nil
		}
	}

	first := download()
	releaseFirst := awaitHeld()
	second := download()
	releaseSecond := awaitHeld()
	secondReleased := false
	t.Cleanup(func() {
		if !secondReleased {
			close(releaseSecond)
		}
	})

	resp, data := downloadHolderExport(t, env, f.sessionID, f.eventID, "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("third export status=%d, want 503; body=%s", resp.StatusCode, string(data))
	}
	if strings.HasPrefix(string(data), "PK") {
		t.Fatal("the busy refusal carried file bytes")
	}
	if got := resp.Header.Get("Retry-After"); got == "" {
		t.Error("the busy refusal says nothing about when to retry")
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Error.Code != "HOLDER_EXPORT_BUSY" {
		t.Fatalf("busy refusal = %s, want the HOLDER_EXPORT_BUSY envelope", string(data))
	}
	// Two exports started; the refused third wrote no audit line.
	started := 0
	for _, line := range logs.snapshot() {
		if line.msg == "holder export started" {
			started++
		}
	}
	if started != 2 {
		t.Errorf("logged %d started lines, want the two admitted exports only", started)
	}

	close(releaseFirst)
	if status := <-first; status != http.StatusOK {
		t.Fatalf("the first export ended %d, want 200", status)
	}
	// One slot is free again: a new export is admitted while the second is still
	// held, and completes once let go.
	fourth := download()
	close(awaitHeld())
	if status := <-fourth; status != http.StatusOK {
		t.Fatalf("an export after one finished ended %d, want 200", status)
	}
	close(releaseSecond)
	secondReleased = true
	if status := <-second; status != http.StatusOK {
		t.Fatalf("the second export ended %d, want 200", status)
	}
}
