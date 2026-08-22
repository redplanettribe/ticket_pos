package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The checkout half of the Answer's life: the questions a cart is asked, the
// Answers held on the Payment across the Payment Provider redirect, and the copy
// onto the minted Tickets when the sale commits (#311, ADR 0044).
//
// WHY THE SALES REPOSITORY READS `ticket_questions` DIRECTLY. It is the same
// arrangement this file's neighbours already have with `ticket_types`, which the
// sales domain owns nothing of and reads on every checkout: the commit spine
// locks those rows FOR UPDATE, prices from them and decrements their sold_count.
// A seam back into the catalog service for the questions would be a second way
// into a read that has to happen inside this module's own transaction anyway,
// and — for the copy below — inside the very transaction that mints the Tickets.
// What the sales module deliberately does NOT own is the RULES: which answers
// are keepable is catalog.HoldableCheckoutAnswers' finding, and this file only
// moves rows.

// ListCheckoutQuestions returns the Ticket Questions a cart's Ticket Types put
// to a buyer AT CHECKOUT, each with the Options it currently offers.
//
// The query and the fold are catalog.CheckoutQuestionsSQL and
// catalog.ScanAskedQuestions, SHARED WITH THE STOREFRONT'S EVENT PAGE, which
// draws the form these answers come back from. Two copies of those filters would
// eventually differ, and the way that failure shows up is a question the buyer
// was shown whose answer is then silently dropped — or one they were never shown
// that this capture happily accepts.
//
// One read for every Ticket Type in the cart rather than one per type: a cart is
// at most a handful of Ticket Types.
func (r *Repository) ListCheckoutQuestions(ctx context.Context, ticketTypeIDs []string) ([]catalog.AskedQuestion, error) {
	if len(ticketTypeIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, catalog.CheckoutQuestionsSQL, ticketTypeIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return catalog.ScanAskedQuestions(rows)
}

// HoldCheckoutAnswers writes the Answers a buyer gave at checkout onto their
// Payment, keyed by (payment line, index).
//
// It resolves the Payment Line for each answer's Ticket Type ITSELF, from the
// Payment this call names, rather than taking line ids from a caller. That keeps
// the (payment_line, index) key an internal fact: the browser names a Ticket
// Type — which is what it was shown — and the line that Ticket Type became is
// the server's own bookkeeping, aggregated across a cart that may have named the
// same type twice (see BeginCheckout).
//
// AN ANSWER FOR A TICKET TYPE WITH NO PAYMENT LINE IS SKIPPED, not refused. It
// cannot normally happen — the rule that produced these answers already checked
// the cart — but this is the one place on the path where a refusal would have
// somewhere to go, and it must not.
//
// IT RUNS IN ITS OWN TRANSACTION, DELIBERATELY, AND NOT IN CreatePayment'S.
// Writing these beside the payment lines would make a failure here a failure of
// the whole checkout, which is precisely what ADR 0044 forbids. Separated, the
// worst case is a Payment with no held Answers — the buyer keeps their tickets
// and answers later by Answer Link, which is the fallback the whole feature is
// built around. All-or-nothing WITHIN the answers, though: a half-written set is
// a form that silently lost three of its four fields.
func (r *Repository) HoldCheckoutAnswers(ctx context.Context, paymentID string, held []catalog.HeldAnswer, now time.Time) error {
	if len(held) == 0 {
		return nil
	}

	lineIDs, err := r.paymentLineIDsByTicketType(ctx, paymentID)
	if err != nil {
		return err
	}

	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, answer := range held {
		lineID, ok := lineIDs[answer.TicketTypeID]
		if !ok {
			continue
		}
		params := heldAnswerColumns(answer.Value)
		var answerID string
		// The casts sit on the placeholders, exactly as they do in
		// UpsertTicketAnswer: `$5` arrives as a string carrying digits, and
		// telling Postgres it is a NUMERIC is what keeps the value typed all the
		// way in. A driver-side float would round it away.
		//
		// ON CONFLICT DO UPDATE rather than DO NOTHING: a buyer who goes back and
		// re-submits the checkout form for the same Payment means the second
		// answer, and a conflict here must never surface as an error.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO payment_ticket_answers (
				payment_line_id, ticket_index, ticket_question_id,
				text_value, number_value, date_value, boolean_value, created_at
			)
			VALUES ($1, $2, $3, $4, $5::numeric, $6::date, $7, $8)
			ON CONFLICT (payment_line_id, ticket_index, ticket_question_id) DO UPDATE
			SET text_value = EXCLUDED.text_value,
			    number_value = EXCLUDED.number_value,
			    date_value = EXCLUDED.date_value,
			    boolean_value = EXCLUDED.boolean_value
			RETURNING id
		`, lineID, answer.TicketIndex, answer.TicketQuestionID,
			params.Text, params.Number, params.Date, params.Checked, now,
		).Scan(&answerID); err != nil {
			return err
		}

		// Replaced wholesale, for UpsertTicketAnswer's reason: a choice Answer IS
		// its set of Options, so a re-submission is "this is the set now" and a
		// diff would only add a way to end up somewhere in between.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM payment_ticket_answer_options WHERE payment_ticket_answer_id = $1
		`, answerID); err != nil {
			return err
		}
		for i, option := range answer.Options {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO payment_ticket_answer_options (
					payment_ticket_answer_id, ticket_question_option_id,
					option_label_snapshot, sort_order, created_at
				)
				VALUES ($1, $2, $3, $4, $5)
			`, answerID, option.TicketQuestionOptionID, option.LabelSnapshot, i, now); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// paymentLineIDsByTicketType maps a Payment's Ticket Types to its line ids.
//
// One line per Ticket Type is an invariant of CreatePayment, not of the schema:
// BeginCheckout aggregates a cart's lines per Ticket Type before writing them,
// so a cart naming the same type twice is one line. If that ever stopped being
// true this map would silently keep the last line, which is why the aggregation
// is asserted where it happens rather than assumed here.
func (r *Repository) paymentLineIDsByTicketType(ctx context.Context, paymentID string) (map[string]string, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT id, ticket_type_id FROM payment_lines WHERE payment_id = $1
	`, paymentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byType := map[string]string{}
	for rows.Next() {
		var id, ticketTypeID string
		if err := rows.Scan(&id, &ticketTypeID); err != nil {
			return nil, err
		}
		byType[ticketTypeID] = id
	}
	return byType, rows.Err()
}

// LineAnswer is one held Answer on its way from a Payment onto a Ticket: which
// unit of the line it is about, which question it replies to, and the reply in
// the columns it is already stored in.
//
// IT CARRIES NO KIND AND DOES NO PARSING, and that is the point of mirroring the
// typed columns on both tables (migration 074). By the time an Answer is here it
// has been through catalog.ParseAnswer once, at checkout, and the write below is
// a COPY. A second parse at commit would be a second chance to disagree with the
// first — after the Payment Provider has taken the money, where a disagreement
// has nowhere to go.
type LineAnswer struct {
	// TicketIndex is which of the line's units this Answer is about, 1..quantity.
	// It becomes `tickets.ordinal` unchanged; see writeTicketAnswers.
	TicketIndex      int
	TicketQuestionID string
	Text             sql.NullString
	// Number and Date are strings out of NUMERIC and DATE columns, so the digits
	// travel exactly as Postgres holds them — `3.50` keeps its trailing zero, and
	// a calendar date never acquires a midnight or a zone.
	Number  sql.NullString
	Date    sql.NullString
	Checked sql.NullBool
	Options []LineAnswerOption
}

// LineAnswerOption is one chosen Option travelling with its Answer: the Option's
// identity, and the SNAPSHOT taken when the buyer read it at checkout.
//
// The snapshot is carried rather than retaken. Between checkout and commit sits
// a Payment Provider redirect, which is the one gap in this product long enough
// for an Organization to rename an Option; re-reading the label here would put
// words on the Ticket that nobody was ever shown (migration 074).
type LineAnswerOption struct {
	TicketQuestionOptionID string
	LabelSnapshot          string
}

// ListHeldAnswersByPaymentLine returns the Answers a Payment is holding, grouped
// by the Payment Line they were given against.
//
// Read inside the commit transaction, so the copy onto the Tickets and the
// minting of those Tickets cannot come apart.
func listHeldAnswersByPaymentLine(ctx context.Context, tx *sql.Tx, paymentID string) (map[string][]LineAnswer, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT a.payment_line_id, a.id, a.ticket_index, a.ticket_question_id,
		       a.text_value, a.number_value::text, a.date_value::text, a.boolean_value
		FROM payment_ticket_answers a
		JOIN payment_lines l ON l.id = a.payment_line_id
		WHERE l.payment_id = $1
		ORDER BY a.payment_line_id, a.ticket_index, a.created_at
	`, paymentID)
	if err != nil {
		return nil, err
	}

	byLine := map[string][]LineAnswer{}
	// Where each Answer ended up, so its Options can be attached in the second
	// pass without a second lookup per row.
	type position struct {
		lineID string
		index  int
	}
	positions := map[string]position{}
	for rows.Next() {
		var lineID, answerID string
		var answer LineAnswer
		if err := rows.Scan(&lineID, &answerID, &answer.TicketIndex, &answer.TicketQuestionID,
			&answer.Text, &answer.Number, &answer.Date, &answer.Checked); err != nil {
			rows.Close()
			return nil, err
		}
		byLine[lineID] = append(byLine[lineID], answer)
		positions[answerID] = position{lineID: lineID, index: len(byLine[lineID]) - 1}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if len(positions) == 0 {
		return byLine, nil
	}

	// TWO QUERIES FOR ANY NUMBER OF ANSWERS, not two per Answer — the shape
	// ListTicketAnswers uses for the same reason. A cart of forty tickets with
	// six choice questions each is one read of the Answers and one of their
	// Options.
	optionRows, err := tx.QueryContext(ctx, `
		SELECT o.payment_ticket_answer_id, o.ticket_question_option_id, o.option_label_snapshot
		FROM payment_ticket_answer_options o
		JOIN payment_ticket_answers a ON a.id = o.payment_ticket_answer_id
		JOIN payment_lines l ON l.id = a.payment_line_id
		WHERE l.payment_id = $1
		ORDER BY o.payment_ticket_answer_id, o.sort_order, o.created_at
	`, paymentID)
	if err != nil {
		return nil, err
	}
	defer optionRows.Close()
	for optionRows.Next() {
		var answerID string
		var option LineAnswerOption
		if err := optionRows.Scan(&answerID, &option.TicketQuestionOptionID, &option.LabelSnapshot); err != nil {
			return nil, err
		}
		at, ok := positions[answerID]
		if !ok {
			continue
		}
		byLine[at.lineID][at.index].Options = append(byLine[at.lineID][at.index].Options, option)
	}
	return byLine, optionRows.Err()
}

// writeTicketAnswers copies a line's held Answers onto the Tickets that line
// just minted, in the caller's transaction.
//
// `ticketIDs` maps ordinal to Ticket id, straight out of mintTickets. THE WHOLE
// OF "IN ORDER" IS THIS LOOKUP: index n on the Payment becomes ordinal n on the
// Ticket, and neither number is ever recomputed from a row position. That is
// what `tickets.ordinal` was given a column for (migration 070) — without it the
// order would be whatever the INSERT happened to return.
//
// AN ANSWER WHOSE INDEX NAMES NO TICKET IS SKIPPED. The capture already bounded
// the index by the line's quantity, so this can only be reached by a Payment
// whose line was somehow written with a smaller quantity than the answers it
// holds. Skipping is the only safe way to be wrong: failing here would fail the
// commit of a sale the Payment Provider has already been paid for, which is the
// PAYMENT_APPROVED_WITHOUT_SALE incident, manufactured over an answer.
//
// It runs in the SAME transaction as the minting, and nothing between the two
// may fail without both rolling back: a Payment that fails or expires produces
// no Tickets, and therefore no Answers on any Ticket.
func writeTicketAnswers(ctx context.Context, tx *sql.Tx, ticketIDs map[int]string, answers []LineAnswer, now time.Time) error {
	for _, answer := range answers {
		ticketID, ok := ticketIDs[answer.TicketIndex]
		if !ok {
			continue
		}
		var answerID string
		// A plain INSERT and not an upsert: these Tickets were minted moments
		// ago in this same transaction and can have no Answers yet. An upsert
		// here would quietly paper over a double-mint, which the UNIQUE on
		// `tickets` exists to make loud.
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO ticket_answers (
				ticket_id, ticket_question_id,
				text_value, number_value, date_value, boolean_value,
				created_at, updated_at
			)
			VALUES ($1, $2, $3, $4::numeric, $5::date, $6, $7, $7)
			RETURNING id
		`, ticketID, answer.TicketQuestionID,
			answer.Text, answer.Number, answer.Date, answer.Checked, now,
		).Scan(&answerID); err != nil {
			return err
		}
		for i, option := range answer.Options {
			// The snapshot travels verbatim from the Payment. The Option's
			// CURRENT label is what every list and the export header show, and
			// it comes from the join; this is what the buyer read.
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ticket_answer_options (
					ticket_answer_id, ticket_question_option_id,
					option_label_snapshot, sort_order, created_at
				)
				VALUES ($1, $2, $3, $4, $5)
			`, answerID, option.TicketQuestionOptionID, option.LabelSnapshot, i, now); err != nil {
				return err
			}
		}
	}
	return nil
}

// heldAnswerColumns turns one validated AnswerValue into the four nullable
// columns it is stored in, setting exactly the one its kind takes.
//
// It is the checkout twin of the catalog service's upsertParams, and the two
// agree on the one thing that matters: a checkbox Answer is written Valid even
// when it is FALSE, because false is an answer. A NULL there would be
// indistinguishable from a question nobody replied to, which is an Outstanding
// Answer and a different fact.
func heldAnswerColumns(value catalog.AnswerValue) struct {
	Text    sql.NullString
	Number  sql.NullString
	Date    sql.NullString
	Checked sql.NullBool
} {
	var params struct {
		Text    sql.NullString
		Number  sql.NullString
		Date    sql.NullString
		Checked sql.NullBool
	}
	switch value.Kind {
	case catalog.TicketQuestionKindShortText, catalog.TicketQuestionKindLongText:
		params.Text = sql.NullString{String: value.Text, Valid: true}
	case catalog.TicketQuestionKindNumber:
		params.Number = sql.NullString{String: value.Number, Valid: true}
	case catalog.TicketQuestionKindDate:
		params.Date = sql.NullString{String: value.Date, Valid: true}
	case catalog.TicketQuestionKindCheckbox:
		params.Checked = sql.NullBool{Bool: value.Checked, Valid: true}
	}
	return params
}

// PurgeAbandonedCheckoutAnswers deletes the Answers held on Payments that never
// reached 'approved' and were begun at or before `cutoff` (#316, ADR 0044). It
// reports how many Answers went and how many Payments they came off.
//
// THE PREDICATE IS `status IS DISTINCT FROM 'approved'` AND NOTHING ELSE, and
// the reason it is not `status = 'expired'` is the whole ticket. Two facts about
// this codebase make the status useless as a "this is over" signal, and each one
// breaks a different half of the naive query:
//
//   - 'expired' IS NOT TERMINAL. It is opportunistic bookkeeping written by
//     whichever begin-checkout happens to pass the same Event
//     (ExpireStalePayments), and ApprovePaymentAndCommitSale accepts an expired
//     Payment exactly like a pending one — a provider that confirms late still
//     commits the sale, flipping expired → approved. Purging on 'expired' would
//     delete the Answers of a sale that then commits, silently: the copy at
//     commit finds nothing, the buyer keeps their Tickets, and the Organization
//     is simply told nobody answered.
//
//   - 'pending' IS OFTEN FOREVER. Nothing sweeps Payments on its own schedule,
//     so an Event with no further traffic keeps its abandoned Payments 'pending'
//     for good. A purge that waited for 'expired' would never touch them, and
//     the quietest Events — the ones with one abandoned checkout and nobody
//     looking — would be exactly the ones that kept the health data.
//
// So the status is read only to EXCLUDE the approved, and it is AGE that decides
// the rest. `IS DISTINCT FROM` rather than `<>` so a status that ever became
// nullable would purge rather than silently exempt: this is a deletion, and the
// safe direction for it to be wrong is loudly, not by quietly retaining.
//
// AGE IS THE PAYMENT'S, NOT THE ANSWER'S. `payment_ticket_answers.created_at`
// moves when a buyer goes back and re-submits the checkout form (HoldCheckoutAnswers
// upserts), so keying on it would restart the clock for a buyer who edited their
// answer and then abandoned — the retention window would be about typing rather
// than about the attempt to buy. The rule is "30 days after the Payment".
//
// The chosen Options go with their Answers by the ON DELETE CASCADE on
// `payment_ticket_answer_options` (migration 074). That cascade is why this is
// one statement and not two, and it is worth knowing that deleting the Answers
// without it would leave the option labels — the words somebody picked — behind.
//
// IT DELETES ONLY FROM `payment_ticket_answers`. The Payment and its
// `payment_lines` are untouched and are kept forever: the platform is entitled
// to remember an attempt to transact, and what it may not keep is the reply to a
// question about a Ticket that will never exist.
//
// IDEMPOTENT BY CONSTRUCTION, because it is a DELETE of rows matched by a
// predicate rather than a state machine: a second run finds the rows gone and
// reports zeros, and two runs racing each other delete disjoint sets. It takes
// no lock and claims nothing — there is no queue here and no per-row bookkeeping
// to leave behind, which is what distinguishes it from the Reversal Reconciler's
// drain.
func (r *Repository) PurgeAbandonedCheckoutAnswers(ctx context.Context, cutoff time.Time) (answers, payments int64, err error) {
	// One statement, in two CTEs. `doomed` names the rows and the Payments they
	// belong to BEFORE the delete, which is the only moment the Payment can still
	// be counted — a DELETE ... RETURNING gives back the answer rows, and by then
	// there is nothing left to join to the line that says whose they were.
	err = r.db.Pool.QueryRowContext(ctx, `
		WITH doomed AS (
			SELECT a.id, l.payment_id
			FROM payment_ticket_answers a
			JOIN payment_lines l ON l.id = a.payment_line_id
			JOIN payments p ON p.id = l.payment_id
			WHERE p.status IS DISTINCT FROM 'approved'
			  AND p.created_at <= $1
		),
		purged AS (
			DELETE FROM payment_ticket_answers
			WHERE id IN (SELECT id FROM doomed)
			RETURNING id
		)
		SELECT (SELECT COUNT(*) FROM purged), (SELECT COUNT(DISTINCT payment_id) FROM doomed)
	`, cutoff).Scan(&answers, &payments)
	if err != nil {
		return 0, 0, err
	}
	return answers, payments, nil
}

// CountHeldCheckoutAnswers is how many Answers are riding Payments right now,
// across the platform.
//
// Reported by every purge run for the reason the Reversal Reconciler reports its
// in-flight backlog: a run that says "0 purged" is indistinguishable from a run
// against a table that was never written to, and an operator watching a feature
// that ships dark (ADR 0045) needs to tell "nothing was due" from "nothing is
// there". Two curls a day apart say whether the checkout is capturing at all.
func (r *Repository) CountHeldCheckoutAnswers(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM payment_ticket_answers`).Scan(&n)
	return n, err
}
