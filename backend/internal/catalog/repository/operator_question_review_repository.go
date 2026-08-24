package repository

import (
	"context"
	"database/sql"
	"time"
)

// The Operator's half of the Question Review (#407, parent #404, ADR 0056):
// the cross-Organization queue, one Review whole, the answer, and the lapse.

// OperatorQuestionReviewRow is one Review with the Organization and Event it
// belongs to beside it, which is what a queue that crosses Organizations has
// to say on every row.
type OperatorQuestionReviewRow struct {
	QuestionReviewRow
	OrganizationID   string
	OrganizationName string
	OrganizationSlug string
	EventName        string
	EventStartsAt    sql.NullTime
	EventTimezone    string
	// QuestionCount is how many question items the Review carries; Options are
	// not counted, because "3 questions" is what an Operator budgets time for.
	QuestionCount int
}

// QuestionReviewLapsedBy is the answered_by an automatic lapse is signed with:
// nobody ended the Review, the Event's start did, and a reader of the column
// can tell that from an Operator's address on the same terms migration 090's
// approvals name the migration.
const QuestionReviewLapsedBy = "system:event_started"

const operatorQuestionReviewSelect = `
	SELECT r.id, r.event_id, r.status, r.note, r.acknowledged_at, r.submitted_by, r.submitted_at,
	       r.answered_by, r.answered_at,
	       o.id, o.name, o.slug, e.name, e.starts_at, e.timezone,
	       (SELECT COUNT(*) FROM question_review_items i
	         WHERE i.question_review_id = r.id AND i.ticket_question_option_id IS NULL)
	FROM question_reviews r
	JOIN events e ON e.id = r.event_id
	JOIN organizations o ON o.id = e.organization_id
`

func scanOperatorQuestionReview(row interface {
	Scan(dest ...any) error
}) (*OperatorQuestionReviewRow, error) {
	var r OperatorQuestionReviewRow
	if err := row.Scan(
		&r.ID, &r.EventID, &r.Status, &r.Note, &r.AcknowledgedAt, &r.SubmittedBy, &r.SubmittedAt,
		&r.AnsweredBy, &r.AnsweredAt,
		&r.OrganizationID, &r.OrganizationName, &r.OrganizationSlug, &r.EventName, &r.EventStartsAt, &r.EventTimezone,
		&r.QuestionCount,
	); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// LapseStartedQuestionReviews ends every outstanding Review whose Event has
// started and returns its items to draft, in one transaction.
//
// THE LAPSE IS DECIDED ON READ. There is no scheduler: every read of the
// queue, the count, one Review or the editor's current Review runs this
// first, so the moment an Event starts is the moment its Review reads lapsed
// — to whoever reads next. The Event's start is an instant, so "started in
// the Event's timezone" is a comparison of instants (see SubmitQuestionReview
// in the service). Idempotent and cheap when nothing has started: the UPDATE
// touches no row.
func (r *Repository) LapseStartedQuestionReviews(ctx context.Context, now time.Time) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `
		UPDATE question_reviews r
		SET status = 'lapsed', answered_by = $2, answered_at = $1
		FROM events e
		WHERE e.id = r.event_id
		  AND r.status = 'outstanding'
		  AND e.starts_at IS NOT NULL
		  AND e.starts_at <= $1
		RETURNING r.id
	`, now, QuestionReviewLapsedBy)
	if err != nil {
		return err
	}
	var lapsed []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		lapsed = append(lapsed, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(lapsed) == 0 {
		return nil
	}

	// Back to draft, on the withdrawal's terms: only the rows still under a
	// lapsed Review's hold.
	for _, reviewID := range lapsed {
		if err := returnQuestionReviewItemsToDraft(ctx, tx, reviewID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// returnQuestionReviewItemsToDraft is the one statement pair a withdrawal
// and a lapse share.
func returnQuestionReviewItemsToDraft(ctx context.Context, tx *sql.Tx, reviewID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_questions q
		SET review_status = 'draft', updated_at = $2
		FROM question_review_items i
		WHERE i.question_review_id = $1
		  AND i.ticket_question_option_id IS NULL
		  AND i.ticket_question_id = q.id
		  AND q.review_status = 'under_review'
	`, reviewID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE ticket_question_options o
		SET review_status = 'draft', updated_at = $2
		FROM question_review_items i
		WHERE i.question_review_id = $1
		  AND i.ticket_question_option_id = o.id
		  AND o.review_status = 'under_review'
	`, reviewID, now)
	return err
}

// ListOutstandingQuestionReviews returns one page of every outstanding Review
// on the platform, OLDEST FIRST, plus the unpaginated total — the Payout
// Request queue's shape (ADR 0026): a work queue rather than a history, and
// the Review that has waited longest is the one whose Event is nearest.
func (r *Repository) ListOutstandingQuestionReviews(ctx context.Context, page, pageSize int) ([]OperatorQuestionReviewRow, int, error) {
	total, err := r.CountOutstandingQuestionReviews(ctx)
	if err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Pool.QueryContext(ctx, operatorQuestionReviewSelect+`
		WHERE r.status = 'outstanding'
		ORDER BY r.submitted_at ASC, r.id
		LIMIT $1 OFFSET $2
	`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]OperatorQuestionReviewRow, 0)
	for rows.Next() {
		row, err := scanOperatorQuestionReview(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *row)
	}
	return out, total, rows.Err()
}

// CountOutstandingQuestionReviews is the queue as one number.
func (r *Repository) CountOutstandingQuestionReviews(ctx context.Context) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `SELECT COUNT(*) FROM question_reviews WHERE status = 'outstanding'`).Scan(&count)
	return count, err
}

// GetQuestionReviewForOperator loads one Review in any state, unscoped by
// Organization: the operator allowlist is the whole of the gate.
func (r *Repository) GetQuestionReviewForOperator(ctx context.Context, reviewID string) (*OperatorQuestionReviewRow, error) {
	return scanOperatorQuestionReview(r.db.Pool.QueryRowContext(ctx, operatorQuestionReviewSelect+`
		WHERE r.id = $1
	`, reviewID))
}

// QuestionReviewVerdict is one ruling, already validated by the service: the
// verdict is `approved` or `refused`, and a refusal carries its reason.
type QuestionReviewVerdict struct {
	ItemID  string
	Verdict string
	Reason  sql.NullString
}

// AnswerQuestionReview records the Operator's verdicts, in ONE transaction:
// the Review is moved to `answered` by compare-and-swap on `outstanding`, each
// item takes its verdict and reason, and each question and Option it names
// takes the matching review_status with the Operator's authorship. A Review
// that is no longer outstanding is left alone and nil comes back, so the
// caller can name the state it actually reached.
//
// An approval clears an earlier refusal's columns, so a question refused
// once and approved on resubmission reads as approved and nothing else.
//
// Only rows still `under_review` are moved: a question retired by the
// Organization meanwhile still takes its verdict (retirement is the
// Organization's business, review state the Operator's), but a row some
// other act already moved is that act's.
func (r *Repository) AnswerQuestionReview(ctx context.Context, reviewID string, verdicts []QuestionReviewVerdict, answeredBy string, now time.Time) (*QuestionReviewRow, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	review, err := scanQuestionReview(tx.QueryRowContext(ctx, `
		UPDATE question_reviews
		SET status = 'answered', answered_by = $2, answered_at = $3
		WHERE id = $1 AND status = 'outstanding'
		RETURNING `+questionReviewColumns,
		reviewID, answeredBy, now,
	))
	if err != nil {
		return nil, err
	}
	if review == nil {
		return nil, nil
	}

	for _, v := range verdicts {
		if _, err := tx.ExecContext(ctx, `
			UPDATE question_review_items
			SET verdict = $3, reason = $4
			WHERE id = $2 AND question_review_id = $1
		`, reviewID, v.ItemID, v.Verdict, v.Reason); err != nil {
			return nil, err
		}
		if v.Verdict == "approved" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE ticket_questions q
				SET review_status = 'approved', approved_at = $3, approved_by = $2, updated_at = $3,
				    refused_at = NULL, refused_by = NULL, refusal_reason = NULL
				FROM question_review_items i
				WHERE i.id = $1 AND i.ticket_question_option_id IS NULL
				  AND q.id = i.ticket_question_id AND q.review_status = 'under_review'
			`, v.ItemID, answeredBy, now); err != nil {
				return nil, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE ticket_question_options o
				SET review_status = 'approved', approved_at = $3, approved_by = $2, updated_at = $3,
				    refused_at = NULL, refused_by = NULL, refusal_reason = NULL
				FROM question_review_items i
				WHERE i.id = $1 AND o.id = i.ticket_question_option_id AND o.review_status = 'under_review'
			`, v.ItemID, answeredBy, now); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_questions q
			SET review_status = 'refused', refused_at = $3, refused_by = $2, refusal_reason = $4, updated_at = $3
			FROM question_review_items i
			WHERE i.id = $1 AND i.ticket_question_option_id IS NULL
			  AND q.id = i.ticket_question_id AND q.review_status = 'under_review'
		`, v.ItemID, answeredBy, now, v.Reason); err != nil {
			return nil, err
		}
		// An Option refused on its own — one added to an already approved
		// question, so the Review carries no item for the question — is
		// retired on the spot (#409): its question keeps collecting in its
		// approved shape, and there is nothing to edit and resubmit. An
		// Option refused beside its question stays with the question, whose
		// first edit takes them all back to draft.
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_question_options o
			SET review_status = 'refused', refused_at = $3, refused_by = $2, refusal_reason = $4, updated_at = $3,
			    retired_at = CASE
			        WHEN o.retired_at IS NOT NULL THEN o.retired_at
			        WHEN EXISTS (
			            SELECT 1 FROM question_review_items p
			            WHERE p.question_review_id = i.question_review_id
			              AND p.ticket_question_id = i.ticket_question_id
			              AND p.ticket_question_option_id IS NULL
			        ) THEN NULL
			        ELSE $3
			    END
			FROM question_review_items i
			WHERE i.id = $1 AND o.id = i.ticket_question_option_id AND o.review_status = 'under_review'
		`, v.ItemID, answeredBy, now, v.Reason); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return review, nil
}
