package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// THE CONSENT ACCESS LOG (#569, parent #556, ADR 0067): one write and one read
// over migration 116, and there is deliberately nothing else in this file.
//
// NO UPDATE, NO DELETE, NO PURGE AND NO COUNT. The table is append-only, its
// retention is unbounded, and the absences are load-bearing rather than
// unimplemented: a purge is a way for evidence to stop existing on a schedule
// nobody remembers setting, and an audit log with an UPDATE beside it is a log
// whose rows are a claim rather than a record. If one of these is ever wanted,
// the argument belongs in an ADR before the method belongs here.
//
// IT LIVES IN THE CONSENT MODULE and not in the operator module, for ADR 0015's
// reason: the operator surface owns no tables, and this table is about consent
// evidence — its subject is a Customer, its FK is `customers`, and it sits
// beside `consent_records` and `consent_evidence_packs`, which it is meaningless
// without. The operator module composes it through a seam, exactly as it
// composes the record read it audits.

// ConsentAccessAct is one of the four ways somebody's data is touched.
//
// A TYPED CONSTANT SET, matching migration 116's CHECK, so a caller cannot
// invent an act at a call site: an unknown string would be refused by the
// database, but at the wrong layer and at the wrong time — halfway through
// serving a read somebody has already performed.
type ConsentAccessAct string

const (
	// ConsentAccessListRead is a page of one of the acceptance browsers.
	ConsentAccessListRead ConsentAccessAct = "list_read"
	// ConsentAccessSubjectRead is one person's record being opened.
	ConsentAccessSubjectRead ConsentAccessAct = "subject_read"
	// ConsentAccessEvidenceExport is a Consent Evidence Pack being generated.
	ConsentAccessEvidenceExport ConsentAccessAct = "evidence_export"
	// ConsentAccessAuditRead is a page of this log itself.
	ConsentAccessAuditRead ConsentAccessAct = "audit_read"
)

// ConsentAccessEntry is one act on its way into the log.
//
// EVERY OPTIONAL FIELD IS A POINTER, because migration 116's act-whole CHECK
// distinguishes absent from empty and refuses a row that is neither. A zero
// `result_count` is a real answer — the search found nobody — and a zero that
// meant "not recorded" would make the one row worth looking at indistinguishable
// from the one nobody filled in.
type ConsentAccessEntry struct {
	Act ConsentAccessAct
	// ActorEmail is the operator, from the Staff Session and never from a body.
	ActorEmail string

	// The question, on `list_read` and `audit_read`.
	Population   *string
	Document     *string
	StatusFilter *string
	// SearchTerm is WHETHER a search narrowed the page and never what was
	// searched for. See migration 116.
	SearchTerm  *bool
	ResultCount *int

	// The subject, on `subject_read` and `evidence_export`.
	SubjectCustomerID *string
	// SubjectEmail is a plain address. A staff subject is resolved from their
	// Staff Digest BEFORE the row is written; the digest itself is never stored,
	// because a key rotation would orphan every row carrying one.
	SubjectEmail *string

	// PackSHA256 is the fingerprint of the file handed over, on
	// `evidence_export` alone.
	PackSHA256 *string
}

// RecordConsentAccess appends one row.
//
// NO TIMESTAMP IS PASSED. `occurred_at` takes the column default, which is the
// database's NOW(), so the log records when the platform was actually touched
// and no caller — or test — can name a time for an act it performed. It is the
// one clock in this module that is deliberately NOT the injectable evidence
// clock: that one exists so the integration harness can capture consent at a
// moment it chose, and an audit log whose entries can be dated by their author
// is not an audit log.
//
// IT RETURNS ONLY AN ERROR. Nothing downstream needs the row's id: the log is
// read as a list and joined to nothing, and handing back an id would invite a
// caller to store it somewhere and create the second, disagreeing record this
// table exists to avoid.
func (r *Repository) RecordConsentAccess(ctx context.Context, entry ConsentAccessEntry) error {
	const query = `
		INSERT INTO consent_access_log (
			act, actor_email,
			population, document, status_filter, search_term, result_count,
			subject_customer_id, subject_email, pack_sha256
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::uuid, $9, $10)
	`
	if _, err := r.db.Pool.ExecContext(ctx, query,
		string(entry.Act), entry.ActorEmail,
		entry.Population, entry.Document, entry.StatusFilter, entry.SearchTerm, entry.ResultCount,
		entry.SubjectCustomerID, entry.SubjectEmail, entry.PackSHA256,
	); err != nil {
		return fmt.Errorf("record consent access: %w", err)
	}
	return nil
}

// ConsentAccessRow is one logged act as the reader sees it.
type ConsentAccessRow struct {
	ID         int64
	Act        string
	ActorEmail string
	OccurredAt time.Time

	Population   sql.NullString
	Document     sql.NullString
	StatusFilter sql.NullString
	SearchTerm   sql.NullBool
	ResultCount  sql.NullInt64

	SubjectCustomerID sql.NullString
	SubjectEmail      sql.NullString

	PackSHA256 sql.NullString
}

// ConsentAccessCursor is a keyset position in the log: the (occurred_at, id) of
// the last row on the page just served.
//
// TWO COLUMNS, for CustomerConsentRecords' reason: two acts inside one clock
// tick would otherwise be skipped or repeated, and the id — a BIGSERIAL, so
// monotonic — is the tiebreak that also happens to be the order they really
// occurred in.
type ConsentAccessCursor struct {
	OccurredAt time.Time
	ID         int64
}

// ConsentAccessFilter is one page of the log.
//
// THREE FILTERS AND NO FOURTH. Actor, act and date, exactly as the reader
// offers them — and NO SUBJECT FILTER, which is the whole design: an audit log
// searchable by data subject is a second way to look people up, keyed on the
// record of people being looked up, and it would be reached by the one role
// that already has the first way. The field does not exist here so that no
// screen can be one line away from having it.
type ConsentAccessFilter struct {
	// ActorEmail narrows to one operator, "" for everybody. An exact match on a
	// normalised address rather than a fragment: an operator allowlist is a
	// handful of people who are picked from a list, not searched for, and a
	// LIKE here would be the beginnings of a search box over addresses.
	ActorEmail string
	// Act narrows to one of the four, "" for all of them.
	Act string
	// From and To bound `occurred_at`, either or both zero for unbounded. Both
	// are instants and the caller decides what a day means — the log stores
	// TIMESTAMPTZ and this layer does not guess a timezone.
	From time.Time
	To   time.Time
	// After is the previous page's last position, or nil for the first page.
	After *ConsentAccessCursor
	// Limit is the page size PLUS ONE, so the caller learns whether another
	// page exists without a COUNT (ADR 0067).
	Limit int
}

// ConsentAccessLog returns one keyset page of the log, NEWEST FIRST.
//
// ONE ORDER AND NO OTHER. There is no ascending read and no sort parameter: the
// question this screen answers is "what has been touched lately", and a
// reversible sort over an append-only log would only ever be used to walk it
// from the beginning, which is an export performed one page at a time.
//
// The comparison is a ROW VALUE against (occurred_at, id), matching the ORDER BY
// and the index (migration 116) so none of the three can drift from the others.
func (r *Repository) ConsentAccessLog(ctx context.Context, filter ConsentAccessFilter) ([]ConsentAccessRow, error) {
	// Every filter is a NULL-guarded predicate in ONE statement, so the
	// unfiltered first page and a filtered page two share a plan and there is
	// no string-built WHERE clause anywhere near an audit log.
	const query = `
		SELECT id, act, actor_email, occurred_at,
		       population, document, status_filter, search_term, result_count,
		       subject_customer_id, subject_email, pack_sha256
		FROM consent_access_log
		WHERE ($1::text IS NULL OR actor_email = $1::text)
		  AND ($2::text IS NULL OR act = $2::text)
		  AND ($3::timestamptz IS NULL OR occurred_at >= $3::timestamptz)
		  AND ($4::timestamptz IS NULL OR occurred_at < $4::timestamptz)
		  AND ($5::timestamptz IS NULL OR (occurred_at, id) < ($5::timestamptz, $6::bigint))
		ORDER BY occurred_at DESC, id DESC
		LIMIT $7
	`

	var (
		actor    sql.NullString
		act      sql.NullString
		from     sql.NullTime
		to       sql.NullTime
		cursorAt sql.NullTime
		cursorID sql.NullInt64
	)
	if filter.ActorEmail != "" {
		actor = sql.NullString{String: filter.ActorEmail, Valid: true}
	}
	if filter.Act != "" {
		act = sql.NullString{String: filter.Act, Valid: true}
	}
	if !filter.From.IsZero() {
		from = sql.NullTime{Time: filter.From, Valid: true}
	}
	if !filter.To.IsZero() {
		to = sql.NullTime{Time: filter.To, Valid: true}
	}
	if filter.After != nil {
		cursorAt = sql.NullTime{Time: filter.After.OccurredAt, Valid: true}
		cursorID = sql.NullInt64{Int64: filter.After.ID, Valid: true}
	}

	rows, err := r.db.Pool.QueryContext(ctx, query, actor, act, from, to, cursorAt, cursorID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("consent access log: %w", err)
	}
	defer rows.Close()

	page := make([]ConsentAccessRow, 0, filter.Limit)
	for rows.Next() {
		var row ConsentAccessRow
		if err := rows.Scan(
			&row.ID, &row.Act, &row.ActorEmail, &row.OccurredAt,
			&row.Population, &row.Document, &row.StatusFilter, &row.SearchTerm, &row.ResultCount,
			&row.SubjectCustomerID, &row.SubjectEmail, &row.PackSHA256,
		); err != nil {
			return nil, fmt.Errorf("consent access log: %w", err)
		}
		page = append(page, row)
	}
	return page, rows.Err()
}
