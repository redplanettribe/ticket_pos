package repository

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// The reads the HOLDER EXPORT needs beyond the roster itself (#529, parent
// #518, ADR 0065): the Event's Ticket Questions, as the columns of the file.
//
// The rows are ListHolderTickets's — the very same query, the very same filters
// and the very same sort the Holder List screen ran — so nothing here re-decides
// which Tickets the file is about. What is missing from that read is the
// QUESTION side: the screen names only what each Ticket still OWES, while the
// file states what every Ticket has ANSWERED, and the Answers themselves are
// already read by ListTicketAnswers next door.

// EventTicketQuestion is one Ticket Question of an Event, with its Options —
// one or more columns of the Holder Export.
//
// IT IS A DELIBERATE RESTATEMENT of sales/repository.EventTicketQuestion and the
// query below is a restatement of its ListEventTicketQuestions. The two modules
// own separate repositories over one database and neither imports the other's;
// the alternative to twelve lines of SQL twice is the catalog service reaching
// into the sales repository, which would couple two domains to save a SELECT.
// What must not diverge is the DEFINITION of an askable question, and it does not:
// both statements are built from catalog.ApprovedQuestionSQL and
// catalog.ApprovedOptionSQL, which live once in the catalog root package and are
// the thing a review would actually be about.
type EventTicketQuestion struct {
	ID    string
	Label string
	// Kind decides whether the question takes one column or one per Option; the
	// service reads it, because the fan-out rule belongs to the file's builder and
	// not to a row.
	Kind    string
	Options []EventTicketQuestionOption
}

// EventTicketQuestionOption is one Option, by identity and current label.
type EventTicketQuestionOption struct {
	ID string
	// Label is the Option's CURRENT label — what the column heading reads. The
	// snapshot each Answer keeps of the words its chooser actually read is a
	// different fact, per Answer rather than per column, and is not something a
	// heading can honestly show.
	Label string
}

// ListEventTicketQuestions returns every Ticket Question defined on the Event's
// Ticket Types, in catalog display order, with each question's Options.
//
// APPROVED QUESTIONS AND APPROVED OPTIONS ONLY (catalog.ApprovedQuestionSQL, ADR
// 0056): a draft, under-review or refused question was asked of nobody and earns
// no column.
//
// RETIRED QUESTIONS AND RETIRED OPTIONS ARE INCLUDED. Retiring either takes it
// off the lists new buyers and staff are shown; it does not unsay what has
// already been answered, and this file reads every Answer of the roster at once.
// A retired question with Answers under it whose column vanished would be data
// silently lost from the file — which is the very promise retiring rather than
// deleting an Option exists to keep.
//
// The order is the Event's own: its Ticket Types in catalog display order, each
// type's questions in the order it asks them. That is the order the form reads
// in, and therefore the order an Organizer already has in their head.
func (r *Repository) ListEventTicketQuestions(ctx context.Context, orgID, eventID string) ([]EventTicketQuestion, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT q.id, q.label, q.kind
		FROM ticket_questions q
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		WHERE tt.event_id = $1 AND tt.organization_id = $2
		  AND `+catalog.ApprovedQuestionSQL+`
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
		  AND `+catalog.ApprovedOptionSQL+`
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
