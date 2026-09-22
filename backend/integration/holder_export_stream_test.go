package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/peter/ticket_pos/backend/internal/server"
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
		// The file is attendee personal data: no cache between here and the
		// browser may keep a copy of it.
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("export %q Cache-Control = %q, want no-store", query, got)
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

	// A CHANGE OF HOLDER IS WRITTEN IN SQL BECAUSE THE API'S WAY TO ONE IS A
	// JOURNEY, NOT A REQUEST: the buyer signs in with a passcode, names the new
	// Holder, and the Holder accepts through the link the Assignment mail
	// carries. The bench fixture's buyers are seeded straight into the database
	// and have no way to sign in, and the pause this runs in holds the export
	// open on the server's goroutine, where a passcode round trip per Ticket
	// would only add mail and rate limits to a test whose subject is the
	// snapshot. What it needs is that a Ticket's holder sort key moves across
	// the export's position, and one UPDATE is exactly that.
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
	assertAbortLineIsSanitized(t, logs, finished)
}

// assertAbortLineIsSanitized holds an aborted export's finished line to what an
// abort record may carry: WARN, because somebody may have to look at it; an
// error CLASS, never the error's own text, which for a client that went away
// names the peer's address and port and for a wrapped error could name anyone;
// and a request id, the key it is joined to its started line on.
func assertAbortLineIsSanitized(t *testing.T, logs *captureLogger, finished capturedLine) {
	t.Helper()
	if finished.level != "warn" {
		t.Errorf("an aborted export's finished line is %s, want warn", finished.level)
	}
	if class, _ := finished.arg(t, "error_class").(string); class == "" {
		t.Error("an aborted export's finished line names no error class")
	}
	for i := 0; i+1 < len(finished.args); i += 2 {
		if finished.args[i] == "error" {
			t.Errorf("the finished line carries the raw error %v; it must carry its class only", finished.args[i+1])
		}
	}
	if strings.Contains(logs.rendered(), "127.0.0.1") {
		t.Errorf("a peer address reached the log:\n%s", logs.rendered())
	}
	if id, _ := finished.arg(t, "request_id").(string); id == "" {
		t.Error("the finished line carries no request id")
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
	const requestID = "gone-fest-download"
	req.Header.Set("X-Request-ID", requestID)
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
	assertAbortLineIsSanitized(t, logs, finished)
	for _, line := range []capturedLine{lines[0], finished} {
		if got := line.arg(t, "request_id"); got != requestID {
			t.Errorf("%s request_id = %v, want the request's own %q", line.msg, got, requestID)
		}
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
	// Each download runs on its own goroutine and hands its outcome back, and
	// the test's goroutine does the failing: t.Fatal from any other goroutine
	// stops only that goroutine.
	type outcome struct {
		status int
		err    error
	}
	download := func() <-chan outcome {
		result := make(chan outcome, 1)
		go func() {
			resp, data, err := fetchHolderExport(env.server.URL, f.sessionID, f.eventID, "")
			if err != nil {
				result <- outcome{err: err}
				return
			}
			if resp.StatusCode == http.StatusOK {
				wb, err := excelize.OpenReader(bytes.NewReader(data))
				if err != nil {
					result <- outcome{status: resp.StatusCode, err: fmt.Errorf("a released export is not a workbook: %w", err)}
					return
				}
				_ = wb.Close()
			}
			result <- outcome{status: resp.StatusCode}
		}()
		return result
	}
	awaitOK := func(name string, result <-chan outcome) {
		t.Helper()
		got := <-result
		if got.err != nil {
			t.Fatalf("the %s export failed: %v", name, got.err)
		}
		if got.status != http.StatusOK {
			t.Fatalf("the %s export ended %d, want 200", name, got.status)
		}
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
	// Most exports finish in seconds, so a few seconds is when trying again is
	// worth it.
	if got := resp.Header.Get("Retry-After"); got != "5" {
		t.Errorf("busy refusal Retry-After = %q, want 5", got)
	}
	// The standard error envelope, whole: data null, the error's code and a
	// message a person can read, and the request id the header carries.
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("busy refusal %s is not an envelope: %v", string(data), err)
	}
	if string(envelope.Data) != "null" {
		t.Errorf("busy refusal data = %s, want null", string(envelope.Data))
	}
	if envelope.Error == nil || envelope.Error.Code != "HOLDER_EXPORT_BUSY" {
		t.Fatalf("busy refusal = %s, want the HOLDER_EXPORT_BUSY envelope", string(data))
	}
	if envelope.Error.Message == "" {
		t.Error("busy refusal carries no message")
	}
	if envelope.RequestID == "" || envelope.RequestID != resp.Header.Get("X-Request-ID") {
		t.Errorf("busy refusal request_id = %q, X-Request-ID = %q; want the same non-empty id",
			envelope.RequestID, resp.Header.Get("X-Request-ID"))
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
	awaitOK("first", first)
	// One slot is free again: a new export is admitted while the second is still
	// held, and completes once let go.
	fourth := download()
	close(awaitHeld())
	awaitOK("fourth", fourth)
	close(releaseSecond)
	secondReleased = true
	awaitOK("second", second)
}

// productionChainServer serves the app through server.NewHandler, the handler
// cmd/server serves: the request pipeline (request id, request log, panic
// recovery) around every route. Two things about a streamed export are only
// true or false through that pipeline: whether a write deadline reaches the
// connection through the request log's ResponseWriter wrapper, and whether a
// panic part way through a file is turned into an envelope glued onto a 200.
//
// It differs from the shared harness server in two ways. EVERY CONNECTION IT
// ACCEPTS HAS A TINY SEND BUFFER, so a client that stops reading stalls the
// export's writes after a few KiB rather than after the several MiB the kernel
// would otherwise buffer on its behalf. And the pipeline logs to the returned
// capture, so a test can read the request log line an aborted export leaves.
func productionChainServer(t *testing.T) (*httptest.Server, *requestLogCapture) {
	t.Helper()
	requestLog := &requestLogCapture{}
	// NewHandler reads the app's logger once, when it builds the pipeline, so
	// the swap is in force for this handler only and undone before any other
	// test can build one.
	appLogger := sharedApp.Logger
	sharedApp.Logger = slog.New(slog.NewJSONHandler(requestLog, &slog.HandlerOptions{Level: slog.LevelDebug}))
	handler := server.NewHandler(sharedApp)
	sharedApp.Logger = appLogger

	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = smallSendBufferListener{srv.Listener}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, requestLog
}

// requestLogCapture collects the JSON lines the request pipeline writes.
type requestLogCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *requestLogCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *requestLogCapture) rendered() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// awaitRequestLine waits for the request log's "request" line for requestID.
// The line is written as the handler unwinds, which for an aborted download is
// after the client has already seen the connection break.
func (c *requestLogCapture) awaitRequestLine(t *testing.T, requestID string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, raw := range strings.Split(c.rendered(), "\n") {
			if raw == "" {
				continue
			}
			var line map[string]any
			if err := json.Unmarshal([]byte(raw), &line); err != nil {
				t.Fatalf("request log line %q is not JSON: %v", raw, err)
			}
			if line["msg"] == "request" && line["request_id"] == requestID {
				return line
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no request log line for %q; the request log was:\n%s", requestID, c.rendered())
	return nil
}

// assertRequestLineAborted holds the request log line of an export that broke
// its connection part way through a file to what the pipeline promises for an
// http.ErrAbortHandler: WARN, aborted=true, and the 200 that had gone out.
func assertRequestLineAborted(t *testing.T, requestLog *requestLogCapture, requestID string) {
	t.Helper()
	line := requestLog.awaitRequestLine(t, requestID)
	if line["level"] != "WARN" {
		t.Errorf("request line for %s is at %v, want WARN", requestID, line["level"])
	}
	if line["aborted"] != true {
		t.Errorf("request line for %s has aborted=%v, want true", requestID, line["aborted"])
	}
	if status, _ := line["status"].(float64); status != http.StatusOK {
		t.Errorf("request line for %s has status %v, want the 200 that had been sent", requestID, line["status"])
	}
}

type smallSendBufferListener struct{ net.Listener }

func (l smallSendBufferListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetWriteBuffer(4 << 10)
	}
	return conn, err
}

// stalledDownload starts a Holder Export whose client reads the response's
// headers and then nothing more, the way a phone on a dead network does. The
// returned body is never read until the test chooses to.
func stalledDownload(t *testing.T, serverURL, sessionID, eventID, requestID string) *http.Response {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetReadBuffer(4 << 10)
			}
			return conn, err
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	req, err := http.NewRequest(http.MethodGet, serverURL+holderExportPath(eventID), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sessionID)
	req.Header.Set("X-Request-ID", requestID)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("stalled download %s: %v", requestID, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stalled download %s status=%d, want the file to have begun", requestID, resp.StatusCode)
	}
	return resp
}

// A CLIENT THAT STOPS READING IS LET GO AT THE EXPORT'S DEADLINE (ADR 0075).
//
// Without a write deadline a write blocked on a full TCP window outlives the
// export's own deadline, because nothing but the peer can unblock it - and the
// export keeps its database connection and its slot for as long as the client
// stays silent. Two clients that stop reading fill both slots and a third
// export is refused; at the deadline both are cut off, each writes its finished
// line with reason `deadline`, and an export after them is admitted and
// completes.
func TestTheHolderExportLetsAStalledClientGoAtTheDeadline(t *testing.T) {
	const tickets = 1_200
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Stall Fest", "stall-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)
	srv, requestLog := productionChainServer(t)
	logs := withCatalogLogger(t)

	const deadline = 3 * time.Second
	sharedApp.CatalogService.WithHolderExportDeadline(deadline)
	t.Cleanup(func() { sharedApp.CatalogService.WithHolderExportDeadline(0) })

	began := time.Now()
	first := stalledDownload(t, srv.URL, sessionID, eventID, "stalled-one")
	second := stalledDownload(t, srv.URL, sessionID, eventID, "stalled-two")

	// Both slots are held by clients that are reading nothing.
	resp, data, err := fetchHolderExport(srv.URL, sessionID, eventID, "")
	if err != nil {
		t.Fatalf("third export: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("third export status=%d with both slots stalled, want 503; body=%.300s", resp.StatusCode, data)
	}
	var busy struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(data, &busy); err != nil || string(busy.Data) != "null" ||
		busy.Error == nil || busy.Error.Code != "HOLDER_EXPORT_BUSY" || busy.RequestID == "" {
		t.Fatalf("third export refusal = %.300s, want the HOLDER_EXPORT_BUSY envelope", data)
	}
	if time.Since(began) >= deadline {
		t.Fatal("the stalled exports reached their deadline before the refusal was checked; the test proves nothing")
	}

	finished := map[string]capturedLine{}
	waitUntil := time.Now().Add(deadline + 15*time.Second)
	for len(finished) < 2 && time.Now().Before(waitUntil) {
		for _, line := range logs.snapshot() {
			if line.msg == "holder export finished" {
				id, _ := line.arg(t, "request_id").(string)
				finished[id] = line
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, id := range []string{"stalled-one", "stalled-two"} {
		line, ok := finished[id]
		if !ok {
			t.Fatalf("export %s never logged its finished line: a stalled client held it past the deadline; log was:\n%s",
				id, logs.rendered())
		}
		if got := line.arg(t, "outcome"); got != "aborted" {
			t.Errorf("%s outcome = %v, want aborted", id, got)
		}
		if got := line.arg(t, "reason"); got != "deadline" {
			t.Errorf("%s reason = %v, want deadline", id, got)
		}
		assertAbortLineIsSanitized(t, logs, line)
		assertRequestLineAborted(t, requestLog, id)
	}
	if elapsed := time.Since(began); elapsed < deadline {
		t.Errorf("the stalled exports ended after %v, before their %v deadline", elapsed, deadline)
	}

	// The stalled bodies were cut, not finished.
	for name, stalled := range map[string]*http.Response{"first": first, "second": second} {
		if body, err := io.ReadAll(stalled.Body); err == nil {
			t.Errorf("the %s stalled download ended cleanly (%d bytes) after its deadline", name, len(body))
		}
	}

	// Their slots are free again.
	sharedApp.CatalogService.WithHolderExportDeadline(0)
	resp, data, err = fetchHolderExport(srv.URL, sessionID, eventID, "")
	if err != nil {
		t.Fatalf("export after the deadline: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export after the stalled ones were let go status=%d, want 200; body=%.300s", resp.StatusCode, data)
	}
	if got := holderExportTicketKeys(t, data); len(got) != tickets {
		t.Fatalf("export after the deadline carries %d rows, want %d", len(got), tickets)
	}
}

// A PANIC PART WAY THROUGH A FILE STILL WRITES THE FINISHED LINE, and still
// aborts the download rather than ending it: behind the production middleware
// a recovered panic would otherwise become a JSON envelope appended to a 200
// that then ends cleanly, which a browser saves as a file.
func TestTheHolderExportRecordsAndAbortsOnAPanicMidStream(t *testing.T) {
	const tickets = 1_200
	env := setupTest(t)
	enableTicketAssignment(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Panic Fest", "panic-fest")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, tickets+100)
	seedHolderExportBenchFixture(t, env, eventID, ticketTypeID, tickets)
	srv, requestLog := productionChainServer(t)
	logs := withCatalogLogger(t)

	withHolderExportPause(t, func(_ context.Context, rowsWritten int) {
		if rowsWritten == 500 {
			panic("injected mid-stream failure")
		}
	})

	resp, body, err := fetchHolderExport(srv.URL, sessionID, eventID, "")
	if resp != nil && resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d; the panic must come after streaming began", resp.StatusCode)
	}
	if err == nil {
		t.Fatalf("the download ended cleanly after a panic mid-stream (%d bytes)", len(body))
	}

	finished := waitForLine(t, logs, "holder export finished")
	if got := finished.arg(t, "outcome"); got != "aborted" {
		t.Errorf("finished outcome = %v, want aborted", got)
	}
	if got := finished.arg(t, "reason"); got != "panic" {
		t.Errorf("finished reason = %v, want panic", got)
	}
	if got := finished.arg(t, "row_count"); got != 500 {
		t.Errorf("finished row_count = %v, want the 500 rows produced before the panic", got)
	}
	assertAbortLineIsSanitized(t, logs, finished)
	if strings.Contains(logs.rendered(), "injected mid-stream failure") {
		t.Errorf("the panic's own message reached the log; only a runtime panic's is logged:\n%s", logs.rendered())
	}
	// The panic is still diagnosable: its stack is logged under the request id.
	panicked := logs.only(t, "holder export panicked")
	if panicked.level != "error" {
		t.Errorf("the panic line is %s, want error", panicked.level)
	}
	if stack, _ := panicked.arg(t, "stack").(string); !strings.Contains(stack, "streamHolderExport") {
		t.Errorf("the panic line's stack does not reach the export:\n%s", stack)
	}
	if got := panicked.arg(t, "request_id"); got != finished.arg(t, "request_id") {
		t.Errorf("the panic line's request_id = %v, the finished line's = %v", got, finished.arg(t, "request_id"))
	}
	// The request log still writes its line, as an abort and not as the 500 a
	// recovered panic would otherwise be recorded as.
	requestID, _ := finished.arg(t, "request_id").(string)
	assertRequestLineAborted(t, requestLog, requestID)
	if strings.Contains(requestLog.rendered(), "panic recovered") {
		t.Errorf("the abort reached the recover middleware as a panic to report:\n%s", requestLog.rendered())
	}
}
