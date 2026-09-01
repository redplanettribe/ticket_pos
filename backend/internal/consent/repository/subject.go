package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ONE PERSON'S CONSENT RECORD (#566, parent #556, ADR 0067): the complete log
// of what one Customer did, read one page at a time.
//
// It is the OTHER HALF of #565. The browsers answer "who owes an acceptance"
// over a population and carry no optional consent at all, deliberately: a
// filterable roster with a marketing column is a segmentation tool. This file
// answers "what did THIS person do", one human being at a time, and therefore
// may carry everything — because reading one record is an act about one person
// and cannot be turned into a list of people who would like to be emailed.
//
// THE RECORD IS THE EVIDENCE RATHER THAN A SUMMARY OF IT. Every column of
// `consent_records` that says something about what happened is read here and
// handed up verbatim: what was answered, what it replaced, on which surface, in
// which language the notice was rendered, and — where somebody else entered it
// — who did and against what paper. Nothing is collapsed, nothing is derived,
// and a NULL travels as a NULL so the surface above can spell it in words
// instead of rendering a blank that reads as a loss.
//
// THIS IS THE SEAM THE EVIDENCE PACK IS GENERATED FROM (#568). The pack must
// name exactly what the screen names — an export that told a different story
// from the record it was exported from would be worse than no export — so it
// walks CustomerConsentRecords with the same cursor the screen uses rather than
// growing a second query beside it.

// ConsentRecordRow is one row of `consent_records` as the per-subject record
// shows it: an immutable transcript of one capture act.
//
// EVERY NULLABLE COLUMN STAYS NULLABLE ALL THE WAY UP. sql.NullBool and
// sql.NullString rather than bool and string, because on this table the
// difference between "answered No" and "the box was not shown", and between
// "collected nothing" and "collected a blank", is the whole point of the
// evidence. Scanning a NULL into a zero value here would destroy the
// distinction two migrations (061, 067) exist to preserve, several layers below
// the screen whose job is to say "not recorded" out loud.
type ConsentRecordRow struct {
	// ID is the row's own id — the keyset tiebreak, and what #568's pack names
	// an act by.
	ID string
	// CapturedAt is the server's clock at the moment of the act, and the
	// primary sort key: the record reads newest first, because the question an
	// operator arrives with is almost always about what happened last.
	CapturedAt time.Time
	// Channel is the surface the person was on. It is what decides whether a
	// presented locale is even a meaningful question — six channels present no
	// document at all (migration 115) — and the surface above omits the field
	// entirely on those rather than rendering a blank beside them.
	Channel string
	// Email is the address AS ASSERTED at the moment of capture, which is not
	// necessarily the Customer's address today (migration 061). It is shown
	// because a record that silently substituted the current address would
	// answer a question about the past with a fact about the present.
	Email string
	// PolicyVersionID is the Policy edition this act was captured against. NOT
	// NULL on the table: every capture happens beside a published Policy.
	PolicyVersionID string
	// The three Privacy Policy answers. Invalid means the box was not shown on
	// this surface, which is emphatically not a No.
	PolicyAcceptance  sql.NullBool
	MarketingConsent  sql.NullBool
	NetworkingConsent sql.NullBool
	// What each optional consent was in immediately before this act (#266,
	// migration 067), observed under the Customer row's lock by the write that
	// replaced it. Invalid where the answer beside it was invalid — a surface
	// that did not ask has replaced nothing — and invalid, too, where the prior
	// state was itself unanswered.
	//
	// IT IS THE ONLY WAY TO READ WHETHER AN ACT TOOK SOMETHING AWAY. `denied`
	// looks identical whether somebody just gave something up or refused for
	// the second time, so a record without this column could not answer the one
	// question a withdrawal is ever audited on.
	PriorMarketingConsent  sql.NullString
	PriorNetworkingConsent sql.NullString
	// The Terms answer and WHICH EDITION was shown (#536, migration 106). Both
	// invalid where the box was not shown; the CHECK holds the pair together.
	TermsAcceptance sql.NullBool
	TermsVersionID  sql.NullString
	// The Adulthood Declaration made in that same act (#590, migration 119,
	// ADR 0069) — TRUE OR INVALID AND NEVER FALSE. A refusal is refused before
	// any capture and writes nothing at all, so there is no row anywhere saying
	// somebody declared themselves a minor and this column can never scan one.
	//
	// Invalid therefore means THE ACT DID NOT ASK: the edition it was captured
	// against carried no `label-adulthood-declaration` Artifact, or the surface
	// showed no Terms box at all. That is a third state, not a No, and it stays
	// a NullBool for exactly the reason the answers above do — the screen and
	// the Evidence Pack spell it "never asked", which is a sentence a zero value
	// could not have been recovered into.
	//
	// It rides `terms_version_id` beside it for the edition and has no version
	// column of its own: what was editioned is the WORDING SHOWN, which is an
	// Artifact of the Terms edition the row already names.
	AdulthoodDeclaration sql.NullBool
	// EmailProven is whether the address was proven at the moment of the act.
	// It is what separates a granted consent from a Pending Confirmation, so it
	// belongs on the record even though the resulting state is beside it.
	EmailProven bool
	// The technical proof. Invalid where the surface collected nothing, so
	// "not collected" stays distinguishable from "collected as blank".
	IP        sql.NullString
	UserAgent sql.NullString
	SessionID sql.NullString
	OriginURL sql.NullString
	// PresentedLocale is the language of the legal text this act was captured
	// beside (#567, migration 115) — the language of the ARTIFACT RENDERED and
	// never of the page it was rendered on. Invalid on every channel that
	// showed no document, which is the truthful answer and not a gap.
	PresentedLocale sql.NullString
	// Who recorded this act on the Customer's behalf, and which artefact it
	// answers (#271, migration 067). Both invalid on every act a Customer
	// performed themselves.
	RecordedBy       sql.NullString
	RequestReference sql.NullString
	// ConfirmedAt and ConfirmationSentAt are the two one-way stamps migration
	// 061's rule admits: that the act was later corroborated from the address
	// itself, and that its subject was later told about it. Both invalid until
	// the later event happens, and neither ever alters what the row says the
	// person did.
	ConfirmedAt        sql.NullTime
	ConfirmationSentAt sql.NullTime
}

// ConsentRecordCursor is a keyset position in one person's history: the
// (captured_at, id) of the last act on the page just served.
//
// TWO COLUMNS AND NOT ONE, because `captured_at` is NOT UNIQUE. A checkout that
// commits inside one transaction, or any two acts stamped from the same clock
// reading, can share a tick to the microsecond; a cursor on the timestamp alone
// would either skip the second row or serve it twice, and on an evidence log
// either would be a defect somebody eventually has to explain. The id breaks
// the tie, and the pair is compared as a row value so the comparison and the
// ORDER BY cannot drift apart.
type ConsentRecordCursor struct {
	CapturedAt time.Time
	ID         string
}

// ConsentRecordFilter is one page of one person's history.
type ConsentRecordFilter struct {
	// CustomerID is whose record this is. The record is reached by UUID and
	// never by address: an opaque id in a URL discloses nobody, and #565's rule
	// is that a data subject's email must not reach a URL, a query string, a
	// referer or an access log.
	CustomerID string
	// After is the previous page's last position, or nil for the first page.
	After *ConsentRecordCursor
	// Limit is the page size PLUS ONE, so the caller learns whether another
	// page exists without a COUNT — the browsers' rule (ADR 0067), for a
	// smaller reason here: a person's history is short, but the screen must
	// still be able to say whether what is on it is the whole of it.
	Limit int
}

// CustomerConsentRecords returns one keyset page of one Customer's consent
// acts, newest first.
//
// KEYSET ON (captured_at DESC, id DESC), SERVED BY THE EXISTING
// (customer_id, captured_at DESC) INDEX (migration 061). No new index is
// needed and none is added: the index seeks straight to the customer's slice
// and walks it in the order this query asks for, and the id only breaks ties
// within one tick.
//
// THE COMPARISON IS A ROW VALUE, `(captured_at, id) < ($2, $3)`, and not the
// hand-expanded `captured_at < $2 OR (captured_at = $2 AND id < $3)`. The two
// mean the same thing; the row value cannot be got subtly wrong by somebody
// later adding a third column to the sort, and it is the form Postgres matches
// against the index.
//
// It is a READ AND NOTHING ELSE. No consent is captured, no state is touched
// and no row is stamped — reading a record must not put an act in the evidence
// log that nobody performed, which is the same rule the old email lookup held.
func (r *Repository) CustomerConsentRecords(ctx context.Context, filter ConsentRecordFilter) ([]ConsentRecordRow, error) {
	// $2/$3 are the cursor, both NULL on the first page: the guard reads "no
	// cursor, or strictly before it", so the first page and page two are ONE
	// statement with one plan rather than two query strings that could drift.
	const query = `
		SELECT id, captured_at, channel, email, policy_version_id,
		       policy_acceptance, marketing_consent, networking_consent,
		       prior_marketing_consent, prior_networking_consent,
		       terms_acceptance, terms_version_id, adulthood_declaration,
		       email_proven, ip, user_agent, session_id, origin_url,
		       presented_locale, recorded_by, request_reference,
		       confirmed_at, confirmation_sent_at
		FROM consent_records
		WHERE customer_id = $1
		  AND ($2::timestamptz IS NULL OR (captured_at, id) < ($2::timestamptz, $3::uuid))
		ORDER BY captured_at DESC, id DESC
		LIMIT $4
	`

	var (
		cursorAt sql.NullTime
		cursorID sql.NullString
	)
	if filter.After != nil {
		cursorAt = sql.NullTime{Time: filter.After.CapturedAt, Valid: true}
		cursorID = sql.NullString{String: filter.After.ID, Valid: true}
	}

	rows, err := r.db.Pool.QueryContext(ctx, query, filter.CustomerID, cursorAt, cursorID, filter.Limit)
	if err != nil {
		return nil, fmt.Errorf("customer consent records: %w", err)
	}
	defer rows.Close()

	page := make([]ConsentRecordRow, 0, filter.Limit)
	for rows.Next() {
		var row ConsentRecordRow
		if err := rows.Scan(
			&row.ID, &row.CapturedAt, &row.Channel, &row.Email, &row.PolicyVersionID,
			&row.PolicyAcceptance, &row.MarketingConsent, &row.NetworkingConsent,
			&row.PriorMarketingConsent, &row.PriorNetworkingConsent,
			&row.TermsAcceptance, &row.TermsVersionID, &row.AdulthoodDeclaration,
			&row.EmailProven, &row.IP, &row.UserAgent, &row.SessionID, &row.OriginURL,
			&row.PresentedLocale, &row.RecordedBy, &row.RequestReference,
			&row.ConfirmedAt, &row.ConfirmationSentAt,
		); err != nil {
			return nil, fmt.Errorf("customer consent records: %w", err)
		}
		page = append(page, row)
	}
	return page, rows.Err()
}

// CustomerSubjectRow is who the record is about, and what is true of them NOW.
//
// IT IS NOT THE CUSTOMER'S PROFILE, and the omissions are the same ones the old
// operator lookup made for the same reason (#271): no phone number, no Tax ID,
// no avatar, no purchases. An operator reading a consent record needs to be
// sure they have the right human being and to see the four consent facts; a
// payload carrying anything else would be a cross-Organization view of somebody
// personal data justified by a compliance ticket.
type CustomerSubjectRow struct {
	ID        string
	Email     string
	FirstName string
	LastName  string
	// The two gates' standing facts. Each edition id is "" where the person has
	// never accepted that document — NULL in the column, which migration 062
	// chose over a backfilled default because staff never accept on a buyer's
	// behalf and there is nothing truthful to backfill with.
	PolicyAcceptedAt sql.NullTime
	PolicyVersionID  string
	TermsAcceptedAt  sql.NullTime
	TermsVersionID   string
	// The two optional consents, "" where never answered. Unanswered is a
	// different fact from denied and travels as the different fact it is.
	MarketingConsent  string
	NetworkingConsent string
}

// ErrConsentSubjectNotFound reports that no Customer holds this id. It is
// SEPARATE FROM ErrCustomerNotFound above, which that file documents as a bug
// — every capture surface resolves its Customer first — whereas this one is the
// ordinary outcome of an operator following a stale link, and the surface maps
// it to a 404.
var ErrConsentSubjectNotFound = errors.New("consent subject not found")

// CustomerSubject reads one Customer by id: who they are, and where they stand.
//
// BY ID AND ONLY BY ID. There is no by-address counterpart here and there must
// not be one: the browsers' search absorbed the one-step lookup an address used
// to be for (#565), and it does so from a POSTed body.
func (r *Repository) CustomerSubject(ctx context.Context, customerID string) (CustomerSubjectRow, error) {
	var (
		row        CustomerSubjectRow
		policyID   sql.NullString
		termsID    sql.NullString
		marketing  sql.NullString
		networking sql.NullString
	)
	err := r.db.Pool.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name,
		       policy_accepted_at, policy_version_id,
		       terms_accepted_at, terms_version_id,
		       marketing_consent, networking_consent
		FROM customers
		WHERE id = $1
	`, customerID).Scan(
		&row.ID, &row.Email, &row.FirstName, &row.LastName,
		&row.PolicyAcceptedAt, &policyID,
		&row.TermsAcceptedAt, &termsID,
		&marketing, &networking,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerSubjectRow{}, ErrConsentSubjectNotFound
	}
	if err != nil {
		return CustomerSubjectRow{}, fmt.Errorf("customer subject: %w", err)
	}
	row.PolicyVersionID = policyID.String
	row.TermsVersionID = termsID.String
	row.MarketingConsent = marketing.String
	row.NetworkingConsent = networking.String
	return row, nil
}

// CustomerIDByEmail resolves an address to a Customer id, or "" when nobody
// holds it.
//
// IT EXISTS FOR THE CROSS-LINK AND FOR NOTHING ELSE (#566). Where one human
// being is both a Customer and somebody who signs into the Staff platform,
// their two records are linked server-side — and the only thing the two
// populations share is the address, which is why the join happens HERE, inside
// the server, and never in a URL. The address arrives from a staff record the
// caller has already resolved; it is never typed, never in a path and never in
// a query string.
//
// It answers "" rather than an error for an unknown address: most staff are not
// Customers, and the absence of a cross-link is an ordinary answer rather than
// a failure.
func (r *Repository) CustomerIDByEmail(ctx context.Context, email string) (string, error) {
	var id string
	err := r.db.Pool.QueryRowContext(ctx, `SELECT id FROM customers WHERE email = $1`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("customer id by email: %w", err)
	}
	return id, nil
}
