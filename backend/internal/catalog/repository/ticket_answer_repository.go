package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// AnswerableTicket is one Ticket together with everything needed to decide
// whether it may be answered, in one row.
//
// It carries facts from four tables — the Ticket, its Ticket Sale Line's Ticket
// Type, its Ticket Sale and its Event — because every one of them is needed on
// every call and four round trips to assemble them would be four chances for the
// answers to disagree. A Ticket holds no state of its own (ADR 0043): whether it
// stands is read from SaleStatus here, and when it stops being answerable is
// read from EventStartsAt.
type AnswerableTicket struct {
	ID string
	// Ordinal is which of the line's units this is, 1..quantity. An internal
	// number, and the only thing telling two Tickets on one line apart — which
	// is exactly what Event Staff need in order to say "the second of Ana's
	// four".
	Ordinal int
	// TicketTypeID is where the Ticket Questions live: they belong to the Ticket
	// Type, never to the Event and never to the Organization.
	TicketTypeID   string
	TicketTypeName string
	TicketSaleID   string
	// ConfirmationRef is the buyer's own reference for the Ticket Sale, which is
	// how staff on the phone find the sale a caller is asking about.
	ConfirmationRef string
	// SaleStatus is 'active' or 'reversed', read from the Ticket Sale because a
	// Sale Reversal is always whole-Sale.
	SaleStatus string
	// Channel is the Sales Channel the Ticket Sale was recorded on: 'online',
	// 'in_person' or 'import'. Read from the Ticket Sale for the reason
	// SaleStatus is — a Ticket carries no copy of anything its Sale already says
	// (ADR 0043).
	//
	// It rides here because Ticket Assignment refuses `in_person` (#324): a door
	// sale has no buyer surface to assign from. Nothing about ANSWERS consults
	// it — an `in_person` Ticket's Answers are perfectly writable by Event Staff
	// — so it is a column this struct carries for one of its readers, exactly as
	// ConfirmationRef is carried for the staff one.
	Channel string
	// EventStartsAt is the instant the doors open. Invalid on an Event that has
	// not said when it starts, which has not started.
	EventStartsAt sql.NullTime

	// THE TICKET ASSIGNMENT (#324, parent #322). Four columns on the Ticket
	// rather than a joined entity — see migration 080 — and the state they
	// describe is DERIVED from them by catalog.AssignmentState and never stored.

	// HolderEmail is the address the buyer named for this Ticket, normalised.
	// Invalid while the Ticket is `unassigned`, which is every Ticket until the
	// flag opens.
	HolderEmail sql.NullString
	// HolderCustomerID is the Customer the Holder proved themselves to be, and
	// the whole of what `accepted` means. ALWAYS INVALID IN #324: nothing writes
	// it until #325 lands the Assignment mail and the accept flow.
	HolderCustomerID sql.NullString
	// AssignedAt is when the CURRENT address was named — not how many times, and
	// not who by. It stands still when the same address is submitted again.
	AssignedAt sql.NullTime
	// AcceptedAt is when the Holder clicked. ALWAYS INVALID IN #324, for the
	// reason HolderCustomerID is.
	AcceptedAt sql.NullTime
}

// TicketAnswer is what one Ticket says in reply to one Ticket Question.
//
// EXACTLY ONE OF THE FIVE VALUE FIELDS IS MEANINGFUL, and which one is decided
// by the Ticket Question's kind rather than by anything on this struct. That is
// why there is no Kind here: the kind is the QUESTION's property, it is frozen
// once an Answer exists precisely so that these columns keep meaning what they
// meant, and a second copy of it on the Answer could only ever drift.
type TicketAnswer struct {
	ID               string
	TicketID         string
	TicketQuestionID string
	// Text answers short_text and long_text.
	Text sql.NullString
	// Number answers number. Read as text out of a NUMERIC column, so the digits
	// arrive exactly as Postgres holds them — `3.50` keeps its trailing zero —
	// with no float in the path to round anything.
	Number sql.NullString
	// Date answers date, in YYYY-MM-DD. Read as text out of a DATE column for
	// the same reason: a calendar date scanned into a time.Time acquires a
	// midnight and a zone it never had, and something downstream eventually
	// shifts it by a day.
	Date sql.NullString
	// Checked answers checkbox. Invalid — rather than false — when this is not a
	// checkbox Answer, because false IS an answer.
	Checked sql.NullBool
	// Options are the Options a choice Answer chose, in the order they were
	// given. Empty for the five kinds that are not answered by choosing.
	Options   []TicketAnswerOption
	CreatedAt time.Time
	// UpdatedAt is when the Answer last changed, and the whole of the history
	// this platform keeps: no versions, and no record of who changed it.
	UpdatedAt time.Time
}

// TicketAnswerOption is one Option a choice Answer chose, with the words that
// Option showed at the time.
type TicketAnswerOption struct {
	// TicketQuestionOptionID is the Option's stable identity — what survives a
	// rename, and what joins this back to the Option itself.
	TicketQuestionOptionID string
	// LabelSnapshot is what the Option READ WHEN IT WAS CHOSEN. Never rewritten
	// by a rename; see migration 073.
	LabelSnapshot string
	// CurrentLabel is what the same Option reads NOW, joined at read time. It is
	// what every list and the Sales Export header show, and it is the value that
	// differs from LabelSnapshot after a correction.
	CurrentLabel string
	// Retired is true for an Option kept only so that what chose it still reads.
	Retired   bool
	SortOrder int
}

const answerableTicketColumns = `
	tk.id, tk.ordinal, l.ticket_type_id, tt.name,
	s.id, s.confirmation_ref, s.status, s.channel, e.starts_at,
	tk.holder_email, tk.holder_customer_id, tk.assigned_at, tk.accepted_at
`

func scanAnswerableTicket(row interface {
	Scan(dest ...any) error
}) (*AnswerableTicket, error) {
	var t AnswerableTicket
	if err := row.Scan(
		&t.ID, &t.Ordinal, &t.TicketTypeID, &t.TicketTypeName,
		&t.TicketSaleID, &t.ConfirmationRef, &t.SaleStatus, &t.Channel, &t.EventStartsAt,
		&t.HolderEmail, &t.HolderCustomerID, &t.AssignedAt, &t.AcceptedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// answerableTicketFrom is the join every Ticket read here starts from.
//
// It is written once and shared because the SCOPE is the security property: a
// Ticket is reachable only through its Ticket Sale, and the Sale carries both
// the Event and the Organization. Two copies of this join would be two chances
// for one of them to forget the organization_id and let one Organization's
// Ticket id resolve under another's Event.
const answerableTicketFrom = `
	FROM tickets tk
	JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
	JOIN ticket_types tt ON tt.id = l.ticket_type_id
	JOIN ticket_sales s ON s.id = l.ticket_sale_id
	JOIN events e ON e.id = s.event_id
`

// GetAnswerableTicketByID loads one Ticket of one Event, scoped to the
// Organization that owns it.
//
// It returns the Ticket whether or not it may still be answered. Refusing a
// reversed or started one HERE would make a Ticket unreadable at the moment it
// most needs reading — a Sale Reversal voids a sale, it does not erase what its
// Tickets answered — so the window is the service's decision on the WRITE path
// alone. See catalog.AnswerWindow.
func (r *Repository) GetAnswerableTicketByID(ctx context.Context, organizationID, eventID, ticketID string) (*AnswerableTicket, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+answerableTicketColumns+answerableTicketFrom+`
		WHERE tk.id = $1 AND s.event_id = $2 AND s.organization_id = $3
	`, ticketID, eventID, organizationID)
	return scanAnswerableTicket(row)
}

// ListAnswerableTicketsByTicketSale returns every Ticket of one Ticket Sale, in
// the order they were minted.
//
// REVERSED SALES COME BACK TOO, and their Tickets with them. This is how staff
// reach a Ticket at all, and a sale that vanished from the surface after a
// Reversal would look like the Answers had been destroyed when they have not.
// What a reversed sale's Tickets refuse is the write.
//
// The order is by Ticket Type then ordinal so that "the second of Ana's four"
// means the same thing on every read — the ordinal is the only thing telling two
// Tickets on one line apart.
func (r *Repository) ListAnswerableTicketsByTicketSale(ctx context.Context, organizationID, eventID, ticketSaleID string) ([]AnswerableTicket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+answerableTicketColumns+answerableTicketFrom+`
		WHERE s.id = $1 AND s.event_id = $2 AND s.organization_id = $3
		ORDER BY l.created_at ASC, l.id ASC, tk.ordinal ASC
	`, ticketSaleID, eventID, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]AnswerableTicket, 0)
	for rows.Next() {
		ticket, scanErr := scanAnswerableTicket(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tickets = append(tickets, *ticket)
	}
	return tickets, rows.Err()
}

// ListAnswerableTicketsForBuyer returns every Ticket of one Ticket Sale, scoped
// to the CUSTOMER who bought it (#315).
//
// THE SCOPE IS THE WHOLE SECURITY PROPERTY OF THIS READ, and it is a different
// scope from every other read in this file. The staff reads narrow by
// organization_id and event_id, because a staff credential names an
// Organization. This one narrows by customer_id, because the credential behind
// it is a Customer Session and what it names is a person. Neither an Event nor
// an Organization appears here at all: a buyer does not know which Organization
// sold them a ticket and has no business naming one, and asking them for an id
// they would have to be told would be an invitation to try somebody else's.
//
// WHAT THIS READ FEEDS IS A LIST OF ANSWER LINKS, which is why the narrowing
// matters more here than it looks. Each Ticket that comes back gets a signed,
// unauthenticated Answer Link minted for it, so a Ticket returned to the wrong
// person is not a disclosure of one row — it is handing them a durable
// credential that answers for somebody else's ticket until the Event starts.
// The customer_id clause is what stands between those two outcomes, and it is
// written into the query rather than checked afterwards so that no caller can
// reach the rows without it.
//
// REVERSED SALES COME BACK TOO, exactly as they do for staff, and their Tickets
// with them. A reversed Sale keeps its place in the Customer Area (CONTEXT.md:
// it "is never deleted"), and Tickets that vanished from it would read as data
// destroyed rather than as a purchase undone. What a reversed Sale's Tickets
// refuse is the write, and the service refuses to mint their Answer Links.
//
// The order matches the staff read's — line, then ordinal — so that "the second
// of my four" means the same thing to a buyer on the phone as to the member of
// staff they are talking to.
func (r *Repository) ListAnswerableTicketsForBuyer(ctx context.Context, customerID, ticketSaleID string) ([]AnswerableTicket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+answerableTicketColumns+answerableTicketFrom+`
		WHERE s.id = $1 AND s.customer_id = $2
		ORDER BY l.created_at ASC, l.id ASC, tk.ordinal ASC
	`, ticketSaleID, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]AnswerableTicket, 0)
	for rows.Next() {
		ticket, scanErr := scanAnswerableTicket(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tickets = append(tickets, *ticket)
	}
	return tickets, rows.Err()
}

// ListTicketAnswers returns the Answers of a set of Tickets, each with the
// Options it chose.
//
// TWO QUERIES FOR ANY NUMBER OF TICKETS, not two per Ticket: a Ticket Sale of
// forty tickets with six questions each is one read of the Answers and one of
// their chosen Options, which is the same shape
// ListTicketQuestionOptionsByTicketTypeID uses for the same reason.
func (r *Repository) ListTicketAnswers(ctx context.Context, ticketIDs []string) ([]TicketAnswer, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, ticket_id, ticket_question_id,
		       text_value, number_value::text, date_value::text, boolean_value,
		       created_at, updated_at
		FROM ticket_answers
		WHERE ticket_id = ANY($1)
	`, ticketIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	answers := make([]TicketAnswer, 0)
	byID := make(map[string]int)
	for rows.Next() {
		var a TicketAnswer
		if err := rows.Scan(
			&a.ID, &a.TicketID, &a.TicketQuestionID,
			&a.Text, &a.Number, &a.Date, &a.Checked,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		byID[a.ID] = len(answers)
		answers = append(answers, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(answers) == 0 {
		return answers, nil
	}

	answerIDs := make([]string, 0, len(answers))
	for _, a := range answers {
		answerIDs = append(answerIDs, a.ID)
	}

	// The snapshot and the current label travel together, because the difference
	// between them is the whole point of keeping the snapshot: after a rename
	// the export header and every list say `Chicken (halal)` while the person
	// who ticked it read `Chicken`, and a surface that can show only one of
	// those cannot answer what somebody actually agreed to.
	optionRows, err := r.db.Pool.QueryContext(ctx, `
		SELECT ao.ticket_answer_id, ao.ticket_question_option_id,
		       ao.option_label_snapshot, o.label, o.retired_at IS NOT NULL,
		       ao.sort_order
		FROM ticket_answer_options ao
		JOIN ticket_question_options o ON o.id = ao.ticket_question_option_id
		WHERE ao.ticket_answer_id = ANY($1)
		ORDER BY ao.sort_order ASC
	`, answerIDs)
	if err != nil {
		return nil, err
	}
	defer optionRows.Close()

	for optionRows.Next() {
		var answerID string
		var option TicketAnswerOption
		if err := optionRows.Scan(
			&answerID, &option.TicketQuestionOptionID,
			&option.LabelSnapshot, &option.CurrentLabel, &option.Retired,
			&option.SortOrder,
		); err != nil {
			return nil, err
		}
		if index, ok := byID[answerID]; ok {
			answers[index].Options = append(answers[index].Options, option)
		}
	}
	return answers, optionRows.Err()
}

// GetTicketAnswer loads one Ticket's Answer to one Ticket Question, or nil when
// the question is still an Outstanding Answer on that Ticket.
func (r *Repository) GetTicketAnswer(ctx context.Context, ticketID, questionID string) (*TicketAnswer, error) {
	answers, err := r.ListTicketAnswers(ctx, []string{ticketID})
	if err != nil {
		return nil, err
	}
	for i := range answers {
		if answers[i].TicketQuestionID == questionID {
			return &answers[i], nil
		}
	}
	return nil, nil
}

// UpsertTicketAnswerParams is one Answer as it goes to storage: the typed value
// for its kind, and the Options a choice Answer chose.
//
// The four scalars are nullable and at most one of them is set — the database
// CHECKs that much — and the choice Answer sets none of them, its value being
// Options.
type UpsertTicketAnswerParams struct {
	Text    sql.NullString
	Number  sql.NullString
	Date    sql.NullString
	Checked sql.NullBool
	Options []UpsertTicketAnswerOption
}

// UpsertTicketAnswerOption is one chosen Option and the words it showed at the
// moment of choosing.
//
// The SNAPSHOT IS THE CALLER'S, taken from the Option as it reads right now and
// passed in rather than copied here with a sub-SELECT. That is deliberate: the
// service has already loaded the Options to check that this question offers them,
// so the label it snapshots is provably the same one it validated against, and
// there is no window in which a rename between the check and the write could put
// a different set of words on the row than the one the caller was looking at.
type UpsertTicketAnswerOption struct {
	TicketQuestionOptionID string
	LabelSnapshot          string
}

// UpsertTicketAnswer writes one Ticket's Answer to one Ticket Question, creating
// it or replacing what was there.
//
// ONE TRANSACTION, because an Answer and its chosen Options are one thing. A
// multi_choice Answer is replaced by clearing its Options and writing the new
// set, and a failure between those two leaves an Answer that says nothing — a
// state no reader should ever be able to observe, and one that would look
// exactly like a question nobody answered.
//
// UPSERT ON (ticket_id, ticket_question_id) rather than delete-then-insert: the
// UNIQUE is the model — one Answer per Ticket per question — and going through it
// means a re-answer keeps the Answer's own id and its created_at, so "when this
// Ticket first said something about this" survives a correction while updated_at
// records the correction. A delete-then-insert would silently reset both.
func (r *Repository) UpsertTicketAnswer(
	ctx context.Context,
	ticketID, questionID string,
	params UpsertTicketAnswerParams,
	now time.Time,
) (*TicketAnswer, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var answerID string
	// The casts are on the placeholders and not on the columns: `$4` arrives as
	// a string carrying digits, and telling Postgres it is a NUMERIC (or a DATE)
	// is what keeps the value typed all the way in. A driver-side float would
	// round it, and an untyped literal would make the column's own type do the
	// coercion silently.
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO ticket_answers (
			ticket_id, ticket_question_id,
			text_value, number_value, date_value, boolean_value,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, $4::numeric, $5::date, $6, $7, $7)
		ON CONFLICT (ticket_id, ticket_question_id) DO UPDATE
		SET text_value = EXCLUDED.text_value,
		    number_value = EXCLUDED.number_value,
		    date_value = EXCLUDED.date_value,
		    boolean_value = EXCLUDED.boolean_value,
		    updated_at = EXCLUDED.updated_at
		RETURNING id
	`, ticketID, questionID,
		params.Text, params.Number, params.Date, params.Checked, now,
	).Scan(&answerID); err != nil {
		return nil, err
	}

	// Replaced wholesale rather than diffed. A choice Answer IS its set of
	// Options, so "correct this Answer" means "this is the set now", and a diff
	// would only add a way for the set to end up somewhere in between. The
	// snapshots come back with the new rows, so a rename between two answering
	// acts is visible as two different snapshots — which is correct: those are
	// two different acts of reading two different sets of words.
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM ticket_answer_options WHERE ticket_answer_id = $1
	`, answerID); err != nil {
		return nil, err
	}
	for i, option := range params.Options {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ticket_answer_options (
				ticket_answer_id, ticket_question_option_id,
				option_label_snapshot, sort_order, created_at
			)
			VALUES ($1, $2, $3, $4, $5)
		`, answerID, option.TicketQuestionOptionID, option.LabelSnapshot, i, now); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetTicketAnswer(ctx, ticketID, questionID)
}

// TouchTicketAnswer is the empty write: it exists so nothing has one.
//
// Deliberately absent. There is no path that stamps updated_at without changing
// the Answer, because updated_at means "when this Answer last CHANGED" and a
// no-op re-submission — a form saved twice, a double-clicked button — did not
// change it. The service compares before writing; see AnswerValue equality
// there.

// DeleteTicketAnswer removes one Ticket's Answer to one Ticket Question,
// reporting whether there was one.
//
// This is the ONLY delete in the feature, and it is the counterpart of
// correcting rather than an exception to "retired, never deleted". A Ticket
// Question and an Option are the Organization's own words that other Tickets'
// Answers still refer to; an Answer refers to nothing, and removing one restores
// the state the Ticket was in before anybody answered — an Outstanding Answer if
// the question is required. Somebody who ticked the wrong box needs a way back
// to "not said", and storing a blank Answer to mean that would be a row no later
// reader could tell from a real reply.
//
// The chosen Options go with it by cascade.
func (r *Repository) DeleteTicketAnswer(ctx context.Context, ticketID, questionID string) (bool, error) {
	result, err := r.db.Pool.ExecContext(ctx, `
		DELETE FROM ticket_answers
		WHERE ticket_id = $1 AND ticket_question_id = $2
	`, ticketID, questionID)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}

// AnswerLinkTicket is one Ticket as the Answer Link's page is allowed to see it
// (#312, ADR 0044).
//
// A SEPARATE TYPE FROM AnswerableTicket, AND THE SEPARATION IS THE SECURITY
// PROPERTY. An Answer Link is an unauthenticated URL that gets forwarded into
// group chats, and ADR 0044 says its safety rests entirely on disclosing nothing
// about the purchase. AnswerableTicket carries the Ticket Sale's id, its
// Sale Confirmation reference and the Ticket's ordinal — every one of which is a
// fact about the purchase — so reusing it here would put all three one
// forgetful `json:` tag away from a stranger's screen.
//
// THE QUERY BELOW CANNOT LEAK WHAT IT DOES NOT SELECT. That is why this exists
// as its own struct and its own SELECT rather than as a filter over the staff
// read: a filter is a thing somebody can widen "for context" without noticing,
// while a column that was never fetched is not there to widen.
//
// What it does carry is exactly the three things the page shows plus the two
// facts the edit window is decided from:
//   - EventName and TicketTypeName, which the page shows.
//   - TicketTypeID, which is where the Ticket Questions live — never shown, and
//     needed to read them.
//   - SaleStatus and EventStartsAt, which catalog.AnswerWindow reads and which
//     never reach the wire.
//
// Note what is absent and must stay absent: the buyer, the price, the Tax ID,
// the Sale Confirmation reference, the Ticket Sale's id, and the ordinal that
// would say how many Tickets the Sale has.
type AnswerLinkTicket struct {
	ID             string
	TicketTypeID   string
	TicketTypeName string
	EventName      string
	// SaleStatus is 'active' or 'reversed'. Read here rather than trusted from
	// the token, because a Sale reversed after the link was minted must stop the
	// link opening — which a baked-in fact never could.
	SaleStatus string
	// EventStartsAt is the instant the doors open, invalid on an Event that has
	// not said when it starts. Read live for the same reason: an Organization
	// that moves its Event moves every Answer Link's deadline with it.
	EventStartsAt sql.NullTime
	// AcceptedAt is when a Holder accepted this Ticket, invalid until one has
	// (#325, ADR 0046).
	//
	// IT IS HERE TO CLOSE THIS DOOR. Once somebody has proved the address and
	// accepted, the Answer Link stops opening for good: it is the door for a
	// Ticket nobody has claimed, and a link still sitting in a group chat must
	// not be able to overwrite what the Holder said about themselves. That is
	// what makes the accept click mean something, and it is CONTEXT.md's own
	// account of the Answer Link.
	//
	// It never reaches the wire. What the holder of a retired link is told is
	// that the link is not valid — the same thing every other closed door says,
	// and it discloses nothing about the person who now holds the Ticket.
	AcceptedAt sql.NullTime
}

// GetAnswerLinkTicket loads one Ticket by id, UNSCOPED BY ORGANIZATION.
//
// THE ABSENCE OF A SCOPE IS DELIBERATE AND IS NOT A HOLE. Every other Ticket
// read on this platform is scoped to the acting Organization because there is an
// actor; here there is none by design (ADR 0044: no sign-in, no passcode, no
// Customer and no session). The authorization is the signed token, checked in
// catalog.AnswerLinkSigner.Parse BEFORE this is called, and it names exactly one
// Ticket. Adding an Organization parameter would mean the caller had learned one
// from somewhere, and the only place it could come from is this row.
//
// It returns the Ticket whether or not it may still be answered, for the same
// reason GetAnswerableTicketByID does: the window is a decision, taken in one
// place, from SaleStatus and EventStartsAt. Deciding it in the WHERE clause here
// would make "reversed" and "no such Ticket" the same row count and put a second
// copy of catalog.AnswerWindow into SQL.
func (r *Repository) GetAnswerLinkTicket(ctx context.Context, ticketID string) (*AnswerLinkTicket, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT tk.id, l.ticket_type_id, tt.name, e.name, s.status, e.starts_at, tk.accepted_at
		`+answerableTicketFrom+`
		WHERE tk.id = $1
	`, ticketID)

	var t AnswerLinkTicket
	if err := row.Scan(
		&t.ID, &t.TicketTypeID, &t.TicketTypeName, &t.EventName, &t.SaleStatus, &t.EventStartsAt,
		&t.AcceptedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// HeldTicket is one Ticket read through the person who HOLDS it (#343,
// ADR 0049): an AnswerableTicket plus the two public facts about its Event the
// Holder's own surface shows.
//
// It carries the whole AnswerableTicket rather than a narrower row so that the
// answer window (SaleStatus, EventStartsAt) and the write path (TicketTypeID)
// come from the same read every other Answer write uses. What the HOLDER may
// SEE of it is the service's decision, and deliberately less than is here:
// ConfirmationRef and TicketSaleID ride along and never leave the process.
type HeldTicket struct {
	AnswerableTicket
	EventName string
	EventSlug string
}

// ListHeldTicketsForCustomer returns every Ticket one Customer holds: the
// Self-held Ticket of their own purchase and every Ticket they accepted by
// Assignment Link, indistinguishably (#343, ADR 0049).
//
// THE SCOPE IS holder_customer_id AND NOTHING ELSE. Not the Sale's customer_id:
// a buyer who assigned a Ticket away no longer holds it, and a Ticket on their
// own Sale that somebody else accepted is not theirs to read here. The column is
// written only by the accept flow and by checkout for the Self-held Ticket
// (migration 080's CHECK refuses one without an acceptance), so what this lists
// is what somebody proved or paid for — never what a buyer typed about them.
//
// A REVERSED SALE'S TICKET STAYS ONLY FOR ITS BUYER. This is the same split the
// Customer Area makes (customers/repository.ListHeldTicketsForCustomer): the
// buyer keeps the reversed Sale as their financial record and its Self-held
// Ticket's Answers stay readable on it — a Reversal voids a purchase, it does
// not erase what was said — while a Holder who is not the buyer simply stops
// holding it. A Ticket the buyer reassigned needs no clause: reassignment
// clears holder_customer_id with the address.
//
// Ordered soonest Event first, then by acceptance, so the Customer's list has a
// stable order to draw.
func (r *Repository) ListHeldTicketsForCustomer(ctx context.Context, customerID string) ([]HeldTicket, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+answerableTicketColumns+`, e.name, e.slug
		`+answerableTicketFrom+`
		WHERE tk.holder_customer_id = $1
		  AND tk.accepted_at IS NOT NULL
		  AND (s.status = 'active' OR s.customer_id = $1)
		ORDER BY e.starts_at ASC NULLS LAST, tk.accepted_at DESC, tk.id ASC
	`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tickets := make([]HeldTicket, 0)
	for rows.Next() {
		var t HeldTicket
		if err := rows.Scan(
			&t.ID, &t.Ordinal, &t.TicketTypeID, &t.TicketTypeName,
			&t.TicketSaleID, &t.ConfirmationRef, &t.SaleStatus, &t.Channel, &t.EventStartsAt,
			&t.HolderEmail, &t.HolderCustomerID, &t.AssignedAt, &t.AcceptedAt,
			&t.EventName, &t.EventSlug,
		); err != nil {
			return nil, err
		}
		tickets = append(tickets, t)
	}
	return tickets, rows.Err()
}
