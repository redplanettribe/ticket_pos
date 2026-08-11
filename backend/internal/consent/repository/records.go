package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent"
)

// Record is one row of `consent_records`: the immutable evidence of one capture
// act, exactly as it is stored.
//
// THERE IS NO UPDATE AND NO DELETE FOR THIS TYPE anywhere in this package, and
// that absence is the enforcement of the append-only rule migration 061 states.
// It is a shape rather than a trigger deliberately — a trigger would also
// refuse the one legitimate later write, the double opt-in's `confirmed_at`
// stamp — so the guarantee lives where the writes are: this package offers
// Append and reads, and nothing else.
type Record struct {
	CustomerID      string
	Email           string
	Channel         consent.Channel
	CapturedAt      time.Time
	PolicyVersionID string
	// The three answers. Invalid (SQL NULL) means the box was not shown on this
	// surface, which is not a No.
	PolicyAcceptance  sql.NullBool
	MarketingConsent  sql.NullBool
	NetworkingConsent sql.NullBool
	EmailProven       bool
	// The prueba técnica. Invalid where the surface collected nothing, so that
	// "not collected" stays distinguishable from "collected as blank".
	IP        sql.NullString
	UserAgent sql.NullString
	SessionID sql.NullString
	OriginURL sql.NullString
}

// StateWrite is what one capture act makes true of the Customer, expressed as
// instructions rather than as a target row.
//
// It is separated from Record because the two answer different questions and
// only one of them is negotiable: the Record is a transcript and is written
// verbatim, while what a transcript CHANGES depends on rules — whether the
// email was proven, what the current state already is — that belong to the
// service. This type is the narrow vocabulary those rules speak in, so that the
// SQL below stays a mechanism and the policy stays readable in one place
// (service/capture.go).
type StateWrite struct {
	// AcceptPolicy stamps policy_accepted_at and policy_version_id. False leaves
	// both exactly as they were: a capture where the required box was not shown
	// must not clear a standing acceptance.
	AcceptPolicy bool
	// Marketing and Networking are the two optional consents' writes.
	Marketing  ConsentStateWrite
	Networking ConsentStateWrite
	// DigestEnabled is the lockstep value for customers.digest_enabled, nil to
	// leave it alone (ADR 0034). It is passed rather than derived here because
	// the rule that derives it — granted on, denied off, Pending Confirmation
	// neither — is a domain rule and this is a data access layer.
	DigestEnabled *bool
}

// ConsentStateWrite is one optional consent's instruction.
type ConsentStateWrite struct {
	// State is the value to write. Empty means "leave this column alone", which
	// is what a surface that did not show the box passes.
	State consent.State
	// OnlyWhenUnanswered restricts the write to a column that is NULL or already
	// 'pending_confirmation' — that is, to a Customer whose owner has not
	// themselves answered.
	//
	// It is what makes ADR 0035's rule structural rather than remembered: a
	// guest's answer never overwrites a state written under a proven session. A
	// proven capture passes false and simply wins, which is also how a proven
	// owner supersedes a guest's Pending Confirmation.
	OnlyWhenUnanswered bool
}

// CustomerConsentState is the Customer's current consent state — what is true
// now, as against the log of what happened.
//
// Every field is nullable and every null means something different: no
// acceptance recorded at all, or an optional consent never answered. Read by
// primary key as part of deciding whether to gate a sign-in, which is why it is
// four columns off `customers` rather than an aggregate over consent_records.
type CustomerConsentState struct {
	PolicyAcceptedAt  sql.NullTime
	PolicyVersionID   sql.NullString
	MarketingConsent  consent.State
	NetworkingConsent consent.State
}

// ErrCustomerNotFound reports that the Customer a capture named does not exist.
// It is a bug rather than a user error — every surface resolves the Customer
// before it captures — so the service maps it to a plain failure.
var ErrCustomerNotFound = errors.New("customer not found")

// ConsentState reads one Customer's current consent state.
func (r *Repository) ConsentState(ctx context.Context, customerID string) (CustomerConsentState, error) {
	const query = `
		SELECT policy_accepted_at, policy_version_id, marketing_consent, networking_consent
		FROM customers
		WHERE id = $1
	`

	var (
		state      CustomerConsentState
		marketing  sql.NullString
		networking sql.NullString
	)
	err := r.db.Pool.QueryRowContext(ctx, query, customerID).Scan(
		&state.PolicyAcceptedAt,
		&state.PolicyVersionID,
		&marketing,
		&networking,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return CustomerConsentState{}, ErrCustomerNotFound
	}
	if err != nil {
		return CustomerConsentState{}, fmt.Errorf("consent state: %w", err)
	}
	state.MarketingConsent = consent.State(marketing.String)
	state.NetworkingConsent = consent.State(networking.String)
	return state, nil
}

// Append writes the Consent Record and applies the state it makes true, in ONE
// TRANSACTION, and returns the record's id and the Customer's resulting state.
//
// The transaction is the point of this method existing at all. The log and the
// state are two halves of one act: a record with no state change would gate a
// person who had just accepted, and a state change with no record is precisely
// the unevidenced consent this feature exists to abolish. Neither is a state
// the platform may be found in, so they commit together or not at all.
//
// The single UPDATE ... RETURNING then does something the two-statement version
// cannot: it applies every conditional write and reads back the result under
// one row lock, so two capture acts racing on the same Customer — a checkout
// committing while its buyer answers in another tab — cannot interleave into a
// state neither of them asked for, and neither can report a state that was
// never true.
func (r *Repository) Append(ctx context.Context, record Record, state StateWrite) (string, CustomerConsentState, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return "", CustomerConsentState{}, fmt.Errorf("begin consent capture: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var recordID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO consent_records (
			customer_id, email, channel, captured_at, policy_version_id,
			policy_acceptance, marketing_consent, networking_consent,
			email_proven, ip, user_agent, session_id, origin_url
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id
	`,
		record.CustomerID, record.Email, string(record.Channel), record.CapturedAt, record.PolicyVersionID,
		record.PolicyAcceptance, record.MarketingConsent, record.NetworkingConsent,
		record.EmailProven, record.IP, record.UserAgent, record.SessionID, record.OriginURL,
	).Scan(&recordID)
	if err != nil {
		return "", CustomerConsentState{}, fmt.Errorf("append consent record: %w", err)
	}

	// Every column here is written by a CASE that can decide to leave it exactly
	// as it was, so this one statement serves the surface that showed all three
	// boxes and the surface that showed one. The alternative — building the SET
	// list from whichever fields were present — makes the SQL a string that
	// varies by caller, and the rules below unreadable as a set.
	//
	// The OnlyWhenUnanswered guard reads "the stored value is NULL or
	// 'pending_confirmation'", which is exactly "the owner of this address has
	// not answered this box themselves" (ADR 0035).
	var (
		resulting  CustomerConsentState
		marketing  sql.NullString
		networking sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		UPDATE customers SET
			policy_accepted_at = CASE WHEN $2 THEN $3 ELSE policy_accepted_at END,
			policy_version_id  = CASE WHEN $2 THEN $4::uuid ELSE policy_version_id END,
			marketing_consent = CASE
				WHEN $5::text = '' THEN marketing_consent
				WHEN $6 AND marketing_consent IS NOT NULL AND marketing_consent <> 'pending_confirmation' THEN marketing_consent
				ELSE $5::text
			END,
			networking_consent = CASE
				WHEN $7::text = '' THEN networking_consent
				WHEN $8 AND networking_consent IS NOT NULL AND networking_consent <> 'pending_confirmation' THEN networking_consent
				ELSE $7::text
			END,
			digest_enabled = COALESCE($9::boolean, digest_enabled)
		WHERE id = $1
		RETURNING policy_accepted_at, policy_version_id, marketing_consent, networking_consent
	`,
		record.CustomerID,
		state.AcceptPolicy, record.CapturedAt, record.PolicyVersionID,
		string(state.Marketing.State), state.Marketing.OnlyWhenUnanswered,
		string(state.Networking.State), state.Networking.OnlyWhenUnanswered,
		state.DigestEnabled,
	).Scan(&resulting.PolicyAcceptedAt, &resulting.PolicyVersionID, &marketing, &networking)
	if errors.Is(err, sql.ErrNoRows) {
		return "", CustomerConsentState{}, ErrCustomerNotFound
	}
	if err != nil {
		return "", CustomerConsentState{}, fmt.Errorf("apply consent state: %w", err)
	}
	resulting.MarketingConsent = consent.State(marketing.String)
	resulting.NetworkingConsent = consent.State(networking.String)

	if err := tx.Commit(); err != nil {
		return "", CustomerConsentState{}, fmt.Errorf("commit consent capture: %w", err)
	}
	return recordID, resulting, nil
}
