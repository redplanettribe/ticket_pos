package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The Platform Operator's reads and one write on Ticket Questions (#410,
// ADR 0056). Unscoped by Organization: the caller's authority is the operator
// allowlist, checked in the middleware on the operator namespace (ADR 0015).

// OperatorTicketQuestionRow is one question as the Operator's Event view lists
// it: the question and the Ticket Type it hangs off.
type OperatorTicketQuestionRow struct {
	TicketQuestion
	TicketTypeName string
}

// OperatorTicketQuestionContext is what a Revocation needs to know about a
// question beyond the row: whose it is, and what to call the Event and the
// Organization in the notice.
type OperatorTicketQuestionContext struct {
	TicketQuestion
	EventID          string
	EventName        string
	OrganizationID   string
	OrganizationName string
}

// ListTicketQuestionsByEventIDForOperator returns every Ticket Question of an
// Event across its Ticket Types, in the order the Ticket Types were created
// and then the order the questions are asked, retired ones after the live ones
// of the same Ticket Type. Retired questions come back for the reason the
// authoring read returns them: a revoked question must still be visible where
// the Operator revoked it.
func (r *Repository) ListTicketQuestionsByEventIDForOperator(ctx context.Context, eventID string) ([]OperatorTicketQuestionRow, error) {
	rows, err := r.db.Pool.QueryContext(ctx, `
		SELECT `+prefixed("q", ticketQuestionColumns)+`, tt.name
		FROM ticket_questions q
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		WHERE tt.event_id = $1
		ORDER BY tt.created_at, tt.id, (q.retired_at IS NOT NULL), q.sort_order, q.created_at
	`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []OperatorTicketQuestionRow
	for rows.Next() {
		var row OperatorTicketQuestionRow
		if err := rows.Scan(
			&row.ID, &row.TicketTypeID, &row.Label, &row.Kind, &row.Required, &row.Timing,
			&row.SortOrder, &row.RetiredAt,
			&row.ReviewStatus, &row.ApprovedAt, &row.ApprovedBy, &row.RefusedAt, &row.RefusedBy, &row.RefusalReason,
			&row.RevokedAt, &row.RevokedBy, &row.RevocationReason,
			&row.CreatedAt, &row.UpdatedAt,
			&row.TicketTypeName,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetTicketQuestionForOperator returns one question with its Event and
// Organization, whatever Organization it belongs to, or nil for none.
func (r *Repository) GetTicketQuestionForOperator(ctx context.Context, questionID string) (*OperatorTicketQuestionContext, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		SELECT `+prefixed("q", ticketQuestionColumns)+`, e.id, e.name, o.id, o.name
		FROM ticket_questions q
		JOIN ticket_types tt ON tt.id = q.ticket_type_id
		JOIN events e ON e.id = tt.event_id
		JOIN organizations o ON o.id = e.organization_id
		WHERE q.id = $1
	`, questionID)
	var out OperatorTicketQuestionContext
	if err := row.Scan(
		&out.ID, &out.TicketTypeID, &out.Label, &out.Kind, &out.Required, &out.Timing,
		&out.SortOrder, &out.RetiredAt,
		&out.ReviewStatus, &out.ApprovedAt, &out.ApprovedBy, &out.RefusedAt, &out.RefusedBy, &out.RefusalReason,
		&out.RevokedAt, &out.RevokedBy, &out.RevocationReason,
		&out.CreatedAt, &out.UpdatedAt,
		&out.EventID, &out.EventName, &out.OrganizationID, &out.OrganizationName,
	); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

// RevokeTicketQuestion is a Revocation: a retirement signed by the Operator
// with the reason the Organization is told (ADR 0056). review_status stays
// `approved` because the approval was real and the Answers given under it
// stay; the shared "asked" predicate stops asking it because retired_at is
// set. Guarded on the row being approved and live, so a second Revocation, or
// one racing a retirement, writes nothing and returns nil.
func (r *Repository) RevokeTicketQuestion(ctx context.Context, questionID, reason, operator string, now time.Time) (*TicketQuestion, error) {
	row := r.db.Pool.QueryRowContext(ctx, `
		UPDATE ticket_questions
		SET retired_at = $4, revoked_at = $4, revoked_by = $3, revocation_reason = $2, updated_at = $4
		WHERE id = $1 AND review_status = 'approved' AND retired_at IS NULL
		RETURNING `+ticketQuestionColumns+`
	`, questionID, reason, operator, now)
	return scanTicketQuestion(row)
}

// prefixed qualifies every column of a column list with a table alias, so the
// staff read's column list serves a join without a second copy to drift.
func prefixed(alias, columns string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
