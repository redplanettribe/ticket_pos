package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// The Holder reaches the Sales Export (#330, parent #322, ADR 0047): the
// per-Ticket sheet gains the person beside what the person answered, so that
// "who is coming and what size are they" is one sheet rather than two files an
// Organizer joins by hand.
//
// A SEPARATE FILE FROM sales_export_answers_test.go, whose helpers it reuses,
// because the two are about different promises. That file is about the shape of
// a sheet; this one is about a DISCLOSURE, and the assertions that matter most
// here are the ones about what is NOT in the file.
//
// THE NEGATIVE ASSERTIONS ARE THE POINT. ADR 0047 rated "disclose
// assigned-but-not-accepted addresses too" as the line the whole design is drawn
// around: an address a buyer typed for a friend who never clicked has no consent
// moment behind it, and the person may not even know a ticket was bought for
// them. Once a Sales Export carries it, it is forwarded and kept somewhere the
// platform cannot reach, log or revoke — so the test that the address is absent
// is asserted against the RAW BYTES OF THE WHOLE WORKBOOK, not against a named
// cell. A cell assertion passes while the address sits on another sheet.

// exportHolderFixture is #325's assignment fixture with the Sales Export in
// view: Ana holds two Tickets and Bruno one, on an Event that asks a T-shirt
// size, with Ana signed in and about to name her friends.
type exportHolderFixture struct {
	assignmentFixture
}

func newExportHolderFixture(t *testing.T, env *testEnv) exportHolderFixture {
	t.Helper()
	return exportHolderFixture{assignmentFixture: newAssignmentFixture(t, env)}
}

// acceptAsHolder walks the whole of what a Holder does: the mail is opened, the
// link is pressed, a name is given and a question is answered — every step
// through the door the Holder actually uses, with no session and no SQL.
//
// It is deliberately not a shortcut. What this file is asserting is that the
// export shows a Holder BECAUSE THEY ACCEPTED, and a fixture that wrote
// accepted_at itself would be asserting against its own UPDATE.
func acceptAsHolder(t *testing.T, env *testEnv, address, firstName, lastName, questionID, answer string) {
	t.Helper()
	token := assignmentTokenFrom(t, assignmentMailFor(t, env, address))
	acceptAssignmentOK(t, env, token)

	resp, body, _ := answerLinkRequest(t, env, http.MethodPut, assignmentLinkNamePath, map[string]any{
		"token": token, "first_name": firstName, "last_name": lastName,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s naming themselves: status=%d error=%+v", address, resp.StatusCode, body.Error)
	}
	resp, body, _ = answerLinkRequest(t, env, http.MethodPut, assignmentLinkQuestionPath+questionID,
		map[string]any{"token": token, "text": answer})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s answering their own question: status=%d error=%+v", address, resp.StatusCode, body.Error)
	}
}

// rowInState finds the one row of the sheet in a given assignment state.
//
// Keyed on the STATE rather than on a row number because the sheet's order is
// the data sheet's order, which is the Sales list's order, which is a filter's
// business and not this test's. Each fixture below puts exactly one Ticket in
// each state, so the state is an identity here.
func (s answersSheet) rowInState(t *testing.T, state string) int {
	t.Helper()
	found := -1
	for i := range s.rows {
		if s.cell(t, i, "assignment_state") == state {
			if found >= 0 {
				t.Fatalf("two rows read %q; this fixture puts one Ticket in each state", state)
			}
			found = i
		}
	}
	if found < 0 {
		t.Fatalf("no row reads %q", state)
	}
	return found
}

// assertWorkbookNeverSays fails if a string appears anywhere in the file — any
// sheet, any cell, any shared string.
//
// RAW BYTES AND NOT CELLS, deliberately. An .xlsx is a zip, so a plain
// bytes.Contains would miss a compressed string; the whole workbook is therefore
// read back through excelize and every cell of every sheet is walked. That is
// what makes this an assertion about the FILE somebody forwards rather than
// about the one sheet this ticket happened to edit.
func assertWorkbookNeverSays(t *testing.T, data []byte, forbidden, why string) {
	t.Helper()
	for _, sheet := range sheetNames(t, data) {
		rows := rowsOfSheet(t, data, sheet)
		for r, row := range rows {
			for c, cell := range row {
				if strings.Contains(cell, forbidden) {
					t.Fatalf("the Sales Export contains %q, on sheet %q at row %d column %d.\n%s",
						forbidden, sheet, r+1, c+1, why)
				}
			}
		}
	}
}

// rowsOfSheet reads one sheet of a downloaded workbook.
func rowsOfSheet(t *testing.T, data []byte, sheet string) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("open .xlsx: %v", err)
	}
	defer func() { _ = f.Close() }()
	rows, err := f.GetRows(sheet)
	if err != nil {
		t.Fatalf("get rows from %q: %v", sheet, err)
	}
	return rows
}

// THE ACCEPTANCE CRITERION, IN ONE WALK: a buyer names two friends, one of them
// clicks, and the Organizer downloads a sheet with a person and a t-shirt size
// on the same row.
func TestSalesExportCarriesTheHolderBesideTheAnswers(t *testing.T) {
	env := setupTest(t)
	f := newExportHolderFixture(t, env)

	// Carla is named and clicks. Dora is named and never does — the ordinary
	// case, and the one the file has to be careful about.
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[1], "dora@example.com")
	acceptAsHolder(t, env, "carla@example.com", "Carla", "Ruiz", f.sizeQuestion.ID, "L")
	// Bruno's Ticket is never assigned at all: the third state, and the one every
	// Ticket on this platform is in today.

	data := downloadOK(t, env, f.staffSession, f.eventID, "")
	sheet := openSalesExportAnswers(t, data)

	// The four columns, in the order the glossary commits to: the state, then the
	// person, then what the person said. The person sits BEFORE the questions
	// because the sheet is read left to right and "who is coming" comes before
	// "what size are they" — after them, a wide Event's Holder columns would be
	// off the right edge, which is the same as being in a second file.
	wantHeader := []string{
		"confirmation_ref", "ticket_type",
		"assignment_state", "holder_first_name", "holder_last_name", "holder_email",
		f.sizeQuestion.Label, f.extraQuestion.Label,
	}
	if !equalStrings(sheet.header, wantHeader) {
		t.Fatalf("header = %v, want %v", sheet.header, wantHeader)
	}
	if len(sheet.rows) != 3 {
		t.Fatalf("rows = %d, want one per Ticket of both sales", len(sheet.rows))
	}

	// ACCEPTED: the whole person, and their size, on one row. This is the sheet
	// the ticket exists to produce.
	accepted := sheet.rowInState(t, "accepted")
	for _, want := range []struct{ column, value string }{
		{"holder_first_name", "Carla"},
		{"holder_last_name", "Ruiz"},
		{"holder_email", "carla@example.com"},
		{f.sizeQuestion.Label, "L"},
		{"confirmation_ref", f.anaRef},
	} {
		if got := sheet.cell(t, accepted, want.column); got != want.value {
			t.Errorf("accepted row %s = %q, want %q", want.column, got, want.value)
		}
	}

	// ASSIGNED: the word, and three blanks where Dora's address is not.
	//
	// Blank rather than omitted and rather than a placeholder: an Organizer
	// filtering this sheet on "holder_email is blank" is asking "who has not
	// claimed their ticket yet", and both of the rows below must come back.
	assigned := sheet.rowInState(t, "assigned")
	unassigned := sheet.rowInState(t, "unassigned")
	for _, row := range []struct {
		name string
		at   int
	}{{"assigned", assigned}, {"unassigned", unassigned}} {
		for _, column := range []string{"holder_first_name", "holder_last_name", "holder_email"} {
			if got := sheet.cell(t, row.at, column); got != "" {
				t.Errorf("%s row %s = %q, want a blank cell", row.name, column, got)
			}
		}
	}

	// AND THE ADDRESS IS NOWHERE IN THE FILE. Not on this sheet under another
	// heading, not on the data sheet beside the buyer, not on the Info sheet.
	assertWorkbookNeverSays(t, data, "dora@example.com",
		"Dora was named by a buyer and has never accepted. ADR 0047: an address with no\n"+
			"consent moment behind it is not the Organization's to see, and this is the line the\n"+
			"whole feature is drawn around.")

	// The Info sheet tells the reader both halves, because a reader who did not
	// download the file cannot read either off the columns: what the new columns
	// are, and why an address they know they typed is not among them.
	info := openSalesExportInfo(t, data)
	info.says(t, "assignment_state")
	info.says(t, "not accepted")
}

// TestSalesExportDataSheetIsUnchangedByHolders: the money stays summable.
//
// The data sheet is one row per Ticket SALE. Ana's sale of two tickets is ONE
// row with ONE amount on it, whoever the two tickets end up belonging to — and
// naming a Holder must not flatten a sale into its Tickets, because the first
// thing anybody does with the amount column is sum it.
func TestSalesExportDataSheetIsUnchangedByHolders(t *testing.T) {
	env := setupTest(t)
	f := newExportHolderFixture(t, env)

	before := rowsOfSheet(t, downloadOK(t, env, f.staffSession, f.eventID, ""), salesExportSheet)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAsHolder(t, env, "carla@example.com", "Carla", "Ruiz", f.sizeQuestion.ID, "L")

	data := downloadOK(t, env, f.staffSession, f.eventID, "")
	after := rowsOfSheet(t, data, salesExportSheet)
	if len(before) != len(after) {
		t.Fatalf("data sheet rows = %d, was %d — a sale stays one row however many of its Tickets found a Holder",
			len(after), len(before))
	}
	for i := range before {
		if !equalStrings(before[i], after[i]) {
			t.Fatalf("data sheet row %d = %v, was %v — the data sheet is unchanged, column for column",
				i, after[i], before[i])
		}
	}

	// And the Holder is not on it. The data sheet's `customer_*` trio is the
	// BUYER, who paid and who the Reversal Window belongs to; a Holder is neither
	// (ADR 0046 — assignment, never transfer), and a Holder's address appearing
	// under a customer heading would be the file asserting a transfer that never
	// happened.
	sales := openSalesExport(t, data)
	if got := sales.value(t, 0, "customer_email"); got == "carla@example.com" {
		t.Fatalf("customer_email = %q on the data sheet — accepting moves no Sale, no money and no buyer", got)
	}
}

// TestSalesExportHolderColumnsAppearOnlyWhileAssignmentIsOpen: with
// TICKET_ASSIGNMENT_ENABLED closed the workbook is the one #314 built, column
// for column.
//
// The flag is closed on every deployment until a Policy Version describes this
// disclosure (ADR 0045), so this is the state the file is actually in today —
// and this is the test that says the flag is a real off switch rather than a
// hidden authoring surface. Note what the fixture does first: a Holder REALLY
// ACCEPTS, so there is a genuine disclosure sitting in the database for the
// closed flag to withhold.
func TestSalesExportHolderColumnsAppearOnlyWhileAssignmentIsOpen(t *testing.T) {
	env := setupTest(t)
	f := newExportHolderFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "carla@example.com")
	acceptAsHolder(t, env, "carla@example.com", "Carla", "Ruiz", f.sizeQuestion.ID, "L")

	sharedApp.SalesService.WithTicketAssignment(false)
	sharedApp.CatalogService.WithTicketAssignment(false)

	data := downloadOK(t, env, f.staffSession, f.eventID, "")
	sheet := openSalesExportAnswers(t, data)
	want := []string{"confirmation_ref", "ticket_type", f.sizeQuestion.Label, f.extraQuestion.Label}
	if !equalStrings(sheet.header, want) {
		t.Fatalf("header = %v, want %v while assignment is dark", sheet.header, want)
	}
	assertWorkbookNeverSays(t, data, "carla@example.com",
		"An address disclosed before the Privacy Policy describes the disclosure must not\n"+
			"leave the building in a file either (ADR 0045). The flag has to withhold a real\n"+
			"acceptance, not merely fail to invent one.")

	// And the per-Ticket sheet is otherwise untouched: closing assignment must
	// not take Ticket Questions down with it. Two flags, and they stay
	// independent.
	if got := sheet.cell(t, 0, f.sizeQuestion.Label); got == "" && len(sheet.rows) != 3 {
		t.Fatalf("rows = %d with the Answers intact, want the three Tickets", len(sheet.rows))
	}
}

// TestSalesExportNeverShowsAPurgedAddress: the retention promise, read back off
// the file.
//
// An address nobody accepted is taken when the Event starts (#331, migration
// 081), and a purged Ticket reads `unassigned` — there is no fourth word, which
// is migration 081's decision rather than this file's. What must not happen is
// the address surviving in an export taken afterwards, which would make the
// purge a deletion of the only copy the platform can see and not of the one that
// travels.
func TestSalesExportNeverShowsAPurgedAddress(t *testing.T) {
	env := setupTest(t)
	f := newExportHolderFixture(t, env)

	assignTicketOK(t, env, f.ana, f.anaSaleID, f.anaTicketIDs[0], "dora@example.com")
	// Ana answers for the friend who never clicked, which is exactly what she is
	// still entitled to do while the Ticket is `assigned`. The Answer is the
	// thing the purge must leave behind.
	putAnswer(t, env, f.staffSession, f.eventID, f.anaTicketIDs[0], f.sizeQuestion.ID,
		map[string]any{"text": "M"})

	// Before the doors: the Ticket is waiting, and the address is already absent
	// from the file. The purge is a second guarantee, not the first one.
	before := downloadOK(t, env, f.staffSession, f.eventID, "")
	if got := openSalesExportAnswers(t, before).rowInState(t, "assigned"); got < 0 {
		t.Fatal("no Ticket reads `assigned` before the Event starts")
	}
	assertWorkbookNeverSays(t, before, "dora@example.com",
		"An address is withheld from the moment it is typed, not from the moment it is purged.")

	// The doors open, and the purge runs.
	holdClocksAt(fixedClock.Add(31 * 24 * time.Hour))
	if result := purgeHolderAddresses(t, env); result.AddressesPurged != 1 {
		t.Fatalf("purge = %+v, want Dora's one address taken", result)
	}

	after := downloadOK(t, env, f.staffSession, f.eventID, "")
	sheet := openSalesExportAnswers(t, after)
	// `unassigned`, like every other Ticket nobody holds. The file does not
	// invent a fourth word for a Ticket somebody was once named for.
	if len(sheet.rows) != 3 {
		t.Fatalf("rows = %d after the purge, want the three Tickets — the purge takes an address, not a Ticket", len(sheet.rows))
	}
	for i := range sheet.rows {
		if got := sheet.cell(t, i, "assignment_state"); got != "unassigned" {
			t.Errorf("row %d reads %q after the purge, want unassigned on every row", i, got)
		}
	}
	assertWorkbookNeverSays(t, after, "dora@example.com",
		"The address was purged when the Event started. An export taken afterwards must not\n"+
			"be the copy that outlived it.")

	// AND THE ANSWER SURVIVES IT. What the purge deleted is the liability, not
	// the platform's record of the sale — a deletion that took the size Ana gave
	// would be destroying an account of what was bought in order to honour a
	// promise about somebody's address.
	var kept bool
	for i := range sheet.rows {
		if sheet.cell(t, i, f.sizeQuestion.Label) == "M" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("no row still answers %q with M: the purge took an Answer as well as an address",
			f.sizeQuestion.Label)
	}
}

// AN EVENT THAT ASKS NOTHING STILL GETS ITS HOLDER COLUMNS (#333).
//
// The Holder columns exist whenever assignment is open, REGARDLESS of
// questions — the same ruling that made the Holder List the roster. Before it,
// `exportAnswers` returned zero rows when the Event asked nothing, so an
// Organization that assigns 80 tickets and asks nothing had no "who is coming"
// in its file either. Here Ticket Questions stay entirely DARK — the flag is
// never opened — and the per-Ticket sheet appears anyway, carrying the fixed
// columns and the four Holder columns and nothing else.
func TestSalesExportCarriesHolderColumnsWithoutAnyTicketQuestion(t *testing.T) {
	env := setupTest(t)
	enableTicketAssignment(t)
	// Deliberately NOT calling enableTicketQuestions: the questions feature is
	// in its shipped state, and there is no question anywhere on this Event.
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Asked Nothing Fest", "asked-nothing-holders")
	ticketTypeID := createTicketTypeWithCapacity(t, env, sessionID, eventID, "GA", 2000, 50)
	commitBatch(t, env, sessionID, eventID, "asked-nothing-holders-batch", []map[string]any{{
		"customer_email":      "ana@example.com",
		"customer_first_name": "Ana",
		"customer_last_name":  "Lopez",
		"ticket_type_id":      ticketTypeID,
		"quantity":            2,
		"payment_method":      "cash",
		"sold_at":             "2026-07-01T10:00:00Z",
	}})

	// Ana names a friend on one of her two Tickets, so the sheet has one
	// `assigned` and one `unassigned` to tell apart. The Sale id is read from
	// the database because the Sale Import response carries a batch, not the
	// id of the Ticket Sale it minted, and the assign route is keyed on it.
	var saleID string
	if err := env.db.QueryRow(
		`SELECT id FROM ticket_sales WHERE event_id = $1`, eventID,
	).Scan(&saleID); err != nil {
		t.Fatalf("read Ticket Sale: %v", err)
	}
	ticketIDs := ticketsOfEvent(t, env, eventID)
	ana := customerSignIn(t, env, "ana@example.com")
	assignTicketOK(t, env, ana, saleID, ticketIDs[0], "diego@example.com")

	data := downloadOK(t, env, sessionID, eventID, "")
	sheet := openSalesExportAnswers(t, data)
	want := []string{"confirmation_ref", "ticket_type",
		"assignment_state", "holder_first_name", "holder_last_name", "holder_email"}
	if !equalStrings(sheet.header, want) {
		t.Fatalf("header = %v, want %v — the Holder columns and no question columns", sheet.header, want)
	}
	if len(sheet.rows) != 2 {
		t.Fatalf("rows = %d, want Ana's two Tickets", len(sheet.rows))
	}
	if got := sheet.rowInState(t, "assigned"); got < 0 {
		t.Fatal("no row reads `assigned` — the assignment did not reach the file")
	}
	if got := sheet.rowInState(t, "unassigned"); got < 0 {
		t.Fatal("no row reads `unassigned`")
	}
	// The disclosure rule travels with the columns: an address nobody accepted
	// is not in the file, on any sheet.
	assertWorkbookNeverSays(t, data, "diego@example.com",
		"An address a buyer typed and its owner never accepted is never exported (ADR 0047),\n"+
			"and giving the sheet to question-less Events must not widen that.")
	// And the Info sheet announces the sheet, so a reader who wonders why it
	// carries no question columns is told rather than left hunting.
	openSalesExportInfo(t, data).says(t, salesExportAnswersSheet)
}
