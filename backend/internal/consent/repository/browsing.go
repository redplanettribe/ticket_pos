package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// Browsing the Customer base by what it owes (#565, parent #556, ADR 0067).
//
// The question "who has not accepted the current Terms" has always been
// answerable — it is one predicate against one column — and until now the only
// place to ask it was psql. This is that question, paged.
//
// ONE ROW PER PERSON, WITH BOTH DOCUMENTS ON IT. A person is one row and not
// two: the two acceptances travel on the same `customers` row, so returning
// them separately would mean the same human appearing twice on one screen and
// the operator reconciling them by eye.

// CustomerAcceptanceRow is one person on the customer acceptance browser: who
// they are, and which edition of each document they hold.
//
// IT CARRIES NO OPTIONAL CONSENT — no marketing, no networking, not even
// nullable. That is a structural refusal and not an oversight: a filterable
// roster with a marketing-consent column IS a segmentation tool, whatever it is
// called (#565). A type that cannot hold the value is a type no handler can
// accidentally serialise it from, and the omission survives somebody later
// adding a column to the SELECT without reading this comment.
//
// The edition ids are the raw version ids; the standing is computed from them
// against the satisfying set, in Go, by legal.StandingOf. They are NOT resolved
// to labels here, because the browser has NO PER-EDITION FILTER and shows no
// edition column — it answers "who owes something", never "who is on 1.1".
type CustomerAcceptanceRow struct {
	// ID is the Customer's UUID — the key the per-subject record is reached by
	// (#566). A CUSTOMER NEEDS NO DIGEST for the reason a staff person does:
	// Customer identity is UUID-keyed, so a link out of this row names an
	// opaque id and no address ever reaches a URL, a query string or a referer.
	ID string
	// Email is the address, in the response BODY only. It is the column an
	// operator recognises a person by and it must be shown; what must never
	// happen is its appearing in a URL, which is why every one of these reads
	// is driven from a POSTed body (see the handler).
	Email     string
	FirstName string
	LastName  string
	// PolicyEditionID and TermsEditionID are "" where the person has never
	// accepted that document — NULL in the column, which migration 062 chose
	// deliberately over a backfilled default because staff never accept on a
	// buyer's behalf and there is nothing truthful to backfill with.
	PolicyEditionID string
	TermsEditionID  string
}

// CustomerAcceptanceFilter is one page of the customer browser.
type CustomerAcceptanceFilter struct {
	// Document is `policy` or `terms`: WHICH document's gate the Standing
	// filter is applied to. Both documents' editions come back on every row
	// regardless — the filter chooses what the page is ABOUT, never what it
	// shows.
	//
	// A STRING, matching the Legal Center's own document vocabulary
	// (service.LegalDocumentPolicy / LegalDocumentTerms, #561), which this
	// package cannot import without a cycle. It is never interpolated into the
	// query: the closed switch below maps it to one of two column names
	// written in this file, and an unknown value is an error rather than SQL.
	Document string
	// Standing is the state being asked for. legal.StandingFormer is not
	// reachable here and the service refuses it: a Customer never becomes
	// former, because Customer records are never deleted.
	Standing legal.Standing
	// Satisfying is the set of edition ids that clear this document's gate
	// (#560). Membership, never equality against the current edition — which
	// is what makes a correction move nobody into outstanding.
	Satisfying []string
	// CursorEmail is the last email of the previous page; "" starts at the
	// beginning. THE SEEK IS STRICTLY GREATER, and `email` is UNIQUE on
	// `customers` (migration 016), so no row can be skipped and none repeated —
	// the tiebreak column a non-unique keyset would need does not exist here
	// because the sort key is already unique.
	CursorEmail string
	// SearchEmail narrows to addresses CONTAINING this fragment, and arrives
	// from a POSTed body. It composes with the standing filter rather than
	// replacing it, so "is this person outstanding?" is one question and not
	// two screens.
	SearchEmail string
	// Limit is the page size plus one: the caller asks for 51 to learn whether
	// a 52nd exists, and there is deliberately NO COUNT — see the service.
	Limit int
}

// BrowseCustomerAcceptances returns one keyset page of the customer acceptance
// browser, ordered by email ascending.
//
// ORDER BY email ASC AND A `>` SEEK, NOT OFFSET/LIMIT, and this is the
// departure ADR 0067 records from ADR 0006's house pagination. The reason is
// specific: immediately after a gating publication the outstanding set is the
// ENTIRE CUSTOMER BASE, so an operator working through it walks to deep offsets
// exactly when the screen matters most, and OFFSET n makes the database count
// and discard n rows for every page — quadratic over a walk. A seek reads 51
// index entries whatever page it is on.
//
// AND NO `COUNT(*) OVER()`, which is the other half of the departure. The total
// is the expensive half of the query — it cannot be answered from 51 index
// entries and forces the full filtered scan the seek just avoided — and it is
// the least actionable number on the screen: "1,569 people owe an acceptance"
// changes nothing an operator does next.
//
// The predicate per standing, over the chosen document's column, is exactly
// spec #556's:
//
//	current      version_id = ANY($satisfying)
//	outstanding  version_id IS NOT NULL AND version_id <> ALL($satisfying)
//	never seen   version_id IS NULL
//
// `version_id <> ALL(...)` is written with the NOT NULL beside it and not left
// to three-valued logic: `NULL <> ALL(...)` is NULL, which is falsy, so the
// null rows would fall out of `outstanding` anyway — but relying on that would
// make the difference between the two largest buckets on the screen depend on
// an unstated SQL subtlety rather than on a written predicate.
//
// Both predicates are served by migration 114's (version_id, email) index,
// nulls included, which is why that index leads on the version column.
func (r *Repository) BrowseCustomerAcceptances(ctx context.Context, filter CustomerAcceptanceFilter) ([]CustomerAcceptanceRow, error) {
	column, ok := customerVersionColumn(filter.Document)
	if !ok {
		return nil, fmt.Errorf("browse customer acceptances: unknown document %q", filter.Document)
	}
	predicate, ok := customerStandingPredicate(filter.Standing, column)
	if !ok {
		return nil, fmt.Errorf("browse customer acceptances: unsupported standing %q", filter.Standing)
	}

	// $1 satisfying, $2 cursor, $3 search, $4 limit. Every one of them is a
	// bound parameter; the only interpolation is `column` and `predicate`,
	// which are compile-time literals chosen by the two closed switches above
	// and can never be caller input.
	//
	// NULLIF/`= ''` rather than two query strings: an empty cursor and an empty
	// search are the ordinary first page, and building the SQL up
	// conditionally would give this one screen four plans to reason about.
	//
	// `($1::uuid[] IS NULL OR TRUE)` IS A TYPE ANCHOR AND NOT A FILTER. It is
	// unconditionally true and filters nothing; it exists because the
	// `never_seen` predicate does not mention $1 at all, and Postgres refuses a
	// statement whose parameter appears nowhere it can infer a type from
	// (SQLSTATE 42P18). Anchoring it here rather than building three parameter
	// lists keeps the three standings ONE statement shape with one plan to
	// reason about.
	//
	// The `::uuid[]` cast is load-bearing on the other two as well: the ids
	// travel from Go as strings, and without the cast the driver's text array
	// meets a uuid column and the comparison is refused.
	query := `
		SELECT id, email, first_name, last_name, policy_version_id, terms_version_id
		FROM customers
		WHERE ($1::uuid[] IS NULL OR TRUE)
		  AND ` + predicate + `
		  AND ($2 = '' OR email > $2)
		  AND ($3 = '' OR email LIKE '%' || $3 || '%')
		ORDER BY email ASC
		LIMIT $4
	`

	rows, err := r.db.Pool.QueryContext(ctx, query,
		filter.Satisfying, filter.CursorEmail, filter.SearchEmail, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("browse customer acceptances: %w", err)
	}
	defer rows.Close()

	page := make([]CustomerAcceptanceRow, 0, filter.Limit)
	for rows.Next() {
		var (
			row    CustomerAcceptanceRow
			policy sql.NullString
			terms  sql.NullString
		)
		if err := rows.Scan(&row.ID, &row.Email, &row.FirstName, &row.LastName, &policy, &terms); err != nil {
			return nil, fmt.Errorf("browse customer acceptances: %w", err)
		}
		row.PolicyEditionID = policy.String
		row.TermsEditionID = terms.String
		page = append(page, row)
	}
	return page, rows.Err()
}

// customerVersionColumn maps a document to the `customers` column holding which
// edition of it the person accepted.
//
// A CLOSED SWITCH RETURNING A LITERAL, so the interpolation above can only ever
// be one of two strings written in this file. A map, or a struct field carrying
// a name, would put a column name somewhere a caller could reach.
func customerVersionColumn(document string) (string, bool) {
	switch document {
	case "policy":
		return "policy_version_id", true
	case "terms":
		return "terms_version_id", true
	}
	return "", false
}

// customerStandingPredicate is the WHERE clause for one standing over one
// column. Same closed-switch discipline, same reason.
//
// legal.StandingFormer is absent and returns false: there is no such thing as a
// former Customer, and a predicate that pretended otherwise would have to guess
// at departure from inactivity.
func customerStandingPredicate(standing legal.Standing, column string) (string, bool) {
	switch standing {
	case legal.StandingCurrent:
		return column + " = ANY($1::uuid[])", true
	case legal.StandingOutstanding:
		return column + " IS NOT NULL AND " + column + " <> ALL($1::uuid[])", true
	case legal.StandingNeverSeen:
		return column + " IS NULL", true
	}
	return "", false
}
