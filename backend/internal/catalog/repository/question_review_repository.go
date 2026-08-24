package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// The Question Review's rows (#406, parent #404, ADR 0056): the Organization's
// one ask per Event, and the questions and Options it carries.

// QuestionReviewRow is one Question Review as stored.
type QuestionReviewRow struct {
	ID      string
	EventID string
	// Status is outstanding, answered, withdrawn or lapsed (migration 091).
	Status         string
	Note           sql.NullString
	AcknowledgedAt time.Time
	// SubmittedBy is the submitter's email, so the record outlives their
	// Membership; AnsweredBy is whoever ended it, whichever way it ended.
	SubmittedBy string
	SubmittedAt time.Time
	AnsweredBy  sql.NullString
	AnsweredAt  sql.NullTime
}

// QuestionReviewItemRow is one thing a Review carries: a question, or an
// Option of one (OptionID set). Verdict and Reason are empty until the
// Operator answers (#407).
type QuestionReviewItemRow struct {
	ID         string
	ReviewID   string
	QuestionID string
	OptionID   sql.NullString
	Verdict    sql.NullString
	Reason     sql.NullString
}

const questionReviewColumns = `
	id, event_id, status, note, acknowledged_at, submitted_by, submitted_at, answered_by, answered_at
`

func scanQuestionReview(row interface {
	Scan(dest ...any) error
}) (*QuestionReviewRow, error) {
	var r QuestionReviewRow
	if err := row.Scan(
		&r.ID, &r.EventID, &r.Status, &r.Note, &r.AcknowledgedAt,
		&r.SubmittedBy, &r.SubmittedAt, &r.AnsweredBy, &r.AnsweredAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// ErrQuestionReviewOutstanding is the database refusing a second outstanding
// Review for the Event: the partial unique index (migration 091) stopped the
// INSERT. The service turns it into the domain refusal.
var ErrQuestionReviewOutstanding = errors.New("a question review is already outstanding for this event")

// ErrNothingToReview is the Event carrying no draft or refused question and no
// draft or refused Option: a Review with no items is not an ask.
var ErrNothingToReview = errors.New("nothing to review")

// SubmitQuestionReviewParams is what a submission records.
type SubmitQuestionReviewParams struct {
	EventID     string
	Note        sql.NullString
	SubmittedBy string
	Now         time.Time
}

// SubmitQuestionReview records one Review over the Event's draft and refused
// questions and Options and moves them all to `under_review`, in one
// transaction, so an Operator can never read a Review whose items disagree
// with the rows they name.
//
// WHAT IS CARRIED: every live question of the Event in `draft` or `refused`,
// and every live Option in `draft` or `refused` whose question is live —
// including the Options of a carried draft question, because the Operator
// rules per question AND per Option (migration 089). An approved question is
// not touched: it keeps collecting while the Review is outstanding (ADR 0056).
//
// THE ONE-OUTSTANDING RULE IS THE INDEX'S. The INSERT's ON CONFLICT DO NOTHING
// returns no row when an outstanding Review exists, and the transaction rolls
// back having moved nothing.
func (r *Repository) SubmitQuestionReview(ctx context.Context, params SubmitQuestionReviewParams) (*QuestionReviewRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	review, err := scanQuestionReview(tx.QueryRowContext(ctx, `
		INSERT INTO question_reviews (event_id, status, note, acknowledged_at, submitted_by, submitted_at)
		VALUES ($1, 'outstanding', $2, $3, $4, $3)
		ON CONFLICT (event_id) WHERE status = 'outstanding' DO NOTHING
		RETURNING `+questionReviewColumns,
		params.EventID, params.Note, params.Now, params.SubmittedBy,
	))
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, ErrQuestionReviewOutstanding
	}

	// The questions, in the order the editor lists them, then their Options.
	// The item rows are written from the UPDATE's own RETURNING so what is
	// carried is exactly what was moved.
	questionRows, err := tx.QueryContext(ctx, `
		WITH moved AS (
			UPDATE ticket_questions q
			SET review_status = 'under_review', updated_at = $2
			FROM ticket_types tt
			WHERE q.ticket_type_id = tt.id
			  AND tt.event_id = $1
			  AND q.retired_at IS NULL
			  AND q.review_status IN ('draft', 'refused')
			RETURNING q.id, tt.sort_order AS type_order, q.sort_order
		)
		INSERT INTO question_review_items (question_review_id, ticket_question_id)
		SELECT $3, id FROM moved ORDER BY type_order, sort_order
		RETURNING ticket_question_id
	`, params.EventID, params.Now, review.ID)
	if err != nil {
		return nil, err
	}
	questionCount := 0
	for questionRows.Next() {
		var id string
		if err := questionRows.Scan(&id); err != nil {
			questionRows.Close()
			return nil, err
		}
		questionCount++
	}
	questionRows.Close()
	if err := questionRows.Err(); err != nil {
		return nil, err
	}

	optionResult, err := tx.ExecContext(ctx, `
		WITH moved AS (
			UPDATE ticket_question_options o
			SET review_status = 'under_review', updated_at = $2
			FROM ticket_questions q, ticket_types tt
			WHERE o.ticket_question_id = q.id
			  AND q.ticket_type_id = tt.id
			  AND tt.event_id = $1
			  AND q.retired_at IS NULL
			  AND o.retired_at IS NULL
			  AND o.review_status IN ('draft', 'refused')
			RETURNING o.id, o.ticket_question_id, tt.sort_order AS type_order, q.sort_order AS question_order, o.sort_order
		)
		INSERT INTO question_review_items (question_review_id, ticket_question_id, ticket_question_option_id)
		SELECT $3, ticket_question_id, id FROM moved ORDER BY type_order, question_order, sort_order
	`, params.EventID, params.Now, review.ID)
	if err != nil {
		return nil, err
	}
	optionCount, err := optionResult.RowsAffected()
	if err != nil {
		return nil, err
	}
	if questionCount == 0 && optionCount == 0 {
		return nil, ErrNothingToReview
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return review, nil
}

// WithdrawQuestionReview moves an OUTSTANDING Review to `withdrawn` and its
// items back to `draft`, as one compare-and-swap on the state: a Review the
// Operator answered in the meantime is left alone and nil comes back, so the
// caller can name the state it actually reached.
func (r *Repository) WithdrawQuestionReview(ctx context.Context, eventID, reviewID, withdrawnBy string, now time.Time) (*QuestionReviewRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	review, err := scanQuestionReview(tx.QueryRowContext(ctx, `
		UPDATE question_reviews
		SET status = 'withdrawn', answered_by = $3, answered_at = $4
		WHERE id = $2 AND event_id = $1 AND status = 'outstanding'
		RETURNING `+questionReviewColumns,
		eventID, reviewID, withdrawnBy, now,
	))
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, nil
	}

	// Back to draft, and ONLY the rows still under this Review's hold: a row
	// some later verdict reached is that verdict's business.
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_questions q
		SET review_status = 'draft', updated_at = $2
		FROM question_review_items i
		WHERE i.question_review_id = $1
		  AND i.ticket_question_option_id IS NULL
		  AND i.ticket_question_id = q.id
		  AND q.review_status = 'under_review'
	`, reviewID, now); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_question_options o
		SET review_status = 'draft', updated_at = $2
		FROM question_review_items i
		WHERE i.question_review_id = $1
		  AND i.ticket_question_option_id = o.id
		  AND o.review_status = 'under_review'
	`, reviewID, now); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return review, nil
}

// GetQuestionReview loads one Review scoped to its Event.
func (r *Repository) GetQuestionReview(ctx context.Context, eventID, reviewID string) (*QuestionReviewRow, error) {
	return scanQuestionReview(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+questionReviewColumns+`
		FROM question_reviews
		WHERE id = $2 AND event_id = $1
	`, eventID, reviewID))
}

// GetCurrentQuestionReview is the Event's outstanding Review, or failing that
// the last one submitted; nil when the Event has never had one.
func (r *Repository) GetCurrentQuestionReview(ctx context.Context, eventID string) (*QuestionReviewRow, error) {
	return scanQuestionReview(r.db.Pool.QueryRowContext(ctx, `
		SELECT `+questionReviewColumns+`
		FROM question_reviews
		WHERE event_id = $1
		ORDER BY (status = 'outstanding') DESC, submitted_at DESC, id
		LIMIT 1
	`, eventID))
}

// ListQuestionReviewItems returns a Review's items in the order the editor
// lists them: each question's item, followed by its Options'.
func (r *Repository) ListQuestionReviewItems(ctx context.Context, reviewID string) ([]QuestionReviewItemRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT i.id, i.question_review_id, i.ticket_question_id, i.ticket_question_option_id, i.verdict, i.reason
		FROM question_review_items i
		JOIN ticket_questions q ON q.id = i.ticket_question_id
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		LEFT JOIN ticket_question_options o ON o.id = i.ticket_question_option_id
		WHERE i.question_review_id = $1
		ORDER BY tt.sort_order, q.sort_order, (o.id IS NOT NULL), o.sort_order
	`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []QuestionReviewItemRow
	for rows.Next() {
		var item QuestionReviewItemRow
		if err := rows.Scan(&item.ID, &item.ReviewID, &item.QuestionID, &item.OptionID, &item.Verdict, &item.Reason); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetOrganizationName is the one Organization fact the submission notice
// carries and the Event row does not.
func (r *Repository) GetOrganizationName(ctx context.Context, orgID string) (string, error) {
	var name string
	err := r.db.Pool.QueryRowContext(ctx, `SELECT name FROM organizations WHERE id = $1`, orgID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}
