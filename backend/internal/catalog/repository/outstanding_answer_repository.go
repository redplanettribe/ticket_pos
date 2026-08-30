package repository

import (
	"github.com/peter/ticket_pos/backend/internal/catalog"

	"context"
	"database/sql"
	"fmt"
	"time"
)

// The Outstanding Answers derivation (#313), and the Holder List's roster query
// beside it.
//
// THIS FILE KEEPS ITS NAME while the service and handler beside it were renamed
// to the Holder List (#519, ADR 0065), because it is the one place that really
// does hold both things: the DEBT's SQL — the four clauses that mirror
// catalog.IsOutstandingAnswer — and the ROSTER's, which owes the debt nothing
// but reuses its FROM and WHERE when `owingOnly` narrows the list. Naming it
// after either half would lie about the other, and splitting it would put the
// two statements of one rule in two files, which is exactly what the note below
// exists to prevent.
//
// An Outstanding Answer is a required Ticket Question one Ticket has not
// answered — DERIVED AND NEVER STORED. There is no table here and there must
// never be one; see catalog.IsOutstandingAnswer for why, and for the statement
// of the rule these queries implement.
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, and that is a deliberate cost.
// An Event's ticket roll can run to thousands of Tickets across several
// questions, so the debt cannot be computed by loading every Ticket into Go and
// filtering — the database has to know the rule. catalog.IsOutstandingAnswer is
// the statement of it, outstandingAnswerWhere below is the same four clauses in
// the same order, and the integration tests hold them together. Neither may be
// changed alone.

// outstandingAnswerFrom is the join that produces one row per (Ticket, required
// Ticket Question) pair, with the Answer that would discharge it if there is
// one.
//
// THE CROSS OF TICKETS AND THEIR TYPE'S QUESTIONS, which is what makes the debt
// derivable at all: a Ticket owes nothing that is written down anywhere, it owes
// whatever its Ticket Type asks and it has not replied to. The join to
// ticket_questions is on l.ticket_type_id, because Ticket Questions belong to
// the TICKET TYPE — never to the Event and never to the Organization — so a
// Ticket of the General type owes nothing the VIP type asks.
//
// THE LEFT JOIN IS THE WHOLE MECHANISM. `a.id IS NULL` in the WHERE is "this
// Ticket has said nothing about this question", and it is an ANTI-JOIN rather
// than a NOT EXISTS only because the same shape has to serve the grouped count
// and the per-question listing without being written twice.
//
// SCOPE IS THE SECURITY PROPERTY, and every caller must add
// `s.event_id = ... AND s.organization_id = ...` to the WHERE. Both, and never
// only the Event: an Event id alone would let one Organization's guess at an id
// resolve. The same reasoning as answerableTicketFrom above, which is scoped for
// the same reason.
const outstandingAnswerFrom = `
	FROM tickets tk
	JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
	JOIN ticket_sales s ON s.id = l.ticket_sale_id
	JOIN ticket_types tt ON tt.id = l.ticket_type_id
	JOIN ticket_questions q ON q.ticket_type_id = l.ticket_type_id
	LEFT JOIN ticket_answers a
		ON a.ticket_id = tk.id AND a.ticket_question_id = q.id
`

// outstandingAnswerWhere is the definition of the debt, in SQL.
//
// FOUR CLAUSES, THE SAME FOUR AS catalog.IsOutstandingAnswer, in the same order:
//
//   - q.required — the flag's only effect anywhere. An unanswered OPTIONAL
//     question is not a debt; nobody promised to answer it.
//
//   - catalog.AskedQuestionSQL — approved AND not retired (ADR 0056). A question
//     the Platform Operator has not approved was put to nobody and is owed by
//     nobody; the shared predicate is what keeps this list, the checkout, the
//     export and the Answer Reminder agreeing on which questions exist. And
//     a RETIRED question owes nothing. Every write path
//     into an Answer refuses a retired question, so a debt under one could never
//     be discharged by anybody: a row on the chase list with no working button
//     behind it. This erases no Answer — a retired question that WAS answered
//     still reads on the Ticket and still gets its export column. What ends is
//     the debt, not the record.
//
//   - s.status = 'active' — a reversed Ticket Sale's Tickets NEVER appear. Its
//     tickets have ceased to exist and its money has gone back, so there is
//     nobody to chase. Liveness is read from the Sale because a Ticket carries
//     no status of its own (ADR 0043).
//
//   - a.id IS NULL — nothing has been said. EXISTENCE AND NOT CONTENT: a
//     checkbox answered `false` is an Answer and discharges the debt, and a
//     blank Answer is never stored, so the row's presence is the whole test.
//
// WHAT IS DELIBERATELY ABSENT IS A CHANNEL FILTER. Tickets from `in_person` and
// `import` sales stand here beside the `online` ones, and start out owing
// EVERYTHING, because nobody ever put the questions to those buyers — there is
// no checkout form on a door sale or a spreadsheet import. That is the honest
// state of the debt and not a defect in the data, and hiding it would hide
// precisely the Tickets an Organization most needs to chase.
//
// AND SO IS THE CLOCK. An Event that has already started still reports its
// Outstanding Answers, even though catalog.AnswerWindow has by then frozen every
// route into an Answer. The doors opening un-asks nothing: the debt was real and
// went unpaid, and that is what somebody reviewing the event afterwards came to
// find out. Surfaces that must go quiet after the start impose their own
// silence; that is a rule about mailing, not about the debt.
const outstandingAnswerWhere = `
	q.required
	AND ` + catalog.AskedQuestionSQL + `
	AND s.status = 'active'
	AND a.id IS NULL
`

// outstandingAnswerScope narrows the derivation to one Event of one
// Organization. Written once beside the FROM so that no caller can assemble the
// join and forget half of it.
const outstandingAnswerScope = `
	AND s.event_id = $1 AND s.organization_id = $2
`

// holderRosterFrom is the join that produces ONE ROW PER TICKET of an Event —
// the Holder List's rows (#333, rulings of 2026-08-22).
//
// EVERY TICKET, NOT EVERY TICKET THAT OWES. This is the roster: a fully
// answered Ticket stays on it, and an Event that asks no questions still has
// one, because the roster is the point and the questions are a column on it.
// It deliberately does NOT join ticket_questions — the debt is somebody else's
// derivation (outstandingAnswerFrom above), reused where it is needed and never
// folded into what a Ticket IS.
//
// LIVENESS IS THE ONE FILTER, applied in holderRosterWhere: a reversed Ticket
// Sale's Tickets have ceased to exist and are on nobody's roster, exactly as
// they are in nobody's debt (ADR 0043).
const holderRosterFrom = `
	FROM tickets tk
	JOIN ticket_sale_lines l ON l.id = tk.ticket_sale_line_id
	JOIN ticket_sales s ON s.id = l.ticket_sale_id
	JOIN ticket_types tt ON tt.id = l.ticket_type_id
`

// holderRosterWhere scopes the roster to the live Tickets of one Event of one
// Organization. Both scopes and never only the Event, for
// outstandingAnswerScope's reason: an Event id alone would let one
// Organization's guess at an id resolve.
//
// A FORMAT STRING AND NOT A CONSTANT WITH `$1`/`$2` BAKED IN (#523). It used to
// carry the numbers, which held for exactly as long as the roster had one
// optional filter appended after a fixed pair of arguments. It now has four,
// each appending its own placeholder, and a fragment that asserts its own
// position is a fragment that silently reads the wrong argument the day
// somebody puts a filter in front of it. The two `%d` are the Event and the
// Organization, in that order; holderRosterFilters below is the only caller and
// hands it the positions it actually used.
const holderRosterWhere = `
	s.status = 'active'
	AND s.event_id = $%d AND s.organization_id = $%d
`

// holderRosterOwingOnly narrows the roster to the Tickets that still owe a
// required Answer — the Outstanding Answers FILTER, which is what Outstanding
// Answers now is: a filter on the Holder List, not its definition (#333).
//
// IT REUSES THE DEBT'S OWN SQL, outstandingAnswerFrom and outstandingAnswerWhere
// verbatim, inside an IN whose aliases shadow the roster's. Restating the four
// clauses here would be a third statement of the rule, and the first to drift.
//
// Parameterised for holderRosterWhere's reason, and with the SAME two arguments
// in the same order: the sub-query re-states the scope rather than inheriting
// it, because its aliases shadow the roster's and an unscoped `IN` here would
// admit another Organization's owing Tickets to this Organization's roster.
const holderRosterOwingOnly = `
	AND tk.id IN (
		SELECT tk.id
	` + outstandingAnswerFrom + `
		WHERE ` + outstandingAnswerWhere + `
		AND s.event_id = $%d AND s.organization_id = $%d
	)
`

// holderRosterHolderJoin reaches the Customer an accepted Holder proved
// themselves to be, so that a row on this list can say WHO is coming and not
// only which Ticket owes what (#329, ADR 0047).
//
// A SEPARATE CONST, ADDED BY ONE CALLER, and deliberately not folded into
// holderRosterFrom. The roster's COUNT has no business joining a person, and
// the debt derivation above must never select one — a join in a shared FROM
// would make a disclosure decision by accident.
//
// LEFT, AND ON THE PRIMARY KEY, so it can neither drop a row nor multiply one: a
// Ticket has at most one holder_customer_id, and that column is NULL on every
// Ticket that was never accepted — which is all of them while the flag is closed.
const holderRosterHolderJoin = `
	LEFT JOIN customers hc ON hc.id = tk.holder_customer_id
`

// TicketSaleHasOutstandingAnswers reports whether ANY Ticket of one Ticket Sale
// still owes a required Ticket Question an Answer (#315).
//
// THE SAME DERIVATION, SCOPED TO ONE SALE INSTEAD OF ONE EVENT. It reuses
// outstandingAnswerFrom and outstandingAnswerWhere untouched and adds a scope of
// its own, which is exactly what the note to #317 at the foot of this file asks
// every new caller to do. Restating the four clauses here would make the
// sentence on a buyer's receipt and the row on the Organization's chase list
// into two different opinions about the same debt — and they would disagree
// first on the retired-question case, which is the one nobody thinks about.
//
// IT NAMES NO ORGANIZATION AND NO EVENT, unlike every other query in this file,
// and that is not a missing clause. Its caller is the Sale Confirmation, which
// runs after a sale has committed and has no actor at all — nobody is asking, so
// there is nobody to scope to. The Ticket Sale id comes from the row that was
// just written rather than from any request, and what it decides is whether one
// sentence appears in an email already addressed to that sale's buyer. There is
// no disclosure here to get wrong: the answer never leaves this process except
// as the presence or absence of a line in a receipt.
//
// EXISTS AND NOT A COUNT, because a count would be a number nobody uses. The
// receipt says "some of these still need answers" and deliberately names no
// figure — the debt is derived live and a number baked into an inbox is wrong
// the moment the buyer answers one — so the query stops at the first row.
func (r *Repository) TicketSaleHasOutstandingAnswers(ctx context.Context, ticketSaleID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
	`+outstandingAnswerFrom+`
			WHERE `+outstandingAnswerWhere+`
			AND s.id = $1
		)
	`, ticketSaleID).Scan(&exists)
	return exists, err
}

// HolderTicket is one Ticket of the Event on the Holder List — the roster —
// with everything needed to say who is coming on it, to chase it and to open
// it (#333).
//
// IT CARRIES THE TICKET SALE'S IDENTITY AND REFERENCE because that is how staff
// reach the Answers at all: the Answers dialog is keyed on a Ticket Sale, and a
// list that named only the Ticket would be a list nobody could act on. The
// buyer's name and email travel for the same reason — chasing means writing to
// somebody, and this list exists to be chased from.
type HolderTicket struct {
	ID string
	// Ordinal is which of its Ticket Sale Line's units this Ticket is,
	// 1..quantity, and the only thing telling two Tickets on one line apart —
	// which is what lets staff say "the second of Ana's four".
	Ordinal        int
	TicketTypeID   string
	TicketTypeName string
	TicketSaleID   string
	// ConfirmationRef is the buyer's own reference, which is what staff on the
	// phone match against and what the Answers dialog names itself after.
	ConfirmationRef string
	// Channel is 'online', 'in_person' or 'import'. It is here to EXPLAIN the
	// row rather than to filter it: a door sale or an import owing every
	// question is a buyer who was never asked, and a surface that could not say
	// so would look like it had lost their Answers.
	Channel           string
	CustomerFirstName string
	CustomerLastName  string
	CustomerEmail     string
	SoldAt            time.Time

	// THE HOLDER (#329, parent #322, ADR 0047). Who this Ticket was handed
	// to, beside what it owes, so that "who is coming and what size are they" is
	// one read rather than two — which is the whole reason this surface was
	// extended instead of a second one being built.
	//
	// FOUR COLUMNS AND NO STATE. The state is DERIVED by catalog.AssignmentState
	// from three of them, in the service, through the same function the buyer's
	// page and the export come through. A `state` column selected here would be a
	// fifth opinion about what these three columns already say.

	// HolderEmail is the address the buyer named, and AssignedAt when they named
	// it. Invalid on an `unassigned` Ticket — and equally invalid on one whose
	// address the retention purge has taken (migration 081), whose record of
	// having been assigned survives only as HolderAddressPurgedAt below.
	//
	// WHAT THE ORGANIZATION IS SHOWN IS NOT DECIDED HERE. This is the repository
	// reporting the row; the disclosure rule — nothing before acceptance — is
	// stated once in the service, where the payload is built.
	HolderEmail sql.NullString
	AssignedAt  sql.NullTime
	// AcceptedAt is when the Holder clicked, and the whole of what `accepted`
	// means. It is also the ONLY thing that makes the two name columns below
	// non-NULL, because migration 080 refuses a holder_customer_id without it.
	AcceptedAt sql.NullTime
	// HolderFirstName and HolderLastName are the accepted Holder's own asserted
	// name, read from the Customer their click minted or matched — never from
	// the Ticket Sale, whose name is the BUYER's and is what this list showed
	// four times over before this feature existed.
	//
	// APART, AS THE BUYER'S TWO ARE, per ADR 0005: which part leads a person's
	// name is the reader's question and not this row's.
	HolderFirstName sql.NullString
	HolderLastName  sql.NullString
	// HolderAddressPurgedAt is migration 081's marker: when the retention purge
	// took an address nobody accepted, or NULL if it never took one. It is NOT a
	// state and catalog.AssignmentState never reads it — but the Holder List
	// derives its assigned-but-never-accepted PRESENTATION from it at read time
	// (#334), so the morning-after sheet can tell "nobody was named" from "named
	// and never claimed". It carries no address; the address is gone by
	// definition.
	HolderAddressPurgedAt sql.NullTime
}

// OutstandingQuestion is one required Ticket Question one Ticket has not
// answered — one Outstanding Answer, named.
//
// The label travels as the Organization COINED it and is never translated (ADR
// 0027), like every other Ticket Question label on every other surface.
type OutstandingQuestion struct {
	TicketID   string
	QuestionID string
	Label      string
	Kind       string
	SortOrder  int
}

// ListHolderTicketsQuery is one reading of the Event's roster: its scope, its
// narrowings, and the page wanted. Every field beyond OrganizationID/EventID is
// optional — a zero value (false, empty string, nil bound) leaves that dimension
// unfiltered — which is what makes the whole roster the query you get by asking
// for nothing.
//
// A STRUCT AND NOT POSITIONAL ARGUMENTS (#523), in the shape of the Sales list's
// repository.ListSalesQuery next door, and this is the argument for it: the
// roster now takes THREE STRING FILTERS beside its two string scopes, and a
// caller that transposed a Ticket Type id with a Sales Channel would compile
// cleanly, run, and quietly return an empty roster. Names at the call site are
// the only thing that catches that. The second reason is growth: #524–#527 add
// the assignment state, the search term, a named question and the sort, and each
// arrives here as a FIELD rather than as a sixth, seventh and eighth positional
// argument that every existing caller must be edited to skip past.
//
// NOTHING HERE IS INTERPOLATED INTO SQL. Every field below reaches the database
// as a placeholder argument (see holderRosterFilters), so a Ticket Type id is a
// value and never a fragment. The sort #527 adds will be the first exception and
// must arrive with an allowlist, as salesSortColumns has.
type ListHolderTicketsQuery struct {
	// The scope, and the security property: BOTH, never only the Event. See
	// holderRosterWhere.
	OrganizationID string
	EventID        string
	// OwingOnly narrows to the Tickets that still owe a required Answer — the
	// Outstanding Answers FILTER (#333). The service closes it while the Ticket
	// Question feature is dark; nothing here knows about flags.
	OwingOnly bool
	// TicketTypeID keeps only the Tickets OF that Ticket Type — the VIP roster
	// apart from general admission (#523).
	//
	// A PLAIN EQUALITY ON THE LINE, and deliberately NOT the Sales list's
	// `EXISTS (SELECT 1 FROM ticket_sale_lines ...)` sub-query. The two lists
	// count different things: a Ticket SALE can span several Ticket Types, so
	// the Sales list needs an existence test to avoid fanning one sale out into
	// several rows or losing half its rollup. A TICKET belongs to exactly one
	// Ticket Type, through the one Ticket Sale Line it was minted on, and the
	// roster's row IS that Ticket. Copying the EXISTS across would be strictly
	// wrong here: it would admit every Ticket of a mixed sale — the GA tickets
	// of a sale that also bought one VIP — to the VIP roster.
	TicketTypeID string
	// Channel keeps only the Tickets of sales made on that Sales Channel:
	// 'online', 'in_person' or 'import'. Equality on the SALE, because a Ticket
	// has no channel of its own — it is how the Ticket was bought.
	//
	// The row already carries the channel to EXPLAIN itself (a door sale owes
	// every question because nobody was ever asked); this makes it a lever, so
	// "the buyers who were never asked" is a query and not a scan.
	Channel string
	// SoldFrom/SoldTo bound the SALE's sold_at as a half-open interval
	// [SoldFrom, SoldTo): the lower bound inclusive, the upper exclusive, both
	// already resolved to absolute time by the service, which is where the
	// Event's timezone is applied. Either may be nil for an open end.
	//
	// HALF-OPEN, AND THE SERVICE PASSES MIDNIGHT OF THE DAY AFTER, which is how
	// "sold to the 3rd" includes the whole of the 3rd without this layer having
	// to know what a day is or how long one is in a zone that changed offset
	// overnight. Exactly the Sales list's shape (ListSalesQuery.SoldFrom), and
	// deliberately so: the two lists must agree about which day a late-night
	// sale fell on.
	SoldFrom *time.Time
	SoldTo   *time.Time
	// WHAT IS DELIBERATELY ABSENT IS A STATUS FILTER, and it is absent for a
	// reason that must survive the next reader (ADR 0065). A Sale Reversal means
	// the Tickets CEASE TO EXIST (ADR 0043): a reversed sale's Tickets are not
	// rows this list is hiding, they are not rows. `s.status = 'active'` in
	// holderRosterWhere is therefore not a default that a filter could widen —
	// there is nothing on the other side of it to show. Adding a status
	// parameter here would produce a roster of people who are not coming and
	// whose money has gone back.
	//
	// SO ARE payment_method AND source, for the plainer reason: they are facts
	// about a SALE with no meaning on a roster, and the Sales list next door
	// already filters by both.
	//
	// Limit and Offset page the result. Limit must be positive; see the refusal
	// at the top of ListHolderTickets.
	Limit  int
	Offset int
}

// holderRosterFilters builds the roster's WHERE and its arguments from one
// query: the scope first, then one condition per active filter.
//
// AN APPENDER AND NOT HAND-NUMBERED PLACEHOLDERS, the idiom ListSales uses.
// With `$1..$4` written out by hand, adding the fifth filter means renumbering
// the ones after it — a change that compiles, runs, and returns the wrong rows
// rather than an error, because every argument here is a string and Postgres
// will happily compare a Ticket Type id to a channel. Here each condition takes
// the placeholder it just appended and can neither know nor care what came
// before it.
//
// THE COUNT AND THE PAGE SHARE THIS, and that sharing is the guarantee that
// pagination totals describe the filtered view rather than the roster behind
// it. Two queries assembling their own conditions would be two views, and the
// screen would say "1 of 12 pages" over four rows.
func holderRosterFilters(q ListHolderTicketsQuery) (string, []any) {
	args := []any{q.EventID, q.OrganizationID}
	eventP, orgP := 1, 2

	where := fmt.Sprintf(holderRosterWhere, eventP, orgP)

	var conds []string
	addCond := func(format string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(format, len(args)))
	}

	if q.OwingOnly {
		// No argument of its own: it re-states the scope already in args.
		where += fmt.Sprintf(holderRosterOwingOnly, eventP, orgP)
	}
	if q.TicketTypeID != "" {
		// Equality on the LINE's Ticket Type — see the field's comment for why
		// this is not the Sales list's EXISTS.
		addCond(`l.ticket_type_id = $%d`, q.TicketTypeID)
	}
	if q.Channel != "" {
		addCond(`s.channel = $%d`, q.Channel)
	}
	if q.SoldFrom != nil {
		addCond(`s.sold_at >= $%d`, *q.SoldFrom)
	}
	if q.SoldTo != nil {
		addCond(`s.sold_at < $%d`, *q.SoldTo)
	}

	for _, cond := range conds {
		where += "\n\t\tAND " + cond
	}
	return where, args
}

// ListHolderTickets returns a page of the Event's ROSTER — every Ticket of
// every live Ticket Sale — oldest sale first, together with how many Tickets
// the view holds (#333, filters #523).
//
// EVERY TICKET AND NOT EVERY TICKET THAT OWES, which is the ruling on #333:
// the Holder List is the Organization's answer to "who is coming", a fully
// answered Ticket stays on it, and an Event that asks no questions still has
// one. What a Ticket owes hangs off the row, from
// ListOutstandingQuestionsForTickets, and OwingOnly narrows the roster to the
// Tickets that owe — Outstanding Answers as a FILTER of this list, never its
// definition.
//
// OLDEST SALE FIRST, and not newest as the Sales list is. When the filter is
// on this is a chase list — the buyer who paid in January and has said nothing
// since belongs at the top — and the roster keeps the same order so switching
// the filter reorders nobody.
//
// The total counts the TICKETS the current view holds, so it agrees with the
// rows being paged, however the filters are set: the two queries take their
// WHERE from the same holderRosterFilters call and cannot describe two views.
func (r *Repository) ListHolderTickets(
	ctx context.Context,
	q ListHolderTicketsQuery,
) ([]HolderTicket, int, error) {
	// A non-positive Limit would mean LIMIT 0: no rows, and a caller handed a
	// confident empty roster instead of an error. Refuse it rather than serve
	// it — ListSales refuses the same thing for the same reason.
	if q.Limit <= 0 {
		return nil, 0, fmt.Errorf("catalog: ListHolderTickets requires a positive Limit, got %d", q.Limit)
	}

	where, args := holderRosterFilters(q)

	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
	`+holderRosterFrom+`
		WHERE `+where,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}
	// A page past the end still reports the true total, so a surface can say how
	// many there are rather than appearing to have emptied. Same arrangement the
	// Sales list has.
	if total == 0 {
		return []HolderTicket{}, 0, nil
	}

	// The page's own two arguments, appended AFTER the filters' so the numbering
	// depends on how many filters were active rather than on a constant nobody
	// remembers to update.
	pageArgs := append(append([]any{}, args...), q.Limit, q.Offset)
	rows, err := r.db.Pool.QueryContext(ctx, fmt.Sprintf(`
		SELECT tk.id, tk.ordinal, l.ticket_type_id, tt.name,
		       s.id, s.confirmation_ref, s.channel,
		       s.customer_first_name, s.customer_last_name, s.customer_email, s.sold_at,
		       tk.holder_email, tk.assigned_at, tk.accepted_at, tk.holder_address_purged_at,
		       hc.first_name, hc.last_name
	`+holderRosterFrom+holderRosterHolderJoin+`
		WHERE `+where+`
		ORDER BY s.sold_at ASC, s.id ASC, tk.ordinal ASC
		LIMIT $%d OFFSET $%d
	`, len(args)+1, len(args)+2), pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tickets := make([]HolderTicket, 0)
	for rows.Next() {
		var t HolderTicket
		if err := rows.Scan(
			&t.ID, &t.Ordinal, &t.TicketTypeID, &t.TicketTypeName,
			&t.TicketSaleID, &t.ConfirmationRef, &t.Channel,
			&t.CustomerFirstName, &t.CustomerLastName, &t.CustomerEmail, &t.SoldAt,
			&t.HolderEmail, &t.AssignedAt, &t.AcceptedAt, &t.HolderAddressPurgedAt,
			&t.HolderFirstName, &t.HolderLastName,
		); err != nil {
			return nil, 0, err
		}
		tickets = append(tickets, t)
	}
	return tickets, total, rows.Err()
}

// ListOutstandingQuestionsForTickets names which required questions each of the
// given Tickets owes.
//
// ONE QUERY FOR THE WHOLE PAGE rather than one per Ticket, the same shape
// ListTicketAnswers uses: a page of fifty Tickets across three questions is one
// read, not fifty.
//
// It re-applies the SCOPE as well as the ids. The ids came from
// ListTicketsOwingAnswers and are already this Organization's, so the scope is
// redundant — and it stays because the day something else passes ids in from
// somewhere less careful, the redundancy is the thing that refuses.
func (r *Repository) ListOutstandingQuestionsForTickets(
	ctx context.Context,
	organizationID, eventID string,
	ticketIDs []string,
) ([]OutstandingQuestion, error) {
	if len(ticketIDs) == 0 {
		return nil, nil
	}

	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT tk.id, q.id, q.label, q.kind, q.sort_order
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope+`
		AND tk.id = ANY($3)
		ORDER BY q.sort_order ASC, q.created_at ASC
	`, eventID, organizationID, ticketIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := make([]OutstandingQuestion, 0)
	for rows.Next() {
		var q OutstandingQuestion
		if err := rows.Scan(&q.TicketID, &q.QuestionID, &q.Label, &q.Kind, &q.SortOrder); err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}

// CountOutstandingAnswers is how many Outstanding Answers an Event carries in
// all — debts and not Tickets, so a Ticket owing three counts three.
//
// The headline figure the surface leads with, and the one an Organization means
// by "how much don't I know yet". Counted in its own query rather than summed
// from a page, because a page is a page.
func (r *Repository) CountOutstandingAnswers(ctx context.Context, organizationID, eventID string) (int, error) {
	var total int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
	`+outstandingAnswerFrom+`
		WHERE `+outstandingAnswerWhere+outstandingAnswerScope,
		eventID, organizationID,
	).Scan(&total)
	return total, err
}

// A NOTE FOR #317, THE ANSWER REMINDER.
//
// The reminder sweeps ACTIVE TICKET SALES that have any Outstanding Answer,
// which is a different SELECT and a different grouping over exactly the same
// derivation: `SELECT s.id, ... ` + outstandingAnswerFrom + ` WHERE ` +
// outstandingAnswerWhere + a scope of its own, grouped by s.id. Reuse those two
// constants rather than restating the four clauses, and reuse
// catalog.IsOutstandingAnswer for anything decided in Go. The rationing per
// Ticket Sale and the silence once the Event has started are the REMINDER's
// rules and belong to it — they are about mailing, not about the debt, and
// pushing them down into this derivation would make the #313 list disagree with
// itself the moment an Event began.
