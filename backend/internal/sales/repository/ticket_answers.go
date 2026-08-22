package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The reads behind the Sales Export's per-Ticket sheet (#314): the Event's
// Ticket Questions as the sheet's columns, and the Tickets of the exported sales
// as its rows.
//
// They live in the sales repository rather than in catalog's for the reason
// ListEventTicketTypes does: the Sales Export is a sales artifact, and this is
// the same module reading the same catalog tables to describe them in a file.
// Nothing here writes, and nothing here decides anything about a Ticket
// Question — the rules for what one is, what may be asked and when it may be
// answered stay entirely on catalog's side of the fence.

// EventTicketQuestion is one Ticket Question of an Event, with the Options it
// offers, as the export's column layout needs it.
type EventTicketQuestion struct {
	ID string
	// Label is the question's CURRENT wording, joined live and never
	// snapshotted, so a heading never contradicts the screen the file came from.
	Label string
	// Kind is one of catalog's seven kinds. The export only distinguishes them to
	// decide a cell's TYPE — a number as a number, a date as a date — and to
	// decide which single kind fans out into one column per Option.
	Kind string
	// Options are the question's Options in display order, RETIRED ONES
	// INCLUDED. An Option is retired and never deleted precisely so the Tickets
	// that chose it keep reading, and a file that dropped its column would be the
	// thing that broke that promise (CONTEXT.md, migration 073).
	Options []EventTicketQuestionOption
}

// EventTicketQuestionOption is one Option, by identity and current label.
type EventTicketQuestionOption struct {
	ID string
	// Label is the Option's CURRENT label — what the column heading reads. The
	// snapshot each Answer keeps of the words its chooser actually read is a
	// different fact, per Answer rather than per column, and is not what a
	// heading can honestly show.
	Label string
}

// ListEventTicketQuestions returns every Ticket Question defined on the Event's
// Ticket Types, in catalog display order, with each question's Options.
//
// RETIRED QUESTIONS AND RETIRED OPTIONS ARE INCLUDED. Retiring either takes it
// off the lists new buyers and staff are shown; it does not unsay what has
// already been answered, and the export is the one surface that reads every
// Answer at once. A retired question with Answers under it whose column
// vanished would be data silently lost from the file.
func (r *Repository) ListEventTicketQuestions(ctx context.Context, orgID, eventID string) ([]EventTicketQuestion, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT q.id, q.label, q.kind
		FROM ticket_questions q
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		WHERE tt.event_id = $1 AND tt.organization_id = $2
		ORDER BY tt.sort_order, tt.name, q.sort_order, q.created_at, q.id
	`, eventID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var questions []EventTicketQuestion
	index := map[string]int{}
	var ids []string
	for rows.Next() {
		var q EventTicketQuestion
		if err := rows.Scan(&q.ID, &q.Label, &q.Kind); err != nil {
			return nil, err
		}
		index[q.ID] = len(questions)
		ids = append(ids, q.ID)
		questions = append(questions, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, nil
	}

	optionRows, err := r.db.Pool.QueryContext(ctx, `
		SELECT o.ticket_question_id, o.id, o.label
		FROM ticket_question_options o
		WHERE o.ticket_question_id = ANY($1)
		ORDER BY o.sort_order, o.created_at, o.id
	`, ids)
	if err != nil {
		return nil, err
	}
	defer optionRows.Close()

	for optionRows.Next() {
		var questionID string
		var option EventTicketQuestionOption
		if err := optionRows.Scan(&questionID, &option.ID, &option.Label); err != nil {
			return nil, err
		}
		if at, ok := index[questionID]; ok {
			questions[at].Options = append(questions[at].Options, option)
		}
	}
	return questions, optionRows.Err()
}

// ExportedTicket is one Ticket of an exported Ticket Sale, and one row of the
// per-Ticket sheet.
type ExportedTicket struct {
	ID string
	// TicketTypeName is the Ticket Type's CURRENT name, joined live exactly as
	// the data sheet's Ticket Type headings are.
	TicketTypeName string

	// THE HOLDER, AS THE EXPORT MAY SEE IT (#330, parent #322, ADR 0047).

	// AssignmentState is `unassigned`, `assigned` or `accepted`, derived here by
	// catalog.AssignmentState so that the file cannot use the word differently
	// from the buyer's page or the staff guest list (migration 080). A Ticket
	// whose address was purged at Event start reads `unassigned` (migration 081).
	AssignmentState catalog.TicketAssignmentState
	// HolderFirstName, HolderLastName and HolderEmail are the accepted Holder,
	// and are EMPTY ON EVERY OTHER TICKET.
	//
	// THEY ARE READ OFF THE JOINED CUSTOMER AND NEVER OFF tickets.holder_email,
	// and that is the enforcement of ADR 0047 rather than a stylistic choice.
	// A Customer reference exists only where an acceptance does — migration
	// 080's tickets_holder_customer_requires_acceptance_ck refuses the pairing
	// outright — so an address that was typed by a buyer and never accepted has
	// no row for this join to reach, and cannot arrive here however this struct
	// is later filled in. Selecting tk.holder_email into this field would be the
	// one-line change that reverses the decision the whole feature is drawn
	// around; there is deliberately no field on this struct for it to land in.
	HolderFirstName string
	HolderLastName  string
	HolderEmail     string

	// Answers is what this Ticket has said, one entry per Ticket Question it has
	// answered. A question missing from here is an Outstanding Answer, or one
	// this Ticket was never asked because it belongs to another Ticket Type.
	Answers []ExportedAnswer
}

// ExportedAnswer is one Answer as the export reads it: at most one typed value,
// plus the Options a choice Answer picked.
type ExportedAnswer struct {
	TicketQuestionID string
	// Text is the short_text/long_text value.
	Text *string
	// Number is the NUMERIC value AS TEXT, exactly as it was stored — trailing
	// zero and all. It is read as text rather than as a float because the column
	// is an unconstrained NUMERIC that no Go float can hold faithfully at every
	// width, and turning it into one is a decision for whoever is about to write
	// it into a cell (migration 073).
	Number *string
	// Date is the DATE value: a calendar date, with no time and no zone.
	Date *time.Time
	// Checked is the checkbox value. FALSE is an Answer and nil is the absence
	// of one.
	Checked *bool
	// ChosenOptionIDs are the Options this Answer picked, by IDENTITY and in the
	// order they were chosen. Identity rather than label because a rename must
	// not fork one Option into two columns.
	ChosenOptionIDs []string
}

// ListTicketsForSales returns the Tickets of the given Ticket Sales with their
// Answers, grouped by Ticket Sale id.
//
// It is keyed by SALE rather than returned as a flat list so the caller can walk
// the sales in the order its own rows are already in: the per-Ticket sheet then
// reads down in the same order as the data sheet beside it, and the two sheets
// cannot disagree about which sales the file is about. Handing it the ids of the
// rows the data sheet was built from is also the whole of how this sheet
// respects the Sales list's filters — it never re-runs the filter, so it cannot
// re-run it differently.
func (r *Repository) ListTicketsForSales(ctx context.Context, saleIDs []string) (map[string][]ExportedTicket, error) {
	if len(saleIDs) == 0 {
		return nil, nil
	}

	// The assignment columns (migration 080) and the Holder's Customer row.
	//
	// TWO DIFFERENT THINGS ARE BEING READ HERE AND THEY MUST NOT BE CONFLATED.
	// tk.holder_email, tk.assigned_at and tk.accepted_at decide which of three
	// WORDS the state column says, and the address among them is scanned into a
	// local that never leaves this loop. The Holder's NAME AND ADDRESS come from
	// the LEFT JOIN, and only from it — see ExportedTicket for why that join is
	// ADR 0047's rule made structural rather than remembered.
	//
	// The join is LEFT because almost every Ticket has no Holder: an Event where
	// nobody has accepted must still export every one of its Tickets.
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT l.ticket_sale_id, tk.id, tt.name,
		       tk.holder_email, tk.assigned_at, tk.accepted_at,
		       h.first_name, h.last_name, h.email
		FROM tickets tk
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		JOIN ticket_types tt ON tt.id = l.ticket_type_id
		LEFT JOIN customers h ON h.id = tk.holder_customer_id
		WHERE l.ticket_sale_id = ANY($1)
		ORDER BY l.ticket_sale_id, tt.sort_order, tt.name, l.id, tk.ordinal
	`, saleIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Pointers while the rows are being assembled, so a Ticket's identity does
	// not move under the map when the slice it sits in grows.
	bySale := map[string][]*ExportedTicket{}
	at := map[string]*ExportedTicket{}
	for rows.Next() {
		var saleID string
		ticket := &ExportedTicket{}
		// Scoped to this iteration and never hung off the Ticket. This is the
		// unaccepted address, and the only thing it is allowed to do is help
		// decide a word.
		var holderEmail sql.NullString
		var assignedAt, acceptedAt sql.NullTime
		var firstName, lastName, customerEmail sql.NullString
		if err := rows.Scan(
			&saleID, &ticket.ID, &ticket.TicketTypeName,
			&holderEmail, &assignedAt, &acceptedAt,
			&firstName, &lastName, &customerEmail,
		); err != nil {
			return nil, err
		}
		ticket.AssignmentState = catalog.AssignmentState(
			holderEmail.String, assignmentTime(assignedAt), assignmentTime(acceptedAt))
		ticket.HolderFirstName = firstName.String
		ticket.HolderLastName = lastName.String
		ticket.HolderEmail = customerEmail.String
		bySale[saleID] = append(bySale[saleID], ticket)
		at[ticket.ID] = ticket
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(at) == 0 {
		return flatten(bySale), nil
	}

	// The Answers, read by SALE rather than by the ticket ids just collected: an
	// Event's sales can carry far more Tickets than sales, and the sale ids are
	// the narrower list by definition.
	answerRows, err := r.db.Pool.QueryContext(ctx, `
		SELECT a.id, a.ticket_id, a.ticket_question_id,
		       a.text_value, a.number_value::text, a.date_value, a.boolean_value
		FROM ticket_answers a
		JOIN tickets tk ON tk.id = a.ticket_id
		JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
		WHERE l.ticket_sale_id = ANY($1)
	`, saleIDs)
	if err != nil {
		return nil, err
	}
	defer answerRows.Close()

	// Held aside rather than hung off their Tickets straight away: the Options
	// each choice Answer picked arrive in a second query below, and an Answer
	// already appended to a growing slice is an Answer whose address moves.
	type pendingAnswer struct {
		id       string
		ticketID string
		answer   ExportedAnswer
	}
	var pending []pendingAnswer
	var answerIDs []string
	for answerRows.Next() {
		var p pendingAnswer
		var date sql.NullTime
		if err := answerRows.Scan(
			&p.id, &p.ticketID, &p.answer.TicketQuestionID,
			&p.answer.Text, &p.answer.Number, &date, &p.answer.Checked,
		); err != nil {
			return nil, err
		}
		if date.Valid {
			d := date.Time
			p.answer.Date = &d
		}
		pending = append(pending, p)
		answerIDs = append(answerIDs, p.id)
	}
	if err := answerRows.Err(); err != nil {
		return nil, err
	}

	chosen := map[string][]string{}
	if len(answerIDs) > 0 {
		// The chosen Options, by identity. The label snapshot each row also
		// carries is deliberately NOT read: the export's headings are the
		// Options' current labels, and a Ticket's snapshot of the words it read
		// is a support question rather than a column (migration 073).
		optionRows, err := r.db.Pool.QueryContext(ctx, `
			SELECT ao.ticket_answer_id, ao.ticket_question_option_id
			FROM ticket_answer_options ao
			WHERE ao.ticket_answer_id = ANY($1)
			ORDER BY ao.sort_order, ao.id
		`, answerIDs)
		if err != nil {
			return nil, err
		}
		defer optionRows.Close()

		for optionRows.Next() {
			var answerID, optionID string
			if err := optionRows.Scan(&answerID, &optionID); err != nil {
				return nil, err
			}
			chosen[answerID] = append(chosen[answerID], optionID)
		}
		if err := optionRows.Err(); err != nil {
			return nil, err
		}
	}

	for _, p := range pending {
		ticket, ok := at[p.ticketID]
		if !ok {
			// Unreachable: the Answers were selected through the same join the
			// Tickets were. Skipped rather than assumed, so a later change to
			// either query cannot turn into a nil dereference.
			continue
		}
		p.answer.ChosenOptionIDs = chosen[p.id]
		ticket.Answers = append(ticket.Answers, p.answer)
	}
	return flatten(bySale), nil
}

// flatten turns the assembly's pointers back into values, which is what a caller
// wants: nothing outside this function has any business holding a Ticket's
// address.
// assignmentTime hands catalog.AssignmentState the nil it reads an absence as.
// Postgres NULL, sql.NullTime and a nil *time.Time are three spellings of one
// fact, and this is where the last two meet.
func assignmentTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time
	return &at
}

func flatten(bySale map[string][]*ExportedTicket) map[string][]ExportedTicket {
	out := make(map[string][]ExportedTicket, len(bySale))
	for saleID, tickets := range bySale {
		flat := make([]ExportedTicket, 0, len(tickets))
		for _, ticket := range tickets {
			flat = append(flat, *ticket)
		}
		out[saleID] = flat
	}
	return out
}
