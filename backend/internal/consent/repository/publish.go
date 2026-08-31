package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
)

// Publishing an edition, and counting who it re-gates (#563, migration 112).
//
// PUBLISHING IS AN INSERT AND A ROW COPY. NOTHING IS EVER MUTATED — not here,
// not anywhere. A correction is a NEW ROW with a new id, a new label and its own
// artifact rows, and the edition it corrects keeps its bytes exactly as the
// acceptances that fingerprint them expect. Neither artifact table is ever
// UPDATEd, and no column of a version row that an acceptance depends on — its
// id, its label, its lineage, its effective date, its content hash — is either:
// the whole value of a content hash is that the text it covers cannot have moved
// since somebody accepted it.
//
// THE ONE EXCEPTION IS THE CANCELLATION MARK (#564, cancellation.go), and it is
// an exception to the letter and not to the rule. It writes `cancelled_by` and
// `cancelled_at` and nothing else, on a row whose day has not come — an edition
// nobody has been shown and therefore nobody can have accepted — so no
// fingerprint moves under anybody's evidence.
//
// THE DRAFT GOES WITH THE PUBLICATION, in the same transaction. What the draft
// said is now what is published, so leaving it behind would leave the editor
// showing a `stored: true` draft identical to the current edition, carrying
// review state about an act that is over. Discarding it puts the document back
// into the one state the editor has for "nothing in progress" (#561: "discard"
// and "never started" are one screen), and the operator loses nothing, because
// every word of it is now a published row.

// PublishLegalEditionInput is one publication, fully decided: the service has
// already chosen the lineage, resolved the effective date, computed the
// fingerprint and counted the headcount. This layer writes it down.
type PublishLegalEditionInput struct {
	// Document is "policy" or "terms". Never interpolated: the closed switch
	// below maps it to one of two pairs of table names written in this file.
	Document string
	// Label is the RENDERING of the lineage (legal.Lineage.Label) and is never
	// typed by a human. Migration 110's CHECK refuses anything else.
	Label      string
	Generation int
	Revision   int
	// EffectiveDate is the day the edition takes effect — date-only, because an
	// effective date is a legal fact stated on the document itself.
	EffectiveDate time.Time
	// ContentHash is legal.ContentHash over exactly the Artifacts below. The two
	// are written in one transaction so no edition can exist whose fingerprint
	// covers different bytes from the ones stored beside it.
	ContentHash string
	Artifacts   []legal.Artifact

	// The provenance (migration 112), whole or not at all.
	PublishedBy string
	PublishedAt time.Time
	DiffSummary string
	// CorrectionReason is empty on a gating edition, where the edition is its
	// own justification. Migration 112 refuses a reason on one, and refuses a
	// correction without one.
	CorrectionReason string
	// RegatedHeadcount is the number that was ON THE BUTTON. Zero on a
	// correction, by constraint as well as by construction.
	RegatedHeadcount int
}

// PublishLegalEdition writes one publication: the version row, its artifact
// rows, and the discard of the draft that became it — one transaction, so a
// half-published edition is not a state anything can observe.
//
// Returns the new version's id, which is what every acceptance from here on will
// name.
func (r *Repository) PublishLegalEdition(ctx context.Context, in PublishLegalEditionInput) (string, error) {
	versions, artifacts, ok := legalTables(in.Document)
	if !ok {
		return "", fmt.Errorf("publish legal edition: unknown document %q", in.Document)
	}

	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("publish legal edition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// NULL and not "" for the two columns migration 112 allows to be absent: the
	// CHECKs are written against NULL, and an empty string would be the platform
	// asserting a blank reason rather than no reason.
	var reason any
	if in.CorrectionReason != "" {
		reason = in.CorrectionReason
	}

	var versionID string
	insert := `
		INSERT INTO ` + versions + `
			(label, generation, revision, effective_date, content_hash,
			 published_by, published_at, publish_diff_summary, correction_reason, regated_headcount)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`
	if err := tx.QueryRowContext(ctx, insert,
		in.Label, in.Generation, in.Revision, in.EffectiveDate, in.ContentHash,
		in.PublishedBy, in.PublishedAt, in.DiffSummary, reason, in.RegatedHeadcount,
	).Scan(&versionID); err != nil {
		return "", fmt.Errorf("publish legal edition: %w", err)
	}

	// The row copy. The ordinals are the draft's own, carried through untouched:
	// they are the fingerprint preimage's order, and renumbering them here would
	// publish an edition hashed differently from the one that was previewed.
	rows := `INSERT INTO ` + artifacts + ` (version_id, locale, slug, ordinal, body) VALUES ($1, $2, $3, $4, $5)`
	for _, artifact := range in.Artifacts {
		if _, err := tx.ExecContext(ctx, rows,
			versionID, string(artifact.Locale), artifact.Slug, artifact.Ordinal, artifact.Body,
		); err != nil {
			return "", fmt.Errorf("publish legal artifact %s/%s: %w", artifact.Locale, artifact.Slug, err)
		}
	}

	// The draft became this edition. Its cells, its language set, its previews
	// and its seen-diff go with it by cascade (migrations 111, 117).
	if _, err := tx.ExecContext(ctx, `DELETE FROM legal_drafts WHERE document = $1`, in.Document); err != nil {
		return "", fmt.Errorf("publish legal edition: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("publish legal edition: %w", err)
	}
	return versionID, nil
}

// legalTables maps a document to its two table names.
//
// A CLOSED SWITCH RETURNING LITERALS, exactly as customerVersionColumn is: the
// only interpolation in this file is these four strings, and no caller input can
// reach the statement.
func legalTables(document string) (versions, artifacts string, ok bool) {
	switch document {
	case "policy":
		return "policy_versions", "policy_version_artifacts", true
	case "terms":
		return "terms_versions", "terms_version_artifacts", true
	default:
		return "", "", false
	}
}

// RegatedCustomerCount is how many Customers a gating publication would move out
// of Current: those holding an acceptance that clears the gate TODAY and would
// not clear it once the new edition takes effect.
//
// WHY THIS IS THE "CURRENT" COUNT AND NOT THE OUTSTANDING ONE. A gating edition
// supersedes everything below it, so after it arrives the satisfying set is that
// edition and whatever sits above it — and nobody holding one of today's
// editions is in it. Everybody Current today therefore becomes Outstanding, and
// that set is exactly what "re-gate" names.
//
// PEOPLE WHO HAVE NEVER ACCEPTED ANYTHING ARE NOT COUNTED, and the omission is
// the whole reason this number is worth putting on a button. On the production
// copy about a third of Customers have never accepted anything (legal.Standing
// says why): a box-office sale or a Sale Import created their record and they
// have never acted on a Storefront surface. They owe the document already and
// this publication changes nothing about them — counting them would make the
// figure "roughly everybody", which is a number nobody reads twice.
//
// Nor are people ALREADY Outstanding: they are behind the gate now and will be
// behind it after, and a publication cannot re-gate somebody it did not move.
func (r *Repository) RegatedCustomerCount(ctx context.Context, document string, satisfying []string) (int, error) {
	column, ok := customerVersionColumn(document)
	if !ok {
		return 0, fmt.Errorf("regated customer count: unknown document %q", document)
	}

	// The `::uuid[]` cast is load-bearing for browsing.go's reason: the ids
	// travel from Go as strings, and without it a text array meets a uuid column
	// and the comparison is refused.
	query := `SELECT count(*) FROM customers WHERE ` + column + ` = ANY($1::uuid[])`

	var count int
	if err := r.db.Pool.QueryRowContext(ctx, query, satisfying).Scan(&count); err != nil {
		return 0, fmt.Errorf("regated customer count: %w", err)
	}
	return count, nil
}

// RegatedStaffCount is RegatedCustomerCount over the OTHER population that can
// owe an acceptance: the Staff platform, which has one gate and it is the Terms'
// (§3, ADR 0066).
//
// KEYED ON EMAIL, because the Staff platform's person key is the address a
// session names — a Platform Operator may hold no `members` row at all, and a
// person with three Organizations holds three (migration 107). DISTINCT for the
// same reason: three memberships are one human, and a headcount that counted
// them three times would be a headcount about rows.
//
// FORMER STAFF ARE EXCLUDED, by the join to `members ∪ platform_operators`. A
// leaver cannot sign in, so the gate they would meet is one they will never
// reach; counting them would inflate the consequence with people nobody can
// chase, which is the same argument legal.StandingFormer makes for keeping them
// off the outstanding list.
func (r *Repository) RegatedStaffCount(ctx context.Context, satisfying []string) (int, error) {
	const query = `
		SELECT count(DISTINCT a.email)
		FROM staff_terms_acceptances a
		WHERE a.terms_version_id = ANY($1::uuid[])
		  AND (EXISTS (SELECT 1 FROM members m WHERE m.email = a.email)
		    OR EXISTS (SELECT 1 FROM platform_operators o WHERE o.email = a.email))
	`

	var count int
	if err := r.db.Pool.QueryRowContext(ctx, query, satisfying).Scan(&count); err != nil {
		return 0, fmt.Errorf("regated staff count: %w", err)
	}
	return count, nil
}
