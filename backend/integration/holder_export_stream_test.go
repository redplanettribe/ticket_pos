package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"testing"

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
}
