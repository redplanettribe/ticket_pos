// Package evidence builds the Consent Evidence Pack: one file that answers a
// data subject or a regulator (#568, parent #556, ADR 0067).
//
// THE PACK IS GENERATED ON DEMAND AND NEVER STORED, the RIDE's rule (ADR 0062)
// applied to something far more sensitive. It is assembled precisely because
// somebody asked what the platform holds about them, so keeping a copy would
// answer a privacy request by making a second, unindexed copy of the most
// sensitive data on the platform. Only the pack's SHA-256 and the ids of the
// acts it covered survive it (migration 118), which is enough to prove a
// handover happened and what it said, because the bytes are reproducible.
//
// THE ZIP IS DETERMINISTIC, and this package exists mostly to keep it that way.
// Two packs generated over an identical record on different days are BYTE-FOR-
// BYTE IDENTICAL: fixed entry ordering, one fixed entry timestamp, fixed PDF
// metadata, and — the rule that actually bites — NOT ONE TIME-VARYING FACT
// ANYWHERE IN THE CONTENTS. That is why an edition carries `published_as`,
// which was fixed when it was published, and never its standing, which changes
// at midnight when the database's own day moves (legal.Satisfying). A generation
// date would do the same damage and is absent for the same reason; it lives in
// the filename, which is a label on the document rather than the document.
//
// Determinism is not decoration. It is what lets the platform store a hash
// instead of a file: a pack produced years later either reproduces the stored
// hash or is not the file that was sent.
//
// `record.json` IS THE RECORD; `evidence.pdf` IS ITS READING. The JSON carries
// the acts exactly as the per-subject record screen carries them (#566) — the
// same types, so an export cannot tell a different story from the screen it was
// exported from — and the PDF is a human's way through the same facts, in ADR
// 0062's shape. Where the two could disagree they are built from one value.
//
// THE TEXTS ARE RAW `.md` FILES, deduplicated BY EDITION rather than by act, and
// every accepted edition rides in EVERY LOCALE IT PUBLISHES. Both halves are
// forced by the fingerprint: `legal.ContentHash` spans all of an edition's
// languages at once, so a single-locale pack could not verify its own hash and
// would be a document asserting a number nobody could check. Raw files rather
// than JSON strings so that verification is a shell script over bytes and the
// locale-and-ordinal ordering is visible in a listing.
//
// THE HMAC DIGEST APPEARS NOWHERE IN THE CONTENTS (#548). A key rotation must
// not orphan a document whose purpose is to stay meaningful for years, and the
// filename resolves that same conflict a different way — see Filename and ADR
// 0067.
//
// It imports `consent/service` for the act type and defines its own small
// mirrors of everything else. It knows nothing of identity: the staff half
// arrives as plain values from the one layer above that is allowed to hold both
// populations at once (ADR 0015).
package evidence

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The three names inside the pack. Fixed strings rather than anything derived,
// because a verification script written against one pack has to work on the
// next one.
const (
	RecordEntry   = "record.json"
	EvidenceEntry = "evidence.pdf"
	TextsPrefix   = "texts/"
)

// ContentType is what the pack is served as.
const ContentType = "application/zip"

// FormatVersion names the pack's own layout, so a reader years from now can
// tell which rules were in force when the file was written. It is bumped when
// the SHAPE changes, never when the data does.
const FormatVersion = "consent-evidence-pack.v1"

// entryModified is the one timestamp every entry in the archive carries.
//
// A CONSTANT, AND THAT IS THE POINT. ZIP records a modification time per entry;
// left to the clock it would put the generation instant into the bytes and make
// two packs over one record differ by the second they were built, which is
// exactly the property the whole feature is arranged to deny. The value itself
// is arbitrary and deliberately not a real date anybody could mistake for a
// fact about the record: 1980-01-01 is the earliest instant the MS-DOS time
// field can express, so it is the one value that cannot be read as a claim.
//
// UTC, because zip.FileHeader writes an extra extended-timestamp field for a
// non-UTC location and the bytes would then depend on the server's zone.
var entryModified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// Input is everything a pack is built from: one address, the two populations
// that may answer to it, and the text of every edition either of them names.
//
// IT IS ASSEMBLED BY THE CALLER AND VALIDATED HERE ONLY FOR SELF-CONSISTENCY.
// This package does no reads: it is a pure function from a record to bytes, so
// the determinism it promises can be tested without a database and the acts it
// prints are provably the acts the record screen served.
type Input struct {
	// Email is the address the pack is about, normalised. It is the ONLY thing
	// the two populations share and the only key that spans both — there is no
	// row anywhere saying a Customer and a staff person are one human being.
	Email string
	// Customer is the Customer half, nil where no Customer holds this address.
	Customer *Subject
	// Acts is that Customer's complete consent history, newest first, exactly
	// as #566's record read produced it. Empty where there is none.
	Acts []consentsvc.ConsentActItem
	// Staff is the staff half, nil where the address is on no staff row and
	// holds no acceptance.
	Staff *StaffSubject
	// Editions is the text of every edition named by any act on either side,
	// deduplicated by (document, id). An edition an act names and this slice
	// omits is a build error rather than a silently thinner pack: the whole
	// value of the file is that the act resolves to the exact bytes.
	Editions []Edition
}

// Subject is who the Customer half is about and what is true of them now.
//
// NO STANDING FIELD, unlike the record screen's CustomerLegalRecordItem which
// this otherwise mirrors. Standing is membership of the satisfying set, and
// which editions are in that set changes at midnight with nothing firing — so a
// pack carrying it would disagree with itself across a night and could not be
// verified against a stored hash. What the pack states instead is what was
// ACCEPTED: the date and the exact edition, which no later publication moves.
type Subject struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	// The two gates, as acceptances rather than as standings.
	Policy Acceptance `json:"policy"`
	Terms  Acceptance `json:"terms"`
	// The two optional consents as they stand, null where never answered.
	// Unanswered is a different fact from denied and stays different here: a
	// document handed to a regulator must not show a refusal nobody made.
	MarketingConsent  *string `json:"marketing_consent"`
	NetworkingConsent *string `json:"networking_consent"`
}

// Acceptance is the last acceptance of one document: when, and of what.
type Acceptance struct {
	AcceptedAt *time.Time                  `json:"accepted_at"`
	Edition    *consentsvc.LegalEditionRef `json:"edition"`
}

// StaffSubject is the staff half: the acceptances one address holds on the
// Staff platform.
//
// NO STANDING HERE EITHER, for Subject's reason, and no `former`: whether
// somebody is still on a membership table today is a fact about the present,
// and this document is about acts.
type StaffSubject struct {
	// Acceptances is the COMPLETE history, newest first, every capacity. It is
	// unpaged upstream because a person holds at most one row per edition per
	// capacity.
	Acceptances []StaffAcceptance `json:"acceptances"`
}

// StaffAcceptance is one Terms Acceptance as the pack states it — identity's
// row, carried as plain values so this package never learns what a Membership
// is (ADR 0015).
type StaffAcceptance struct {
	ID string `json:"id"`
	// TermsEdition names the exact edition, id and label together, so the act
	// resolves to bytes in `texts/`.
	TermsEdition consentsvc.LegalEditionRef `json:"terms_edition"`
	// Capacity is what the person accepted AS. Carried rather than assumed: an
	// acceptance in some other capacity is real evidence of a real act, and a
	// pack that dropped it would be hiding one.
	Capacity   string    `json:"capacity"`
	AcceptedAt time.Time `json:"accepted_at"`
	// The technical proof, null where the surface collected nothing — which is
	// not a blank it collected, all the way into the file.
	IP        *string `json:"ip"`
	UserAgent *string `json:"user_agent"`
	SessionID *string `json:"session_id"`
	OriginURL *string `json:"origin_url"`
	// PresentedLocale is the language of the acceptance label actually served
	// (#567), null on every row written before migration 115.
	PresentedLocale *string `json:"presented_locale"`
}

// Edition is one published edition and every word of it, in every language it
// publishes.
type Edition struct {
	// Document is `policy` or `terms` — the two lineages, which version
	// independently and never re-gate each other (ADR 0066).
	Document string
	// ID is the version row's id: what an act stores.
	ID string
	// Label is legal.Lineage.Label() — "2", "1.1" — produced by the ONE
	// function that ever produces one, so the pack cannot name an edition
	// differently from the Legal Center that published it.
	Label string
	// Gating is whether publishing this edition re-gated everybody, which is
	// `revision = 0` and nothing else (legal.Lineage.Gating). It is rendered as
	// `published_as`.
	//
	// THIS IS THE ONE PROPERTY OF AN EDITION THE PACK MAY CARRY, because it was
	// fixed at publication and no later act can change it. "Does this edition
	// currently clear the gate" is the fact that MUST NOT be here.
	Gating bool
	// EffectiveDate is the day the edition took effect: a legal fact stated on
	// the document itself.
	EffectiveDate time.Time
	// ContentHash is the fingerprint the version row carries and every
	// acceptance points at. The pack restates the rule that produces it and
	// lays out its preimage, so the file can verify this number itself.
	ContentHash string
	// Artifacts is every stored artifact, in every language.
	Artifacts []legal.Artifact
}

// Pack is the finished file and the two facts that outlive it.
type Pack struct {
	// Filename is `consent-evidence-<pack-sha256[:16]>-<YYYY-MM-DD>.zip`.
	Filename string
	// ContentType is always ContentType.
	ContentType string
	// Body is the ZIP.
	Body []byte
	// SHA256 is the hex SHA-256 of Body — persisted (migration 118), offered at
	// this seam so #569's access log can record the same value without
	// recomputing it over a copy of the bytes.
	SHA256 string
	// CoveredActs is every act the pack disclosed, so the coverage rows can be
	// written without reopening the archive.
	CoveredActs []CoveredAct
}

// CoveredAct is one act the pack disclosed: which log it lives in, and its id.
type CoveredAct struct {
	// Kind is `consent_record` or `staff_terms_acceptance`, migration 118's
	// vocabulary.
	Kind string
	ID   string
}

// The two act kinds, matching migration 118's CHECK.
const (
	ActKindConsentRecord        = "consent_record"
	ActKindStaffTermsAcceptance = "staff_terms_acceptance"
)

// Build assembles the pack.
//
// GENERATED EVEN WHEN EMPTY, and this is a ruling rather than a tolerance
// (#568). An address the platform holds nothing about produces a complete,
// well-formed, signable file saying so — "we hold nothing about this person" is
// an answer somebody is entitled to receive as a document, and an error would
// leave an operator to write that sentence themselves in an email.
//
// `generatedOn` is used for the FILENAME ONLY and touches no byte of the
// archive. It is passed rather than read from a clock here so a caller can
// stamp it from the same instant it logs the export.
func Build(in Input, generatedOn time.Time) (Pack, error) {
	texts, editionIndex, err := buildTexts(in.Editions)
	if err != nil {
		return Pack{}, err
	}
	record, err := buildRecord(in, editionIndex)
	if err != nil {
		return Pack{}, err
	}
	recordJSON, err := marshalRecord(record)
	if err != nil {
		return Pack{}, err
	}
	pdf, err := renderEvidencePDF(record)
	if err != nil {
		return Pack{}, err
	}

	// FIXED ENTRY ORDERING: the record, its reading, then the texts by path.
	// The two documents come first because they are what a reader opens, and
	// the texts sort into a tree a listing makes sense of.
	entries := make([]entry, 0, len(texts)+2)
	entries = append(entries, entry{Name: RecordEntry, Body: recordJSON})
	entries = append(entries, entry{Name: EvidenceEntry, Body: pdf})
	entries = append(entries, texts...)

	body, err := writeZip(entries)
	if err != nil {
		return Pack{}, err
	}

	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	return Pack{
		Filename:    Filename(digest, generatedOn),
		ContentType: ContentType,
		Body:        body,
		SHA256:      digest,
		CoveredActs: coveredActs(in),
	}, nil
}

// FilenameKeyLength is how much of the pack's own hash names it: 16 hex
// characters, 64 bits. Long enough that two packs in one folder will not
// collide, short enough to read out over a telephone.
const FilenameKeyLength = 16

// Filename names the pack after its own SHA-256 and the day it was generated.
//
// THIS SETTLES #546 AGAINST #548 (ADR 0067). #546 keyed the filename on the
// staff HMAC digest, specifically so that it would never be the email. #548
// then ruled the digest is never written to a file or an export, because a key
// rotation would leave it unresolvable in a document whose whole purpose is to
// stay meaningful for years. Both are right, and they collide over exactly this
// string.
//
// The pack's own hash is a THIRD KEY that belongs to neither problem. It is not
// the email and is not derived from it, which is all #546 ever wanted. It
// depends on no key, so no rotation can strand it, which is all #548 ever
// wanted. And it is the ONE VALUE THE PLATFORM PERSISTS about the handover
// (migration 118), so a file in a mailbox in 2031 resolves to its own row with
// no second index — something the digest could never have done, since no row
// was ever allowed to carry one. It is self-checking: `sha256sum` the file, and
// the first sixteen characters are its name.
//
// The date is time-varying and stays here deliberately, on #546's shape: a
// filename is a label on a document rather than the document, and the archive's
// BYTES are what two packs over one record must agree on.
func Filename(sha256Hex string, generatedOn time.Time) string {
	key := sha256Hex
	if len(key) > FilenameKeyLength {
		key = key[:FilenameKeyLength]
	}
	return fmt.Sprintf("consent-evidence-%s-%s.zip", key, generatedOn.UTC().Format("2006-01-02"))
}

// coveredActs lists every act the pack disclosed, customers first then staff,
// in the order the pack presents them.
func coveredActs(in Input) []CoveredAct {
	covered := make([]CoveredAct, 0, len(in.Acts))
	for _, act := range in.Acts {
		covered = append(covered, CoveredAct{Kind: ActKindConsentRecord, ID: act.ID})
	}
	if in.Staff != nil {
		for _, acceptance := range in.Staff.Acceptances {
			covered = append(covered, CoveredAct{Kind: ActKindStaffTermsAcceptance, ID: acceptance.ID})
		}
	}
	return covered
}

// entry is one file in the archive, already rendered.
type entry struct {
	Name string
	Body []byte
}

// writeZip writes the entries in the order given, with one fixed timestamp
// each.
//
// DEFLATE, and compression is what makes the size argument hold: the texts are
// markdown and the pack carries every locale of every edition an act names, so
// the same paragraphs appear in two languages and compress hard.
//
// No file modes are set. `zip.FileHeader.SetMode` writes the creator-version
// and external-attributes fields from the host OS's idea of a permission bit,
// which is one more thing that could differ between a developer's machine and
// Cloud Run for a file whose bytes must not.
func writeZip(entries []entry) ([]byte, error) {
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := archive.CreateHeader(&zip.FileHeader{
			Name:     e.Name,
			Method:   zip.Deflate,
			Modified: entryModified,
		})
		if err != nil {
			return nil, fmt.Errorf("evidence: create %s: %w", e.Name, err)
		}
		if _, err := w.Write(e.Body); err != nil {
			return nil, fmt.Errorf("evidence: write %s: %w", e.Name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, fmt.Errorf("evidence: close archive: %w", err)
	}
	return buf.Bytes(), nil
}

// buildTexts renders every edition's artifacts to `.md` entries and indexes the
// editions by (document, id) for the record.
//
// DEDUPLICATED BY EDITION, NOT BY ACT (#568). Twenty acts against one edition
// carry that edition's text once, which is why a pack's size is bounded by how
// many editions exist rather than by how active the person was.
//
// EVERY LOCALE OF EVERY EDITION, always. The fingerprint spans all of an
// edition's languages at once, so a pack that carried only the language
// somebody was shown could not reproduce the hash printed beside their
// acceptance — it would state a number no reader could check, which is worse
// than stating none.
func buildTexts(editions []Edition) ([]entry, map[string]Edition, error) {
	index := make(map[string]Edition, len(editions))
	var entries []entry
	for _, edition := range editions {
		key := editionKey(edition.Document, edition.ID)
		if _, seen := index[key]; seen {
			return nil, nil, fmt.Errorf("evidence: edition %s/%s supplied twice", edition.Document, edition.ID)
		}
		index[key] = edition
		for _, locale := range legal.Locales(edition.Artifacts) {
			for _, artifact := range legal.InLocale(edition.Artifacts, locale) {
				entries = append(entries, entry{
					Name: TextPath(edition, artifact),
					// The bytes as stored and as served, with no normalisation
					// step: these are the bytes that were hashed.
					Body: []byte(artifact.Body),
				})
			}
		}
	}
	// Sorted by path so the ordering is a property of the names rather than of
	// the order the caller happened to read the editions in.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, index, nil
}

// TextPath is where one artifact lives in the archive:
// `texts/<document>/<edition-label>/<locale>/<ordinal>-<slug>.md`.
//
// THE ORDINAL LEADS THE FILENAME so that a plain `ls` shows the preimage order,
// which is the order the fingerprint is computed in. A listing that sorted by
// slug would put the pieces of the hash in an order that looked authoritative
// and was not.
func TextPath(edition Edition, artifact legal.Artifact) string {
	return fmt.Sprintf("%s%s/%s/%s/%d-%s.md",
		TextsPrefix, edition.Document, edition.Label, artifact.Locale, artifact.Ordinal, artifact.Slug)
}

// editionKey is the (document, id) pair an act resolves an edition by. The
// document is part of the key because the two lineages are independent tables
// and an id is only unique within one.
func editionKey(document, id string) string { return document + "/" + id }

// The two documents, in the vocabulary the paths and the record use.
const (
	DocumentPolicy = "policy"
	DocumentTerms  = "terms"
)

// localeCode renders a locale for a path or a record field.
func localeCode(locale platform.Locale) string { return string(locale) }
