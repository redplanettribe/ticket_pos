package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// TicketQuestion is one thing an Organization wants to know about whoever will
// hold a ticket of one Ticket Type.
type TicketQuestion struct {
	ID           string
	TicketTypeID string
	// Label is the Organization's own words, stored and read AS COINED in every
	// Locale (ADR 0027). There is no per-Locale sibling column and there is not
	// going to be one.
	Label string
	Kind  string
	// Required has no enforcement behind it anywhere: an unanswered required
	// question is an Outstanding Answer, never a refusal.
	Required  bool
	Timing    string
	SortOrder int
	// RetiredAt is invalid while the question is live. Retired, never deleted.
	RetiredAt sql.NullTime
	// ReviewStatus is where the question stands with the Platform Operator
	// (ADR 0056): draft, under_review, approved or refused. Only `approved`
	// is asked of anybody, and approval is never a column default — it is
	// written by a review, or by the grandfathering migration that names
	// itself in ApprovedBy.
	ReviewStatus     string
	ApprovedAt       sql.NullTime
	ApprovedBy       sql.NullString
	RefusedAt        sql.NullTime
	RefusedBy        sql.NullString
	RefusalReason    sql.NullString
	RevokedAt        sql.NullTime
	RevokedBy        sql.NullString
	RevocationReason sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// TicketQuestionOption is one selectable value of a choice Ticket Question.
type TicketQuestionOption struct {
	// ID is the Option's identity, and it is emphatically NOT its label: a
	// rename leaves this untouched, which is what lets the Answers already given
	// under the old wording stay attached to the same Option.
	ID               string
	TicketQuestionID string
	Label            string
	SortOrder        int
	RetiredAt        sql.NullTime
	// The Option's own review state, on the question's terms: an Option added
	// to an approved question is a draft until a Question Review approves it.
	ReviewStatus     string
	ApprovedAt       sql.NullTime
	ApprovedBy       sql.NullString
	RefusedAt        sql.NullTime
	RefusedBy        sql.NullString
	RefusalReason    sql.NullString
	RevokedAt        sql.NullTime
	RevokedBy        sql.NullString
	RevocationReason sql.NullString
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// reviewColumns are the ADR 0056 review columns, identical on both tables.
const reviewColumns = `review_status, approved_at, approved_by, refused_at, refused_by, refusal_reason,
	revoked_at, revoked_by, revocation_reason`

const ticketQuestionColumns = `
	id, ticket_type_id, label, kind, required, timing, sort_order,
	retired_at, ` + reviewColumns + `, created_at, updated_at
`

const ticketQuestionOptionColumns = `
	id, ticket_question_id, label, sort_order, retired_at, ` + reviewColumns + `, created_at, updated_at
`

func scanTicketQuestion(row interface {
	Scan(dest ...any) error
}) (*TicketQuestion, error) {
	var q TicketQuestion
	if err := row.Scan(
		&q.ID, &q.TicketTypeID, &q.Label, &q.Kind, &q.Required, &q.Timing,
		&q.SortOrder, &q.RetiredAt,
		&q.ReviewStatus, &q.ApprovedAt, &q.ApprovedBy, &q.RefusedAt, &q.RefusedBy, &q.RefusalReason,
		&q.RevokedAt, &q.RevokedBy, &q.RevocationReason,
		&q.CreatedAt, &q.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &q, nil
}

func scanTicketQuestionOption(row interface {
	Scan(dest ...any) error
}) (*TicketQuestionOption, error) {
	var o TicketQuestionOption
	if err := row.Scan(
		&o.ID, &o.TicketQuestionID, &o.Label, &o.SortOrder, &o.RetiredAt,
		&o.ReviewStatus, &o.ApprovedAt, &o.ApprovedBy, &o.RefusedAt, &o.RefusedBy, &o.RefusalReason,
		&o.RevokedAt, &o.RevokedBy, &o.RevocationReason,
		&o.CreatedAt, &o.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// ListTicketQuestionsByTicketTypeID returns a Ticket Type's Ticket Questions in
// the order they are asked.
//
// RETIRED QUESTIONS COME BACK TOO. This is an authoring read, and the surface
// has to be able to show an Organization what it retired — a question that
// vanished entirely would look like data loss, and the Answers under it are
// still on Tickets and still in the export. Callers that only want the live ones
// filter on RetiredAt; the retired ones sort after them so the working list is
// undisturbed.
func (r *Repository) ListTicketQuestionsByTicketTypeID(ctx context.Context, ticketTypeID string) ([]TicketQuestion, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+ticketQuestionColumns+`
		FROM ticket_questions
		WHERE ticket_type_id = $1
		ORDER BY (retired_at IS NOT NULL), sort_order ASC, created_at ASC
	`, ticketTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := make([]TicketQuestion, 0)
	for rows.Next() {
		q, err := scanTicketQuestion(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, *q)
	}
	return questions, rows.Err()
}

// ListTicketQuestionOptionsByTicketTypeID returns every Option belonging to
// every Ticket Question on a Ticket Type, so a list read is two queries rather
// than one per question.
func (r *Repository) ListTicketQuestionOptionsByTicketTypeID(ctx context.Context, ticketTypeID string) ([]TicketQuestionOption, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT o.id, o.ticket_question_id, o.label, o.sort_order, o.retired_at,
		       o.review_status, o.approved_at, o.approved_by, o.refused_at, o.refused_by, o.refusal_reason,
		       o.revoked_at, o.revoked_by, o.revocation_reason, o.created_at, o.updated_at
		FROM ticket_question_options o
		JOIN ticket_questions q ON q.id = o.ticket_question_id
		WHERE q.ticket_type_id = $1
		ORDER BY (o.retired_at IS NOT NULL), o.sort_order ASC, o.created_at ASC
	`, ticketTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	options := make([]TicketQuestionOption, 0)
	for rows.Next() {
		option, err := scanTicketQuestionOption(rows)
		if err != nil {
			return nil, err
		}
		options = append(options, *option)
	}
	return options, rows.Err()
}

// ListApprovedTicketQuestionsByTicketTypeID returns the Ticket Questions of one
// Ticket Type that were EVER ASKED — catalog.ApprovedQuestionSQL, retired ones
// included and last — which is what the answer views read (ADR 0056). A draft,
// under-review or refused question was put to nobody, so there is no Answer to
// pair it with and no reason to show a Holder a question they were never asked.
func (r *Repository) ListApprovedTicketQuestionsByTicketTypeID(ctx context.Context, ticketTypeID string) ([]TicketQuestion, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+ticketQuestionColumns+`
		FROM ticket_questions q
		WHERE q.ticket_type_id = $1
		  AND `+catalog.ApprovedQuestionSQL+`
		ORDER BY (q.retired_at IS NOT NULL), q.sort_order ASC, q.created_at ASC
	`, ticketTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	questions := make([]TicketQuestion, 0)
	for rows.Next() {
		q, err := scanTicketQuestion(rows)
		if err != nil {
			return nil, err
		}
		questions = append(questions, *q)
	}
	return questions, rows.Err()
}

// ListApprovedTicketQuestionOptionsByTicketTypeID is the Options counterpart:
// every approved Option of every approved question on a Ticket Type, retired
// ones included (catalog.ApprovedOptionSQL), so an Answer against a retired
// Option still has something to read its snapshot against.
func (r *Repository) ListApprovedTicketQuestionOptionsByTicketTypeID(ctx context.Context, ticketTypeID string) ([]TicketQuestionOption, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT o.id, o.ticket_question_id, o.label, o.sort_order, o.retired_at,
		       o.review_status, o.approved_at, o.approved_by, o.refused_at, o.refused_by, o.refusal_reason,
		       o.revoked_at, o.revoked_by, o.revocation_reason, o.created_at, o.updated_at
		FROM ticket_question_options o
		JOIN ticket_questions q ON q.id = o.ticket_question_id
		WHERE q.ticket_type_id = $1
		  AND `+catalog.ApprovedQuestionSQL+`
		  AND `+catalog.ApprovedOptionSQL+`
		ORDER BY (o.retired_at IS NOT NULL), o.sort_order ASC, o.created_at ASC
	`, ticketTypeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	options := make([]TicketQuestionOption, 0)
	for rows.Next() {
		option, err := scanTicketQuestionOption(rows)
		if err != nil {
			return nil, err
		}
		options = append(options, *option)
	}
	return options, rows.Err()
}

// ListTicketQuestionOptions returns one Ticket Question's Options, live first.
func (r *Repository) ListTicketQuestionOptions(ctx context.Context, questionID string) ([]TicketQuestionOption, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+ticketQuestionOptionColumns+`
		FROM ticket_question_options
		WHERE ticket_question_id = $1
		ORDER BY (retired_at IS NOT NULL), sort_order ASC, created_at ASC
	`, questionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	options := make([]TicketQuestionOption, 0)
	for rows.Next() {
		option, err := scanTicketQuestionOption(rows)
		if err != nil {
			return nil, err
		}
		options = append(options, *option)
	}
	return options, rows.Err()
}

// GetTicketQuestionByID loads a Ticket Question scoped to its Ticket Type. The
// Ticket Type is scoped to the Organization by the caller, which is what keeps
// one Organization's question id from resolving under another's Event.
func (r *Repository) GetTicketQuestionByID(ctx context.Context, ticketTypeID, questionID string) (*TicketQuestion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+ticketQuestionColumns+`
		FROM ticket_questions
		WHERE id = $1 AND ticket_type_id = $2
	`, questionID, ticketTypeID)
	return scanTicketQuestion(row)
}

// GetTicketQuestionOptionByID loads an Option scoped to its Ticket Question.
func (r *Repository) GetTicketQuestionOptionByID(ctx context.Context, questionID, optionID string) (*TicketQuestionOption, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+ticketQuestionOptionColumns+`
		FROM ticket_question_options
		WHERE id = $1 AND ticket_question_id = $2
	`, optionID, questionID)
	return scanTicketQuestionOption(row)
}

// CountLiveTicketQuestionOptions counts the Options a question currently offers.
// Retired ones are excluded, which is the count the twenty-Option cap is read
// against — see catalog.MaxTicketQuestionOptions for why.
func (r *Repository) CountLiveTicketQuestionOptions(ctx context.Context, questionID string) (int, error) {
	var count int
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM ticket_question_options
		WHERE ticket_question_id = $1 AND retired_at IS NULL
	`, questionID).Scan(&count)
	return count, err
}

// NextTicketQuestionSortOrder returns the place a new Ticket Question takes: the
// end of the list, as a new Ticket Type takes the end of its own.
func (r *Repository) NextTicketQuestionSortOrder(ctx context.Context, ticketTypeID string) (int, error) {
	var maxOrder sql.NullInt64
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT MAX(sort_order) FROM ticket_questions WHERE ticket_type_id = $1
	`, ticketTypeID).Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	if !maxOrder.Valid {
		return 0, nil
	}
	return int(maxOrder.Int64) + 1, nil
}

// NextTicketQuestionOptionSortOrder is the same for a new Option.
func (r *Repository) NextTicketQuestionOptionSortOrder(ctx context.Context, questionID string) (int, error) {
	var maxOrder sql.NullInt64
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT MAX(sort_order) FROM ticket_question_options WHERE ticket_question_id = $1
	`, questionID).Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	if !maxOrder.Valid {
		return 0, nil
	}
	return int(maxOrder.Int64) + 1, nil
}

// CreateTicketQuestionParams holds values for a new Ticket Question.
type CreateTicketQuestionParams struct {
	Label     string
	Kind      string
	Required  bool
	Timing    string
	SortOrder int
	// OptionLabels are the choice Options to create with the question, in order.
	// Empty for the five kinds that take none.
	OptionLabels []string
}

// CreateTicketQuestion inserts a Ticket Question and its Options in one
// transaction.
//
// One transaction because a choice question and its Options are one thing: a
// `single_choice` row that briefly exists with nothing to choose from is a state
// no reader should ever be able to observe, and a failure halfway would leave
// exactly that behind.
func (r *Repository) CreateTicketQuestion(
	ctx context.Context,
	ticketTypeID string,
	params CreateTicketQuestionParams,
	now time.Time,
) (*TicketQuestion, []TicketQuestionOption, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()

	question, err := scanTicketQuestion(tx.QueryRowContext(ctx, `
		INSERT INTO ticket_questions (
			ticket_type_id, label, kind, required, timing, sort_order,
			created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING `+ticketQuestionColumns+`
	`, ticketTypeID, params.Label, params.Kind, params.Required,
		params.Timing, params.SortOrder, now))
	if err != nil {
		return nil, nil, err
	}

	options := make([]TicketQuestionOption, 0, len(params.OptionLabels))
	for i, label := range params.OptionLabels {
		option, optErr := scanTicketQuestionOption(tx.QueryRowContext(ctx, `
			INSERT INTO ticket_question_options (
				ticket_question_id, label, sort_order, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $4)
			RETURNING `+ticketQuestionOptionColumns+`
		`, question.ID, label, i, now))
		if optErr != nil {
			return nil, nil, optErr
		}
		options = append(options, *option)
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return question, options, nil
}

// UpdateTicketQuestionParams holds the mutable fields of a Ticket Question.
//
// Note what is NOT here: retired_at, which only RetireTicketQuestion writes, and
// which no edit path may clear. Retirement is one-way — an un-retire would put a
// question back in front of buyers whose Answers were collected under a
// different understanding of the list.
type UpdateTicketQuestionParams struct {
	Label     string
	Kind      string
	Required  bool
	Timing    string
	SortOrder int
}

// UpdateTicketQuestion writes a Ticket Question's editable fields. Whether the
// kind among them is allowed to differ is the service's decision, not this
// one's — see catalog.TicketQuestionKindFrozen.
//
// A REFUSED QUESTION'S FIRST EDIT RETURNS IT TO DRAFT (#407, ADR 0056): the
// refusal was an invitation to change it, and once changed it is a new thing
// for the next Review to carry. The refusal's reason and author stay on the
// row as the record of what was said about the earlier wording.
func (r *Repository) UpdateTicketQuestion(
	ctx context.Context,
	ticketTypeID, questionID string,
	params UpdateTicketQuestionParams,
	now time.Time,
) (*TicketQuestion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_questions
		SET label = $3, kind = $4, required = $5, timing = $6, sort_order = $7, updated_at = $8,
		    review_status = CASE WHEN review_status = 'refused' THEN 'draft' ELSE review_status END
		WHERE id = $1 AND ticket_type_id = $2
		RETURNING `+ticketQuestionColumns+`
	`, questionID, ticketTypeID, params.Label, params.Kind, params.Required,
		params.Timing, params.SortOrder, now)
	return scanTicketQuestion(row)
}

// RetireTicketQuestion stamps a Ticket Question retired. There is deliberately
// no DeleteTicketQuestion: a question that any Ticket answered must keep reading
// on that Ticket and in the Sales Export, and no authoring surface can tell
// which those are.
func (r *Repository) RetireTicketQuestion(ctx context.Context, ticketTypeID, questionID string, now time.Time) (*TicketQuestion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_questions
		SET retired_at = $3, updated_at = $3
		WHERE id = $1 AND ticket_type_id = $2 AND retired_at IS NULL
		RETURNING `+ticketQuestionColumns+`
	`, questionID, ticketTypeID, now)
	return scanTicketQuestion(row)
}

// ReorderTicketQuestions resequences a Ticket Type's questions to the order
// given, in one transaction so a half-applied order is never readable.
//
// The ids are the caller's whole statement of the order: the service has already
// checked that they are exactly the Ticket Type's live questions, so an id here
// that names nothing is a bug rather than a case to tolerate.
func (r *Repository) ReorderTicketQuestions(ctx context.Context, ticketTypeID string, questionIDs []string, now time.Time) error {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for i, id := range questionIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ticket_questions
			SET sort_order = $3, updated_at = $4
			WHERE id = $1 AND ticket_type_id = $2
		`, id, ticketTypeID, i, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateTicketQuestionOption adds one Option to a Ticket Question. Options are
// added freely and at any time, Answers or not.
func (r *Repository) CreateTicketQuestionOption(
	ctx context.Context,
	questionID, label string,
	sortOrder int,
	now time.Time,
) (*TicketQuestionOption, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		INSERT INTO ticket_question_options (
			ticket_question_id, label, sort_order, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $4)
		RETURNING `+ticketQuestionOptionColumns+`
	`, questionID, label, sortOrder, now)
	return scanTicketQuestionOption(row)
}

// RenameTicketQuestionOption writes an Option's current label.
//
// The id is untouched, and that is the entire point of the Option having one: a
// correction from `Mediun` to `Medium` reaches every Answer already given, in
// the export header and in every list, without forking anything.
func (r *Repository) RenameTicketQuestionOption(
	ctx context.Context,
	questionID, optionID, label string,
	now time.Time,
) (*TicketQuestionOption, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_question_options
		SET label = $3, updated_at = $4
		WHERE id = $1 AND ticket_question_id = $2
		RETURNING `+ticketQuestionOptionColumns+`
	`, optionID, questionID, label, now)
	return scanTicketQuestionOption(row)
}

// RetireTicketQuestionOption stamps an Option retired: gone from new lists, kept
// on the Tickets that chose it, and still entitled to its export column. There
// is deliberately no delete.
func (r *Repository) RetireTicketQuestionOption(
	ctx context.Context,
	questionID, optionID string,
	now time.Time,
) (*TicketQuestionOption, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_question_options
		SET retired_at = $3, updated_at = $3
		WHERE id = $1 AND ticket_question_id = $2 AND retired_at IS NULL
		RETURNING `+ticketQuestionOptionColumns+`
	`, optionID, questionID, now)
	return scanTicketQuestionOption(row)
}

// TicketQuestionHasAnswers reports whether any Ticket has answered this Ticket
// Question — the fact the kind freeze is read against.
//
// It answered `false` unconditionally until #310 landed the Answer, which was
// the true answer while there was no table for one to live in. It is a real read
// now, and the guard in catalog.TicketQuestionKindFrozen has teeth for the first
// time: a `single_choice` whose Answers are Option identities does not become a
// `date` by relabelling, so once a Ticket has replied the kind is frozen and the
// way to change it is to retire the question and add another.
//
// EXISTS AND NOT COUNT: nothing here wants to know how many, and one is enough
// to freeze the kind. It reads every Answer including those on REVERSED Ticket
// Sales, deliberately — a reversed sale keeps its Tickets and their Answers, and
// they are still stored in this question's kind, so changing the kind under them
// would misread them exactly as it would misread a live one.
func (r *Repository) TicketQuestionHasAnswers(ctx context.Context, questionID string) (bool, error) {
	var exists bool
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM ticket_answers WHERE ticket_question_id = $1)
	`, questionID).Scan(&exists)
	return exists, err
}

// ListCheckoutQuestions returns the Ticket Questions a set of Ticket Types puts
// to a buyer AT CHECKOUT, each with the Options it currently offers — the form
// the Storefront event page draws (#311).
//
// The query and the fold are catalog.CheckoutQuestionsSQL and
// catalog.ScanAskedQuestions, SHARED WITH THE SALES REPOSITORY, which reads what
// comes back from that form. One definition of "what is this buyer being asked",
// used by the surface that asks it and by the surface that judges the reply: two
// copies of those filters would eventually differ, and the failure would be a
// question shown whose answer is dropped, or one never shown that the capture
// accepts.
//
// It takes Ticket Type ids rather than an Event id because that is what BOTH
// callers hold — the event page has just listed its Ticket Types, and the
// checkout has just resolved a cart — and because a Ticket Question belongs to a
// Ticket Type and to nothing above it.
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
