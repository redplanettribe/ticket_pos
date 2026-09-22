package repository

import (
	"github.com/peter/ticket_pos/backend/internal/catalog"

	"context"
	"database/sql"
	"fmt"
	"strings"
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

// holderRosterOwingQuestion narrows the roster to the Tickets owing ONE NAMED
// Ticket Question (#525, ADR 0065) — "who still hasn't told me their shirt
// size", which on an Event asking several questions is a different chase from
// "who owes anything at all".
//
// IT IS holderRosterOwingOnly WITH ONE CLAUSE ADDED, AND THAT IS THE WHOLE
// DESIGN. The same outstandingAnswerFrom and outstandingAnswerWhere, verbatim,
// inside an IN whose aliases shadow the roster's — with `AND q.id = $%d` and
// nothing else. NOTHING HERE DECIDES WHAT IS OUTSTANDING: the debt is defined
// once, in catalog.IsOutstandingAnswer and in the four clauses of
// outstandingAnswerWhere beside it, and this filter NARROWS THAT ANSWER rather
// than restating it.
//
// THE REJECTED ALTERNATIVE WAS AN `EXISTS` SPELLING THE DEBT OUT AGAIN —
// `EXISTS (SELECT 1 FROM ticket_answers ... WHERE q.id = $n AND q.required AND
// a.id IS NULL)` — which reads perfectly well and is the thing to refuse. It
// would be a THIRD statement of a rule that already, deliberately, lives twice;
// and it would drift first on THE RETIRED-QUESTION CASE, which is the one
// nobody thinks about. A named-question filter written that way would happily
// return Tickets "owing" a question the Organization has stopped asking — a
// chase list with no working button behind it, disagreeing with the very
// `outstanding` checkbox beside it on the same screen. Because the clauses are
// reused rather than copied, the retired case, the optional case, the reversed
// sale and the approval gate all follow this filter for free, and the
// integration tests prove it by asserting `question_id` finds nothing wherever
// `outstanding` says nothing is owed.
//
// IT DOES NOT REPLACE holderRosterOwingOnly AND COMPOSES WITH IT. Both may be
// applied at once — `outstanding=true&question_id=X` is two INs and means
// exactly what `question_id=X` alone means, since owing X implies owing
// something. Keeping them independent is what lets the checkbox stay exactly as
// it was.
//
// Parameterised for holderRosterWhere's reason, with the same two scope
// arguments in the same order and the question third; the scope is re-stated
// rather than inherited because the aliases shadow the roster's.
const holderRosterOwingQuestion = `
	AND tk.id IN (
		SELECT tk.id
	` + outstandingAnswerFrom + `
		WHERE ` + outstandingAnswerWhere + `
		AND s.event_id = $%d AND s.organization_id = $%d
		AND q.id = $%d
	)
`

// holderRosterHolderJoin reaches the Customer an accepted Holder proved
// themselves to be, so that a row on this list can say WHO is coming and not
// only which Ticket owes what (#329, ADR 0047).
//
// A SEPARATE CONST, ADDED BY THE ROSTER'S TWO QUERIES AND BY NOTHING ELSE, and
// deliberately not folded into holderRosterFrom: the debt derivation above must
// never select a person, and a join in a shared FROM would make a disclosure
// decision by accident.
//
// IT USED TO BE ADDED BY THE PAGE ALONE, on the argument that "the roster's
// COUNT has no business joining a person". THAT STOPPED BEING TRUE WITH #526,
// and the note is corrected here rather than left to mislead. The search
// predicate (holderRosterSearch below) matches an accepted Holder's NAME, which
// lives on this Customer row and nowhere on `tickets` — so the COUNT must join
// it too or the two queries would be reading different columns. A COUNT that
// dropped the join would not error: it would silently count more rows than the
// page can show, and the screen would say "1 of 3 pages" over a single page of
// results. A total and a page that disagree are worse than either being wrong,
// because nothing on the screen says which one to believe.
//
// LEFT, AND ON THE PRIMARY KEY, so it can neither drop a row nor multiply one —
// which is what makes it safe under COUNT(*) as well as under the page: a Ticket
// has at most one holder_customer_id, and that column is NULL on every Ticket
// that was never accepted, which is all of them while the flag is closed. An
// INNER join here would empty the roster of every unaccepted Ticket, and a join
// on anything but the primary key would double-count a buyer.
const holderRosterHolderJoin = `
	LEFT JOIN customers hc ON hc.id = tk.holder_customer_id
`

// holderRosterOwesJoin counts, per Ticket, HOW MUCH THAT TICKET OWES — the
// number the `owes` sort orders on (#527, ADR 0065), so an Organization can put
// the worst offenders at the top and chase them first.
//
// IT REUSES THE DEBT'S OWN SQL, outstandingAnswerFrom and outstandingAnswerWhere
// VERBATIM, exactly as holderRosterOwingOnly and holderRosterOwingQuestion do.
// The header of this file says why the rule lives exactly twice, in
// catalog.IsOutstandingAnswer and in those four clauses; a third statement of it
// written to make a column to sort on would be the FIRST to drift, and it would
// drift on the retired-question case, which is the one nobody thinks about — a
// list ordered by a debt that includes questions the Organization has stopped
// asking, sitting beside an Owes column that does not. The two would disagree on
// the same screen, in the same row, and the sort would look like a rendering bug.
//
// A GROUPED DERIVED TABLE AND NOT A CORRELATED SCALAR SUB-QUERY, and the reason
// is mechanical rather than a preference. The debt's FROM binds the aliases `tk`,
// `l`, `s` and `tt` — the SAME aliases the roster binds — so inside a correlated
// sub-query `tk.id` would resolve to the INNER Ticket and the correlation could
// not be written at all. Grouping by the inner `tk.id` and correlating in the
// JOIN's own ON clause, where only the outer aliases are in scope, is the same
// answer with the shadowing moved somewhere it cannot bite. `COUNT(*)` over
// outstandingAnswerFrom counts (Ticket, required question) pairs, which is what
// the Owes column lists and therefore what a reader means by "owes more".
//
// IT CANNOT CHANGE A COUNT, and that is what makes it safe to add to the page
// query alone: the GROUP BY yields at most one row per Ticket and the join is
// LEFT and on that Ticket's primary key, so it can neither drop a row nor
// multiply one — holderRosterHolderJoin's property, for holderRosterHolderJoin's
// reason. The COUNT query does not carry it because nothing in a WHERE reads it;
// an ORDER BY is the only thing that ever will.
//
// A TICKET OWING NOTHING HAS NO ROW HERE AND SORTS AS ZERO, through the
// COALESCE in holderSortColumns. It is still on the roster: this is a sort, not
// the `outstanding` filter, and a fully answered Ticket stays on the list.
//
// Parameterised for holderRosterWhere's reason, and with the SAME two scope
// arguments in the same order: the sub-query re-states the scope rather than
// inheriting it, because its aliases shadow the roster's and an unscoped
// derivation here would count another Organization's debts against this
// Organization's Tickets.
const holderRosterOwesJoin = `
	LEFT JOIN (
		SELECT tk.id AS ticket_id, COUNT(*) AS owed
	` + outstandingAnswerFrom + `
		WHERE ` + outstandingAnswerWhere + `
		AND s.event_id = $%d AND s.organization_id = $%d
		GROUP BY tk.id
	) owes ON owes.ticket_id = tk.id
`

// holderRosterSearch is the roster's search predicate: one case-insensitive
// substring over the buyer's name and address, the Sale Confirmation reference,
// and — FOR AN ACCEPTED HOLDER ONLY — that Holder's name and address (#526,
// ADR 0065).
//
// SEARCHABLE IF AND ONLY IF DISPLAYABLE, AND THAT IS THE WHOLE OF THIS
// PREDICATE. A Holder who has not accepted their Ticket Assignment is named
// nowhere on this platform: ADR 0047 withholds the address because it has no
// consent moment behind it and the person may not know a ticket was bought for
// them. The address is nonetheless sitting in `tickets.holder_email`, so a
// search that matched it would answer, one address at a time, precisely the
// question the non-disclosure exists to refuse — type an address, get a row, and
// the empty Holder cell now means *yes, they are on this list*. The display rule
// would survive in the markup and die in the query. This predicate is the
// QUERY-SIDE TWIN of service.fillHolderListEntry, and the two must be read
// together.
//
// THE ACCEPTANCE TEST IS INSIDE THE HOLDER BRANCH OF THE `OR`, NOT A TOP-LEVEL
// `AND`, and not a filter applied to rows in Go afterwards. This is the one
// structural decision of the ticket, and the shape is chosen so the mistake is
// impossible rather than merely unmade:
//
//   - As a top-level `AND tk.accepted_at IS NOT NULL` the search would narrow
//     the WHOLE roster to accepted Tickets — searching a buyer's name would lose
//     every unaccepted Ticket of that buyer's sale — so the clause would have to
//     be deleted to make the obvious bug go away, and deleting it silently
//     restores the disclosure.
//
//   - As a post-filter in Go it would run over ONE PAGE of rows, so the COUNT
//     would still describe the wider view; the leak would be visible in
//     `pagination.total` even where no row was drawn.
//
//   - Written as it is, DELETING THE ACCEPTANCE CLAUSE BREAKS A TEST rather than
//     widening a result quietly:
//     integration.TestSearchingAnUnacceptedHoldersAddressReturnsZeroRows.
//
// THE EMAIL MATCHED IS `tk.holder_email` AND NOT `hc.email`, deliberately, and
// this is the searchable-set-equals-displayed-set property stated in columns:
// fillHolderListEntry discloses `ticket.HolderEmail` — the address ON THE
// TICKET, the one the buyer typed and the Holder proved — and never the Customer
// row's own. The two are the same address today, because acceptance is what
// binds them; matching a different column would make the two sets the same only
// by coincidence, and the coincidence would end the first time a Customer
// changed their address after accepting. The NAMES come from `hc`, because that
// is where fillHolderListEntry takes them from: a Ticket has no name on it.
//
// A PURGED ADDRESS THEREFORE MATCHES NOTHING, and needs no clause of its own.
// The purge nulls `holder_email` and a NULL never satisfies ILIKE; the purged
// Ticket was by definition never accepted, so it fails the acceptance test as
// well. Two reasons, and the integration tests assert the outcome rather than
// trusting either.
//
// THE SEMANTICS ARE THE SALES LIST'S, clause for clause — ILIKE, likeEscape'd so
// the term is a literal and not a pattern, and the name matched as
// `first || ' ' || last` — because "search" must mean the same thing on the two
// screens an Organizer moves between. See sales/repository.ListSales; a pg_trgm
// index (ADR 0006) is the documented upgrade path if one Event's volume ever
// makes this scan too slow.
//
// THE KNOWN AND ACCEPTED COST, so it is not re-litigated as a bug: an Organizer
// who typed an address into a Ticket Assignment CANNOT LATER SEARCH FOR IT, and
// must find the row by its buyer or its Sale Confirmation reference. This will
// be reported as a defect one day. It is working as designed, it is ADR 0065's
// stated price for ADR 0047, and widening it is an ADR, not a patch.
//
// ONE PLACEHOLDER, REFERENCED FIVE TIMES with an explicit argument index, so the
// term is bound once and the appender in holderRosterFilters can hand it a
// single position — `%[1]d` and not five `%d`, which would need the same number
// written five times and would be renumbered wrong the day a filter moves.
const holderRosterSearch = `(
			s.customer_email ILIKE $%[1]d
			OR (s.customer_first_name || ' ' || s.customer_last_name) ILIKE $%[1]d
			OR s.confirmation_ref ILIKE $%[1]d
			OR (
				tk.accepted_at IS NOT NULL
				AND (
					tk.holder_email ILIKE $%[1]d
					OR (hc.first_name || ' ' || hc.last_name) ILIKE $%[1]d
				)
			)
		)`

// holderRosterLikeEscape escapes the LIKE/ILIKE metacharacters (\, %, _) so a
// search term is matched as a LITERAL SUBSTRING and never as a pattern. The
// backslash is Postgres's default ILIKE escape character.
//
// A SECOND COPY OF sales/repository.likeEscape, FOUR LINES, AND DELIBERATE. The
// alternative is the catalog module importing the sales module — or a fourth
// package invented to hold four lines — over a piece of Postgres trivia, which
// is the coupling resolveEventLocation in the service beside this already
// refuses on the same terms. What matters is that the ANSWER agrees, and the
// integration tests assert the property directly: a `%` or a `_` typed into the
// box matches those characters and nothing else.
func holderRosterLikeEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "%", `\%`)
	s = strings.ReplaceAll(s, "_", `\_`)
	return s
}

// holderRosterAssignmentStates is the assignment-state FILTER, in SQL: one
// predicate per selectable value, over the SAME FOUR COLUMNS the Holder List
// already reports a state from (#524, ADR 0065).
//
// `never_accepted` IS A VALUE OF THIS FILTER AND NOT A FOURTH ASSIGNMENT STATE.
// catalog.AssignmentState still knows three — #331 rejected a fourth outright
// and nothing here reopens it. What the fourth value selects is the
// PRESENTATION service.fillHolderListEntry already derives at read time from
// migration 081's purge marker: somebody was named, nobody accepted, and the
// address is gone by definition. "Who did I name who never claimed their
// ticket" is the morning-after question, and after the Event starts it is
// otherwise indistinguishable from "nobody was named".
//
// THE SAME RULE LIVES TWICE, IN GO AND IN SQL, exactly as the debt's does above
// — and for the same reason: an Event's ticket roll cannot be filtered in Go.
// service.fillHolderListEntry is the statement of it and these four predicates
// are the same tests in the same order, clause for clause:
//
//   - `accepted` — catalog.AssignmentState returns TicketAccepted for
//     `acceptedAt != nil` and tests it FIRST, because acceptance is the
//     strongest fact. `tk.accepted_at IS NOT NULL` is that test, and it is what
//     makes every predicate below able to start from "not accepted".
//
//   - `never_accepted` — fillHolderListEntry's `state != TicketAccepted &&
//     HolderAddressPurgedAt.Valid`. `state != accepted` IS `accepted_at IS
//     NULL`, because acceptance is the only way into that state.
//
//   - `assigned` — TicketAssigned's `holderEmail != "" && assignedAt != nil`,
//     with the purge marker excluded so the two values cannot both match one
//     Ticket. The email test is NULL-safe on purpose: the column is nullable and
//     the Go side reads a NULL as "", so `IS NOT NULL AND <> ”` is the one
//     comparison that agrees with it.
//
//   - `unassigned` — TicketUnassigned, which is the fall-through: neither
//     accepted nor purged, and missing either half of an assignment.
//
// A PURGED TICKET IS ON `never_accepted` AND ON NOTHING ELSE — in particular
// NOT on `assigned`, even though its row reports `assignment_state:
// "assigned"` on the wire. This is the one deliberate divergence between the
// filter and the field beside it, and it is chosen so THE FOUR VALUES PARTITION
// THE ROSTER: every Ticket matches exactly one, so the four filtered counts sum
// to the unfiltered total and a reader can trust that the values do not overlap
// or leak. Counting a purged Ticket under both `assigned` and `never_accepted`
// would make the four counts sum to more than the roster, and leaving it out of
// both would make them sum to less — the second is worse, because a Ticket
// nothing selects is a Ticket a filtered export silently drops.
//
// The divergence costs nothing on the screen: the row's BADGE reads "never
// accepted" and not "assigned" (the staff app's holderStateKey picks the word
// off the same marker), so the filter agrees with what the reader is looking
// at. The wire field stays `assigned` because it is the Ticket's STATE, and its
// state is what fillHolderListEntry says it is.
//
// NEITHER STATEMENT MAY BE CHANGED ALONE. These predicates and
// fillHolderListEntry are one rule written twice, held together by the Holder
// List's integration tests — the partition test in particular, which is the one
// that notices a clause added to one side and not the other.
var holderRosterAssignmentStates = map[string]string{
	"accepted": `tk.accepted_at IS NOT NULL`,
	"never_accepted": `tk.accepted_at IS NULL
			AND tk.holder_address_purged_at IS NOT NULL`,
	"assigned": `tk.accepted_at IS NULL
			AND tk.holder_address_purged_at IS NULL
			AND tk.holder_email IS NOT NULL AND tk.holder_email <> ''
			AND tk.assigned_at IS NOT NULL`,
	"unassigned": `tk.accepted_at IS NULL
			AND tk.holder_address_purged_at IS NULL
			AND (tk.holder_email IS NULL OR tk.holder_email = '' OR tk.assigned_at IS NULL)`,
}

// The Holder List's FIVE SORTS (#527, ADR 0065). Everything down to
// holderOrderBy is one decision written in three pieces: what may be sorted on,
// what counts as a blank, and how the two are assembled with a tiebreak beneath
// them.

// holderSortSoldAt is the DEFAULT, and holderSortOwes is the one that belongs to
// a feature flag. Named rather than spelled as literals because both are tested
// against by name — the fallback below and the service's dark-flag rule.
const (
	holderSortSoldAt = "sold_at"
	holderSortOwes   = "owes"
)

// holderSortColumns maps an allowlisted sort key to the ordered list of primary
// ORDER BY columns for that sort. Because these values come from a fixed
// allowlist — enforced here by the map lookup itself, and again in the handler —
// they are safe to interpolate into the query; nothing a caller types reaches
// the SQL. Same arrangement as sales/repository.salesSortColumns, deliberately,
// because the two staff lists must not have two ideas about sorting.
//
// `sold_at` CARRIES THREE COLUMNS AND NOT ONE, AND THAT IS THE WHOLE OF "THE
// DEFAULT ORDER DOES NOT CHANGE". The roster has always come back
// `s.sold_at, s.id, tk.ordinal`, and those last two are ORDER and not decoration:
// they keep one buyer's four Tickets together and in the order they were minted,
// which is what the screen shows today and what several tests assert. Reducing
// them to a bare id tiebreak would scatter a sale's Tickets into UUID order — a
// silent reordering of a screen people already use, which is precisely the change
// this ticket exists to refuse.
//
// `buyer` IS THE SALES LIST'S `customer`, CLAUSE FOR CLAUSE: last name then
// first, so a roster and a ledger read the same way to the same person.
//
// `holder` READS THE JOINED CUSTOMER ROW (hc) and never `tickets` — a Ticket has
// no name on it, and the name exists only because somebody ACCEPTED. Its blanks
// are the subject of holderSortBlanksLast below.
//
// `ticket_type` SORTS BY `tt.sort_order`, THE EVENT'S OWN CATALOG DISPLAY ORDER,
// and deliberately NOT by `tt.name`. An Organization orders its catalog on
// purpose — Early Bird, General, VIP — and alphabetically that is General, VIP,
// Early Bird, which is nobody's idea of the list. The Sales Export's columns and
// the Trends' legend already read `tt.sort_order` for the same reason, and a
// third surface disagreeing with them would look like a bug in one of the three.
//
// `owes` SORTS ON THE DEBT COUNT holderRosterOwesJoin derives, COALESCEd because
// a Ticket owing nothing has no row in that derivation and must sort as zero
// rather than as NULL. It is the one key here that needs a join of its own, which
// is why holderOrderBy reports whether it was chosen.
var holderSortColumns = map[string][]string{
	holderSortSoldAt: {"s.sold_at", "s.id", "tk.ordinal"},
	"buyer":          {"s.customer_last_name", "s.customer_first_name"},
	"holder":         {"hc.last_name", "hc.first_name"},
	"ticket_type":    {"tt.sort_order"},
	holderSortOwes:   {"COALESCE(owes.owed, 0)"},
}

// holderSortBlanksLast names, per sort, the expression that is 1 when the row's
// key is BLANK and 0 when it is not. It is applied as a LEADING ORDER BY key,
// ALWAYS ASCENDING, whichever direction the reader asked for.
//
// THIS DELIBERATELY BREAKS THE CONVENTION THAT DESCENDING IS THE REVERSE OF
// ASCENDING, and it is not an oversight to be tidied up. On a real Event most
// Tickets have no accepted Holder — every Ticket nobody was named for, every one
// named and never claimed, and ALL of them on a build where Ticket Assignment is
// closed. Under the conventional flip, one of the two directions opens on three
// hundred empty cells and the reader scrolls past all of them to reach the first
// name: the control is useless in half its range, which is a worse defect than
// the inconsistency. ADR 0065 records the choice and the alternative it beat.
// Sorting Z→A must therefore be read as "names, backwards, then the blanks" and
// never as "the ascending page, upside down".
//
// A CASE EXPRESSION AND NOT `NULLS LAST`, and the difference is not cosmetic:
// `NULLS LAST` would miss half the blanks. `customers.first_name` and
// `last_name` are `TEXT NOT NULL`, so a Holder who accepted their Assignment
// and never got round to naming themselves has EMPTY STRINGS and not NULLs —
// a row that draws as an empty cell, sorts to the very front of an ascending
// list under `NULLS LAST`, and would make the option look broken on exactly the
// Events that use it. The CASE tests what the reader sees is blank, which is the
// property this rule is actually about; `hc.last_name IS NULL` (an unaccepted
// Ticket, and the common case) and `hc.last_name` holding an empty string (an
// accepted Holder who never named themselves) both answer 1.
//
// ONLY `holder` IS IN THIS MAP, AND THE OTHER FOUR WERE CHECKED RATHER THAN
// ASSUMED. `sold_at` and `ticket_type` are NOT NULL columns of rows that must
// exist for a Ticket to exist at all. `owes` has no blank: a Ticket owing
// nothing is a ZERO and not an absence, and zero is a meaningful end of that
// scale — pushing it last would hide precisely the Tickets a reader sorting
// ascending is looking for. `buyer` is the near miss and is deliberately left
// out: `ticket_sales.customer_first_name`/`customer_last_name` are NOT NULL and
// a Manually Recorded Sale may leave one half empty, but never both — a sale has
// a buyer by construction, so the column is not blank-heavy and a handful of
// half-names is not worth breaking the convention twice.
var holderSortBlanksLast = map[string]string{
	"holder": `CASE WHEN COALESCE(hc.last_name, '') = '' AND COALESCE(hc.first_name, '') = ''
			THEN 1 ELSE 0 END`,
}

// holderOrderBy builds the roster's ORDER BY from a validated sort key and
// direction, and reports whether the debt-count join holderRosterOwesJoin has to
// be added for it. An unrecognised key falls back to the default — oldest sale
// first — rather than erroring, which is the same leniency every other parameter
// on this list has and what makes a stale bookmark a roster instead of a 500.
//
// THE DIRECTION DEFAULTS TO ASCENDING, and this list is the one place on the
// platform where that is right: the Sales list opens on the newest sale because
// it is a ledger being watched, and the roster opens on the oldest because it is
// a list being worked through. `dir` is compared case-insensitively for the
// handler's leniency, and anything that is not `desc` is ascending.
//
// EVERY SORT ENDS IN `tk.id`, AND THAT IS NOT OPTIONAL. Without a key that is
// unique per row, two Tickets with equal primary values may come back in one
// order on page 1 and another on page 2 — which does not merely look untidy, it
// DUPLICATES one Ticket across the two pages and DROPS another entirely, and a
// roster that loses a person is worse than one that is badly ordered. `tk.id` is
// the narrowest thing that is genuinely unique here: the row IS a Ticket, the
// column is the table's primary key, and it is already selected by the page
// query. Neither `s.id` nor `tk.ordinal` would do on its own — a sale of four
// Tickets shares one, and two lines of one sale each start their ordinals at 1.
//
// The tiebreak takes the reader's direction, as salesOrderBy's does. Which way
// it points cannot affect stability — it only has to be the SAME way on every
// page of one view — and following the direction keeps a reversed list the exact
// reverse of itself wherever the blanks rule is not in play.
func holderOrderBy(sort, dir string) (string, bool) {
	cols, ok := holderSortColumns[sort]
	if !ok {
		sort = holderSortSoldAt
		cols = holderSortColumns[sort]
	}
	direction := "ASC"
	if strings.EqualFold(dir, "desc") {
		direction = "DESC"
	}

	parts := make([]string, 0, len(cols)+2)
	// The blanks key first, and ASC in both directions — see holderSortBlanksLast.
	if blank, blanksLast := holderSortBlanksLast[sort]; blanksLast {
		parts = append(parts, blank+" ASC")
	}
	for _, col := range cols {
		parts = append(parts, col+" "+direction)
	}
	parts = append(parts, "tk.id "+direction)
	return "ORDER BY " + strings.Join(parts, ", "), sort == holderSortOwes
}

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

	// CustomerID is the buyer's Customer, and HolderCustomerID the Customer who
	// accepted the Ticket (NULL until accepted) — the ids the Holder List links
	// each person's Customer Dossier by (#640). The Holder's goes out only
	// through catalog.DiscloseHolder, like the rest of the Holder.
	CustomerID       string
	HolderCustomerID sql.NullString
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
	// QuestionID keeps only the Tickets owing ONE NAMED Ticket Question (#525)
	// — the shirt sizes still missing, rather than every debt of every kind.
	//
	// IT NARROWS THE EXISTING ANSWER AND DEFINES NOTHING. See
	// holderRosterOwingQuestion: the sub-query is the debt's own FROM and WHERE
	// with `AND q.id = $n` added, so a retired question, an optional one, an
	// unapproved one and a reversed sale answer here exactly as they answer to
	// OwingOnly — because it is the same four clauses and not a second opinion
	// about them.
	//
	// IT COMPOSES WITH OwingOnly rather than replacing it: both set is two INs
	// and the same rows, since owing this question implies owing something. The
	// service closes it while the Ticket Question feature is dark, beside
	// OwingOnly; nothing here knows about flags.
	QuestionID string
	// Search is a case-insensitive substring matched over the BUYER's name and
	// address, the Sale Confirmation reference, and — for an ACCEPTED Holder
	// only — that Holder's name and address (#526). Empty means no search.
	//
	// SEARCHABLE IF AND ONLY IF DISPLAYABLE. The acceptance test lives INSIDE
	// the Holder branch of the predicate, never as a condition applied beside
	// it and never as a filter over rows in Go: an unaccepted address is in
	// `tickets.holder_email` and is named nowhere on this platform (ADR 0047),
	// so matching it would answer one address at a time the question the
	// non-disclosure exists to refuse. See holderRosterSearch, which is where
	// that is argued and where it is enforced.
	//
	// NEVER WRITTEN TO A LOG. It is a customer's address, and a log aggregator
	// is a wider audience than the database (ADR 0065). Nothing on the read
	// path logs it today — the request middleware logs `r.URL.Path` and not the
	// query string, and no layer between the handler and here logs its
	// arguments — and this note is here so that a future logger added to this
	// repository knows to redact the field rather than discovering it in an
	// audit. #529's audit line records only THAT a search was applied.
	Search string
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
	// AssignmentState keeps only the Tickets standing in one place with their
	// Holder: `unassigned`, `assigned`, `accepted` — or `never_accepted`, which
	// is a VALUE OF THIS FILTER AND NOT A FOURTH STATE (#524). See
	// holderRosterAssignmentStates for the predicates and for why a purged
	// Ticket answers to that value alone.
	//
	// A STRING AND NOT catalog.TicketAssignmentState, unlike the field it
	// filters. The type has three values and this has four, so typing it as the
	// enum would be an invitation to add the fourth member — which is exactly
	// what #331 refused. Anything unrecognised is IGNORED here rather than
	// matched, so no caller can turn a typo into an empty roster.
	AssignmentState string
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

	// Sort and Dir choose the order — one of the five keys in
	// holderSortColumns, and `asc` or `desc` (#527). THE ONLY FIELDS ON THIS
	// STRUCT THAT ARE INTERPOLATED INTO SQL RATHER THAN BOUND AS ARGUMENTS,
	// which the note at the head of this type warned would one day be true: an
	// ORDER BY cannot be a placeholder. They arrive with an ALLOWLIST for
	// exactly that reason, and holderOrderBy answers an unrecognised value with
	// the default order rather than with an error, so a hand-edited URL is a
	// roster and not a 500.
	//
	// EMPTY MEANS THE DEFAULT — oldest sale first — and the default is
	// UNCHANGED from what this list has always returned. See holderSortColumns:
	// `sold_at` carries `s.id` and `tk.ordinal` beneath it because those were
	// always part of the order, not a tiebreak bolted on.
	//
	// `owes` BELONGS TO TICKET QUESTIONS and the service drops it while that
	// feature is dark, beside OwingOnly and QuestionID — at which point this
	// field arrives empty and the roster comes back in the default order.
	// Nothing here knows about flags.
	Sort string
	Dir  string

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
// holderRosterScopeEventArg and holderRosterScopeOrgArg are the positions of the
// two scope arguments, which holderRosterFilters appends FIRST and every
// sub-query that re-states the scope reads by name.
//
// CONSTANTS AND NOT TWO LOCALS, since #527. The owes join (holderRosterOwesJoin)
// is assembled by ListHolderTickets rather than by the filter builder, so it
// would otherwise have to write `1` and `2` by hand on the strength of a comment
// — the exact fragility holderRosterWhere stopped being a constant to avoid.
const (
	holderRosterScopeEventArg = 1
	holderRosterScopeOrgArg   = 2
)

func holderRosterFilters(q ListHolderTicketsQuery) (string, []any) {
	args := []any{q.EventID, q.OrganizationID}
	eventP, orgP := holderRosterScopeEventArg, holderRosterScopeOrgArg

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
	if q.QuestionID != "" {
		// ONE ARGUMENT OF ITS OWN, appended here rather than through addCond,
		// because this filter is a whole sub-query joined to the WHERE and not
		// a condition sitting beside the others — exactly as OwingOnly above.
		// Its placeholder is `len(args)` for the appender's reason one comment
		// up: a number written by hand is a number the next filter renumbers by
		// accident, and every argument here is a string that Postgres would
		// happily compare to the wrong column.
		//
		// APPENDING TO `where` AHEAD OF `conds` CHANGES NOTHING about the
		// numbering: a placeholder names its argument by position in args and
		// not by where it appears in the statement.
		args = append(args, q.QuestionID)
		where += fmt.Sprintf(holderRosterOwingQuestion, eventP, orgP, len(args))
	}
	if q.Search != "" {
		// ONE ARGUMENT BOUND ONCE and referenced five times by index, wrapped in
		// the `%` that make it a SUBSTRING and escaped so the reader's own `%`
		// or `_` is a literal — the Sales list's semantics exactly, because the
		// two screens must not mean different things by "search".
		//
		// WHAT THIS BRANCH MATCHES AND WHY IT REFUSES THE REST is
		// holderRosterSearch's subject, and it is the security decision of this
		// list: the acceptance test is inside the predicate, in the Holder
		// branch of the OR, and NOT added here as a condition beside it. A
		// clause added here instead would narrow the whole roster rather than
		// the Holder branch, would look like a bug, and would be deleted.
		addCond(holderRosterSearch, "%"+holderRosterLikeEscape(q.Search)+"%")
	}
	if q.TicketTypeID != "" {
		// Equality on the LINE's Ticket Type — see the field's comment for why
		// this is not the Sales list's EXISTS.
		addCond(`l.ticket_type_id = $%d`, q.TicketTypeID)
	}
	if predicate, ok := holderRosterAssignmentStates[q.AssignmentState]; ok {
		// NO ARGUMENT OF ITS OWN, and no interpolation of the caller's value:
		// the state selects one of four predicates written above, and what
		// reaches the SQL is that predicate. An unrecognised value matches no
		// key and leaves the dimension unfiltered — the whole roster, never an
		// empty one.
		//
		// PARENTHESISED because three of the four predicates are conjunctions
		// containing an OR, and this string is joined to its neighbours with
		// AND. Without the brackets `unassigned` would widen the roster instead
		// of narrowing it, silently and only in combination with another filter.
		conds = append(conds, "("+predicate+")")
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
// OLDEST SALE FIRST BY DEFAULT, and not newest as the Sales list is. When the
// filter is on this is a chase list — the buyer who paid in January and has said
// nothing since belongs at the top — and the roster keeps the same order so
// switching the filter reorders nobody. Since #527 the caller may ask for one of
// FIVE orders instead, in either direction; the default is unchanged, down to
// the `s.id, tk.ordinal` beneath it that keeps one sale's Tickets together. See
// holderOrderBy, holderSortColumns and holderSortBlanksLast, which is where the
// blanks-last rule and its deliberate break with the ascending/descending
// convention are argued.
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

	// THE COUNT CARRIES THE HOLDER JOIN TOO, since #526, and it must: the search
	// predicate matches an accepted Holder's NAME, which lives on the joined
	// Customer row and on no column of `tickets`. Two queries reading different
	// columns would be two views — the total describing one and the page the
	// other — which is worse than either being wrong, because nothing on the
	// screen says which to believe. The join is LEFT and on the primary key, so
	// it changes no count on its own: see holderRosterHolderJoin.
	var total int
	if err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*)
	`+holderRosterFrom+holderRosterHolderJoin+`
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
	roster, _ := holderRosterSelect(q)
	pageArgs := append(append([]any{}, args...), q.Limit, q.Offset)
	rows, err := r.db.Pool.QueryContext(ctx,
		roster+fmt.Sprintf("\n\t\tLIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	tickets := make([]HolderTicket, 0)
	for rows.Next() {
		t, err := scanHolderTicket(rows)
		if err != nil {
			return nil, 0, err
		}
		tickets = append(tickets, t)
	}
	return tickets, total, rows.Err()
}

// holderRosterSelect is the roster's whole read - its columns, its joins, its
// WHERE and its ORDER BY - with no page on it, and the arguments it binds.
//
// ONE STATEMENT FOR THE SCREEN AND THE FILE. The Holder List pages it and the
// Holder Export reads it through a cursor to the end (ADR 0075), and neither
// assembles a second copy: "the file mirrors the view" is a promise only one
// query can keep.
func holderRosterSelect(q ListHolderTicketsQuery) (string, []any) {
	where, args := holderRosterFilters(q)

	// THE ORDER, AND THE ONE JOIN THAT ONLY AN ORDER NEEDS (#527). holderOrderBy
	// resolves the sort against its allowlist — an unrecognised key is the
	// default order, never an error — and says whether the debt-count join has
	// to be added for it. That join goes on the ROWS ALONE: nothing in the WHERE
	// reads it, and it can change no count, being LEFT and on the Ticket's
	// primary key over a grouped derivation (see holderRosterOwesJoin). The
	// list's COUNT therefore still describes exactly the view a page shows.
	orderBy, needsOwesJoin := holderOrderBy(q.Sort, q.Dir)
	owesJoin := ""
	if needsOwesJoin {
		// The scope re-stated with the same two arguments the filters already
		// bound, in their known positions — its aliases shadow the roster's, so
		// an unscoped derivation would count another Organization's debts. It
		// binds nothing of its own, for holderRosterOwingOnly's reason.
		owesJoin = fmt.Sprintf(holderRosterOwesJoin, holderRosterScopeEventArg, holderRosterScopeOrgArg)
	}

	return `
		SELECT tk.id, tk.ordinal, l.ticket_type_id, tt.name,
		       s.id, s.confirmation_ref, s.channel,
		       s.customer_first_name, s.customer_last_name, s.customer_email, s.sold_at,
		       tk.holder_email, tk.assigned_at, tk.accepted_at, tk.holder_address_purged_at,
		       hc.first_name, hc.last_name,
		       s.customer_id, tk.holder_customer_id
	` + holderRosterFrom + holderRosterHolderJoin + owesJoin + `
		WHERE ` + where + `
		` + orderBy, args
}

// scanHolderTicket reads one row of holderRosterSelect.
func scanHolderTicket(rows *sql.Rows) (HolderTicket, error) {
	var t HolderTicket
	err := rows.Scan(
		&t.ID, &t.Ordinal, &t.TicketTypeID, &t.TicketTypeName,
		&t.TicketSaleID, &t.ConfirmationRef, &t.Channel,
		&t.CustomerFirstName, &t.CustomerLastName, &t.CustomerEmail, &t.SoldAt,
		&t.HolderEmail, &t.AssignedAt, &t.AcceptedAt, &t.HolderAddressPurgedAt,
		&t.HolderFirstName, &t.HolderLastName,
		&t.CustomerID, &t.HolderCustomerID,
	)
	return t, err
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
