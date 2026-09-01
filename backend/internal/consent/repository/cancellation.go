package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Cancelling a scheduled edition (#564, migration 113).
//
// THE ONE UPDATE THIS CODEBASE ISSUES AGAINST EITHER VERSION TABLE, and it must
// stay the only one. publish.go's rule — nothing is ever mutated — is a rule
// about the EDITION: its label, its lineage, its effective date, its artifact
// rows and its content hash are what acceptances fingerprint, and none of them
// is touched here. What is written is a MARK ABOUT the row, once, on a row that
// by construction no acceptance can name: an edition whose day has not come has
// been on no screen, so nobody was shown it and nobody can have accepted it.
//
// THE REFUSAL IS IN THE WHERE CLAUSE. "The effective date must not have passed"
// is checked by `effective_date > CURRENT_DATE` inside the statement that does
// the writing, against the database's own day — the same predicate that decides
// which edition is current, read at the same instant, so there is no window
// between deciding and acting in which midnight can fall. Asking Go's clock
// would be a second opinion about a question this feature has exactly one
// answer to.

// ErrLegalEditionNotFound reports that the version id names no edition of that
// document. The two documents are separate tables (ADR 0066), so a Terms id
// offered on the Policy's path is not found — the same answer as an id nothing
// ever issued, and deliberately: one document's surface must not confirm the
// existence of the other's rows.
var ErrLegalEditionNotFound = errors.New("legal edition not found")

// ErrLegalEditionAlreadyEffective reports that the edition's day has arrived, so
// there is nothing left to cancel: people are being held to those words now, and
// withdrawing them is a publication of its own rather than an undo.
var ErrLegalEditionAlreadyEffective = errors.New("legal edition already effective")

// CancelledLegalEdition is what the caller needs to say what happened: which
// edition it was, when it was going to take effect, and whether this call is
// what withdrew it.
type CancelledLegalEdition struct {
	Label         string
	EffectiveDate time.Time
	// AlreadyCancelled is true when the edition was withdrawn before this call.
	// Cancelling twice is a SUCCESS — what the caller asked for is exactly what
	// they now have, the same answer discarding a draft that does not exist
	// gives — and the FIRST cancellation's provenance is kept, because it is the
	// one that describes the act.
	AlreadyCancelled bool
}

// CancelLegalEdition withdraws a scheduled edition, leaving the row and every
// word of it in place.
//
// Refuses with ErrLegalEditionAlreadyEffective once the day has passed, and with
// ErrLegalEditionNotFound when the id names no edition of this document.
func (r *Repository) CancelLegalEdition(
	ctx context.Context, document, versionID, by string, at time.Time,
) (CancelledLegalEdition, error) {
	versions, _, ok := legalTables(document)
	if !ok {
		return CancelledLegalEdition{}, fmt.Errorf("cancel legal edition: unknown document %q", document)
	}

	// The guarded write. Its WHERE clause IS the rule: still scheduled, still
	// live, and only then marked.
	update := `
		UPDATE ` + versions + `
		SET cancelled_by = $2, cancelled_at = $3
		WHERE id::text = $1
		  AND cancelled_at IS NULL
		  AND effective_date > CURRENT_DATE
		RETURNING label, effective_date
	`

	var cancelled CancelledLegalEdition
	err := r.db.Pool.QueryRowContext(ctx, update, versionID, by, at).
		Scan(&cancelled.Label, &cancelled.EffectiveDate)
	if err == nil {
		return cancelled, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CancelledLegalEdition{}, fmt.Errorf("cancel legal edition: %w", err)
	}

	// Nothing was written, and the row itself says which of the three reasons it
	// was. Read AFTER the attempt rather than before it, so the decision to
	// write is never taken on a state that has since moved.
	read := `
		SELECT label, effective_date, cancelled_at IS NOT NULL, effective_date <= CURRENT_DATE
		FROM ` + versions + `
		WHERE id::text = $1
	`
	var alreadyEffective bool
	err = r.db.Pool.QueryRowContext(ctx, read, versionID).
		Scan(&cancelled.Label, &cancelled.EffectiveDate, &cancelled.AlreadyCancelled, &alreadyEffective)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return CancelledLegalEdition{}, ErrLegalEditionNotFound
	case err != nil:
		return CancelledLegalEdition{}, fmt.Errorf("cancel legal edition: %w", err)
	case cancelled.AlreadyCancelled:
		return cancelled, nil
	case alreadyEffective:
		// The edition travels WITH the refusal, so the sentence on the screen can
		// name the day that passed rather than say only that one did.
		return cancelled, ErrLegalEditionAlreadyEffective
	default:
		// Neither cancelled nor effective, yet the guarded update matched
		// nothing: the row moved between the two statements. Refusing is the
		// honest answer, and the operator's retry will find the new state.
		return CancelledLegalEdition{}, fmt.Errorf("cancel legal edition %s: the edition moved while it was being cancelled", versionID)
	}
}

// A NOTE ON THE ID AND WHERE IT COMES FROM. The version id is a client's here,
// unlike everywhere else in this module — the banner names one and the button
// sends it back. That is safe because the id is matched against ONE document's
// table and the act it authorizes is bounded to rows nobody has been shown: the
// worst a guessed uuid can do is withdraw an edition its guesser could already
// have withdrawn from the screen in front of them, since the whole surface is
// the operator allowlist's and there is one operator behind it.
//
// The comparison is `id::text = $1` and not `id = $1`, which is the opposite
// direction from browsing.go's `= ANY($1::uuid[])` and for a reason that only
// applies to a single client-supplied id: casting the PARAMETER makes a
// malformed uuid a database ERROR, answered 500, when what it plainly is is an
// edition that does not exist. Casting the COLUMN makes it no rows, which is the
// 404 it deserves. The forfeited index costs nothing on a table that holds a
// handful of rows for the lifetime of the product (migration 060).
