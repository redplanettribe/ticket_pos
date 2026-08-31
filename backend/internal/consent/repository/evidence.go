package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// THE TEXT OF NAMED EDITIONS, AND THE RECORD OF A HANDOVER (#568, parent #556,
// ADR 0067).
//
// Two reads and one write, all three in service of the Consent Evidence Pack.
//
// WHY THIS IS NOT `CurrentPolicyEdition` WITH A PARAMETER. That read answers
// "what is in force today", which is a question about the calendar, and its
// `effective_date <= CURRENT_DATE` predicate is load-bearing: it is what makes a
// scheduled edition become current at midnight with nothing firing. An Evidence
// Pack asks the opposite question — "what were these particular acts captured
// against" — and the answer must include editions that are superseded,
// scheduled or cancelled, because an act naming one of those is still an act
// somebody performed. Bending the current-edition selector to serve both would
// put a WHERE clause in front of somebody's evidence.

// LegalEditionText is one published edition and every word of it, read by id.
//
// It carries the LINEAGE rather than a label, because `published_as` is
// `revision = 0` and nothing else (legal.Lineage.Gating), and because the label
// must be produced by the one function that ever produces one — a second
// labeller here would be a second thing that could disagree with the Legal
// Center about what an edition is called.
type LegalEditionText struct {
	ID            string
	Lineage       legal.Lineage
	EffectiveDate time.Time
	// ContentHash is the fingerprint the row carries and every acceptance
	// points at. Read from the ROW and never recomputed here: the pack lays out
	// the preimage so a reader can recompute it themselves, and a hash this
	// code derived would prove only that this code is self-consistent.
	ContentHash string
	Artifacts   []legal.Artifact
}

// PolicyEditionTexts reads the named Policy editions with all of their text.
//
// NO DATE PREDICATE AND NO CANCELLATION PREDICATE, deliberately — see above. An
// id that names no row is simply absent from the result: the caller reports the
// act with its edition id and no text beside it rather than refusing to produce
// somebody's record over a referential problem in this platform's own tables.
//
// A LEFT JOIN, so an edition with no artifacts is a version with empty text
// rather than a missing edition; the two are different failures.
func (r *Repository) PolicyEditionTexts(ctx context.Context, ids []string) ([]LegalEditionText, error) {
	return r.editionTexts(ctx, `
		SELECT v.id, v.generation, v.revision, v.effective_date, v.content_hash,
		       a.locale, a.slug, a.ordinal, a.body
		FROM policy_versions v
		LEFT JOIN policy_version_artifacts a ON a.version_id = v.id
		WHERE v.id = ANY($1::uuid[])
		ORDER BY v.effective_date, v.id, a.locale COLLATE "C", a.ordinal
	`, ids, "policy edition texts")
}

// TermsEditionTexts is PolicyEditionTexts over the parallel table (ADR 0066).
// The query is a literal at each call site rather than a table name
// interpolated below, so neither table can be reached by passing a string.
func (r *Repository) TermsEditionTexts(ctx context.Context, ids []string) ([]LegalEditionText, error) {
	return r.editionTexts(ctx, `
		SELECT v.id, v.generation, v.revision, v.effective_date, v.content_hash,
		       a.locale, a.slug, a.ordinal, a.body
		FROM terms_versions v
		LEFT JOIN terms_version_artifacts a ON a.version_id = v.id
		WHERE v.id = ANY($1::uuid[])
		ORDER BY v.effective_date, v.id, a.locale COLLATE "C", a.ordinal
	`, ids, "terms edition texts")
}

func (r *Repository) editionTexts(ctx context.Context, query string, ids []string, what string) ([]LegalEditionText, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Pool.QueryContext(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()

	var (
		editions []LegalEditionText
		current  *LegalEditionText
	)
	for rows.Next() {
		var (
			id            string
			generation    int
			revision      int
			effectiveDate time.Time
			contentHash   string
			locale        sql.NullString
			slug          sql.NullString
			ordinal       sql.NullInt64
			body          sql.NullString
		)
		if err := rows.Scan(&id, &generation, &revision, &effectiveDate, &contentHash,
			&locale, &slug, &ordinal, &body); err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		if current == nil || current.ID != id {
			editions = append(editions, LegalEditionText{
				ID:            id,
				Lineage:       legal.Lineage{Generation: generation, Revision: revision},
				EffectiveDate: effectiveDate,
				ContentHash:   contentHash,
			})
			current = &editions[len(editions)-1]
		}
		if !locale.Valid {
			continue
		}
		current.Artifacts = append(current.Artifacts, legal.Artifact{
			Locale:  platform.Locale(locale.String),
			Slug:    slug.String,
			Ordinal: int(ordinal.Int64),
			Body:    body.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return editions, nil
}

// EvidencePackHandover is what survives a pack: its fingerprint, its size, and
// the ids of the acts it disclosed (migration 118).
type EvidencePackHandover struct {
	SHA256    string
	SizeBytes int
	Acts      []EvidencePackAct
}

// EvidencePackAct is one act a pack covered.
type EvidencePackAct struct {
	// Kind is `consent_record` or `staff_terms_acceptance`.
	Kind string
	ID   string
}

// RecordEvidencePack writes the handover row and its coverage.
//
// THE PACK ITSELF IS NOT STORED AND MUST NEVER BE. A pack is assembled because
// somebody asked what the platform holds about them; keeping a copy would answer
// a privacy request by making a second copy of the most sensitive data on the
// platform, in a place no erasure reaches. What is kept is enough to prove a
// handover happened and what it said, because the pack is DETERMINISTIC: the
// same acts regenerate the same bytes, so a file produced later either hashes to
// the stored value or is not the file that was sent.
//
// ONE TRANSACTION, so a pack is never half-recorded: a handover row with no
// coverage would claim something was sent and be unable to say what.
//
// It returns the new row's id so the caller can hand it on. It writes NO ACTOR:
// who exported is #569's `consent_access_log`, which records the act with this
// same `pack_sha256`, and duplicating it here would be two facts that can
// disagree.
func (r *Repository) RecordEvidencePack(ctx context.Context, handover EvidencePackHandover) (string, error) {
	tx, err := r.db.Pool.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("record evidence pack: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO consent_evidence_packs (pack_sha256, size_bytes)
		VALUES ($1, $2)
		RETURNING id
	`, handover.SHA256, handover.SizeBytes).Scan(&id); err != nil {
		return "", fmt.Errorf("record evidence pack: %w", err)
	}

	for _, act := range handover.Acts {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO consent_evidence_pack_acts (pack_id, act_kind, act_id)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, id, act.Kind, act.ID); err != nil {
			return "", fmt.Errorf("record evidence pack acts: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("record evidence pack: %w", err)
	}
	return id, nil
}
