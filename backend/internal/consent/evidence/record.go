package evidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	consentsvc "github.com/peter/ticket_pos/backend/internal/consent/service"
)

// `record.json` — THE RECORD ITSELF (#568). `evidence.pdf` beside it is this
// file's reading, rendered from this same value so the two cannot disagree.
//
// It carries the acts VERBATIM, in the per-subject record's own types
// (consentsvc.ConsentActItem), which is the ruling that keeps the export
// honest: the pack names exactly what the screen names, because it is the same
// struct serialised by the same tags. A pack with its own act shape would be a
// second story about one person's evidence, and the two would drift on the
// first field either one grew.
//
// NULL KEEPS MEANING "NOT SHOWN". Every nullable field survives as JSON null
// rather than being flattened to a blank or a "No" — a box that was not on a
// surface is not a refusal, and a technical field the surface never collected
// is not one it collected empty. `presented_locale` keeps its three states,
// absent included, all the way into the file (#567).
//
// WITHDRAWALS ARE THE ACTS THEMSELVES. There is no withdrawal log in here and
// there must never be one: a withdrawal is a `consent_records` row like any
// other, readable as one because `prior_marketing_consent` and
// `prior_networking_consent` say what it took away. A synthesised list would be
// a statement the database does not hold, in a document whose entire value is
// that every sentence in it is a stored fact.

// recordFile is the top-level shape of `record.json`.
//
// STRUCTS AND SLICES ONLY, never a map. Go marshals map keys in sorted order so
// a map would in fact be deterministic, but the ORDER OF THE FIELDS is part of
// what a reader learns from, and a struct is the only way to fix it.
type recordFile struct {
	// Pack is what this file is, how to check it, and what it does not say.
	// FIRST, because a reader who stops after the first screen should have
	// read the caveats rather than the payload.
	Pack packStatement `json:"pack"`
	// Subject is the address the pack was assembled for. It is the only key
	// that spans both populations below.
	Subject subjectStatement `json:"subject"`
	// Customer is the Customer half, null where no Customer holds the address.
	Customer *Subject `json:"customer"`
	// CustomerActs is that Customer's COMPLETE consent history, newest first,
	// exactly as the record screen serves it. An empty array where there is
	// none — which is a different fact from a null Customer, and stays
	// different.
	CustomerActs []consentActJSON `json:"customer_acts"`
	// Staff is the staff half, null where the address is on no staff row and
	// holds no acceptance. SEE THE LIMITATION ABOVE: null here means "no rows
	// for this address", never "this person is not staff".
	Staff *StaffSubject `json:"staff"`
	// Editions is every edition either half names, with its fingerprint and the
	// exact preimage of it, keyed to the files under `texts/`.
	Editions []editionRecord `json:"editions"`

	// annexBodies is path -> stored text, for the PDF's annexes to print from.
	//
	// UNEXPORTED, so encoding/json ignores it, and that is the point: the texts
	// are RAW `.md` FILES in the archive and must not also appear as JSON
	// strings (#568). Verification is then a shell script over bytes rather
	// than a JSON parse, and there is exactly one copy of each text in the
	// pack. It is keyed by the archive path so the annex, the preimage list and
	// the file itself all name the same thing.
	annexBodies map[string]string
}

// consentActJSON is the act type carried straight through: the per-subject
// record's OWN struct, not a copy of it, so a field added to the record screen
// appears in the pack automatically and the two can never fall out of step.
type consentActJSON = consentsvc.ConsentActItem

// packStatement is the pack telling the reader what it is.
type packStatement struct {
	Format string `json:"format"`
	// About is one paragraph on what the file contains and where it came from.
	About string `json:"about"`
	// WhatMakesAConsentValid is the statement #568 requires in so many words,
	// so that four supporting fields are never mistaken for the proof they are
	// not.
	WhatMakesAConsentValid string `json:"what_makes_a_consent_valid"`
	// Limitations are the two sentences the pack must state about itself.
	Limitations []string `json:"limitations"`
	// PreimageRule is how a fingerprint is computed, restated here so the pack
	// can verify its own hashes with nothing but the files beside it.
	PreimageRule preimageRule `json:"preimage_rule"`
	// Determinism explains why there is no generation date and no standing.
	Determinism string `json:"determinism"`
}

// subjectStatement names who the pack is about.
//
// AN EMAIL AND NOTHING ELSE. There is no digest here and there is none anywhere
// in this file (#548): a key rotation must not orphan a document whose purpose
// is to stay readable for years. The filename's key is the pack's own hash, for
// the same reason — see Filename.
type subjectStatement struct {
	Email string `json:"email"`
	// Populations names which halves of the platform answered to this address,
	// so "one of the two is empty" is stated rather than inferred from a null.
	Populations []string `json:"populations"`
}

// preimageRule is legal.ContentHash written down for a reader with a shell.
type preimageRule struct {
	Algorithm string   `json:"algorithm"`
	Steps     []string `json:"steps"`
	// Example is a runnable reproduction of one edition's hash from the files
	// in this archive, so the rule is checkable rather than merely stated.
	Example string `json:"example"`
}

// editionRecord is one edition: what it is, what it hashes to, and the exact
// sequence of bytes that hash covers.
type editionRecord struct {
	Document string `json:"document"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	// PublishedAs is `gating` or `correction` — whether publishing this edition
	// re-gated everybody.
	//
	// THE ONLY PROPERTY OF AN EDITION THIS FILE CARRIES, and it is here because
	// it was fixed at publication and cannot change afterwards. There is
	// deliberately NO field saying whether the edition clears the gate TODAY:
	// that answer moves at midnight as the database's own day moves, so two
	// packs over one record would disagree across a night and neither could be
	// checked against a stored hash.
	PublishedAs   string `json:"published_as"`
	EffectiveDate string `json:"effective_date"`
	ContentHash   string `json:"content_hash"`
	// Locales are the languages this edition publishes, ascending — read from
	// its own rows. ALL of them ride in the archive, whatever anybody was
	// shown, because the fingerprint spans them at once.
	Locales []string `json:"locales"`
	// Preimage is the fingerprint's input, field by field, in order. Feed these
	// fields to the framing rule above and the SHA-256 is `content_hash`.
	Preimage []preimageField `json:"preimage"`
}

// preimageField is one framed field of a content hash: either a language code
// or one artifact's body, which lives in the archive at `path`.
type preimageField struct {
	// Field is `locale` or `artifact`.
	Field string `json:"field"`
	// Locale is the language this field belongs to, on both kinds.
	Locale string `json:"locale"`
	// Bytes is the field's length in BYTES, which is what the framing counts —
	// `octet_length` in SQL, `len(string)` in Go. Characters would be a
	// different number for every accented word in the Spanish text.
	Bytes int `json:"bytes"`
	// Slug, Ordinal and Path are an artifact's; absent on a locale field.
	Slug    string `json:"slug,omitempty"`
	Ordinal *int   `json:"ordinal,omitempty"`
	Path    string `json:"path,omitempty"`
}

// The prose the pack states about itself. THESE ARE CONSTANTS AND THE PDF READS
// THE SAME ONES, so the machine-readable record and its human reading cannot
// state different caveats.
const (
	packAbout = "This is a Consent Evidence Pack: the complete record of what this platform holds " +
		"about one email address in its consent and legal-acceptance logs, generated on demand by a " +
		"platform operator and not stored. record.json is the record; evidence.pdf is a reading of it; " +
		"the files under texts/ are the exact bytes of every legal edition named below, in every " +
		"language each was published in. Nothing here is summarised, inferred or reconstructed: every " +
		"value is a stored fact, and where the log holds no value the field is null rather than blank."

	// The statement #568 requires in so many words.
	packValidityStatement = "A consent is valid because email_proven was true when it was captured. " +
		"That single field is what separates a granted consent from one still awaiting confirmation. " +
		"The ip, user_agent, session_id and origin_url fields are supporting technical detail and are " +
		"NOT proof of anything on their own: they record what the surface collected, they are null " +
		"wherever it collected nothing, and none of them can establish that the person at the address " +
		"is the person who answered."

	// The first of the two limitations #568 requires.
	packLimitationStaff = "An empty or absent staff section means THERE ARE NO ROWS FOR THIS ADDRESS " +
		"in the staff acceptance log. It does not mean the person is not a member of staff, and it must " +
		"never be read that way: somebody may hold a Staff platform account under a different address, " +
		"or have signed in before this log existed."

	// The second.
	packLimitationEmail = "The email on each act is the address ASSERTED AT THE MOMENT OF CAPTURE, not " +
		"a canonical identity. This platform has no verified identity for a person: an address is what " +
		"somebody typed and, where email_proven is true, later proved they could receive mail at. Acts " +
		"are grouped here by that address and by the Customer record it belonged to, which is the " +
		"strongest link the stored data supports."

	packDeterminism = "This pack is deterministic: the same acts produce byte-identical bytes, whichever " +
		"day it is generated, which is why only its SHA-256 and the ids of the acts it covers are " +
		"retained rather than a copy of the file. That is also why nothing here is time-varying. There " +
		"is no generation date inside the archive (it is in the filename), and an edition states " +
		"published_as — a fact fixed when it was published — but never whether it currently satisfies " +
		"a gate, which changes as dates arrive."

	packHashAlgorithm = "SHA-256, hex-encoded lowercase"
)

// preimageSteps is legal.ContentHash's rule in words. It is the same rule
// migration 109's DO block implements in SQL; a pack that stated it differently
// would print a recipe that produced a different number.
var preimageSteps = []string{
	"An edition's fingerprint covers EVERY language it publishes at once. A single language cannot be verified on its own, which is why every language of every edition named here is in this archive.",
	"Take the edition's languages in ascending order of their locale code, compared as bytes (\"en\" before \"es\").",
	"For each language, emit the LOCALE CODE as one field, then that language's artifacts as one field each, in ascending `ordinal` — the order their filenames are numbered in under texts/.",
	"Frame every field as: its length in BYTES in decimal, then a single newline (0x0A), then the field's bytes verbatim. Nothing is trimmed, normalised or re-encoded at this point; the stored bytes are the served bytes are the hashed bytes.",
	"SHA-256 the concatenation of the framed fields. Its lowercase hexadecimal form is the edition's content_hash.",
	"The framing is what makes the fingerprint mean something: without it, moving a sentence out of one artifact and into the next would leave the hash unchanged.",
}

// preimageExample is a runnable check, so the rule above is testable with the
// archive in front of you rather than merely readable.
const preimageExample = "For each field of an edition's preimage list below, in order: " +
	"`printf '%s\\n' \"$bytes\"` then, for an artifact field, `cat \"$path\"`, and for a locale field the " +
	"locale code with no trailing newline. Pipe the whole concatenation through `sha256sum` and compare " +
	"with content_hash."

// buildRecord assembles record.json's value.
//
// It does NOT verify that every edition an act names is present in `editions`.
// A missing one leaves the act naming an id with no text beside it, which is a
// thinner pack; refusing to build would deny somebody their record over a
// referential problem in this platform's own tables, which is the wrong way
// round for a document that exists to be handed over.
func buildRecord(in Input, editions map[string]Edition) (recordFile, error) {
	record := recordFile{
		Pack: packStatement{
			Format:                 FormatVersion,
			About:                  packAbout,
			WhatMakesAConsentValid: packValidityStatement,
			Limitations:            []string{packLimitationStaff, packLimitationEmail},
			PreimageRule: preimageRule{
				Algorithm: packHashAlgorithm,
				Steps:     preimageSteps,
				Example:   preimageExample,
			},
			Determinism: packDeterminism,
		},
		Subject: subjectStatement{
			Email:       in.Email,
			Populations: populations(in),
		},
		Customer: in.Customer,
		// A non-nil empty slice, so an empty history is `[]` rather than
		// `null`: "this person performed no acts" and "this field was not
		// filled in" are different sentences.
		CustomerActs: append([]consentActJSON{}, in.Acts...),
		Staff:        in.Staff,
		Editions:     editionRecords(editions),
		annexBodies:  annexBodies(editions),
	}
	return record, nil
}

// annexBodies indexes every stored text by its archive path.
func annexBodies(editions map[string]Edition) map[string]string {
	bodies := make(map[string]string)
	for _, edition := range editions {
		for _, artifact := range edition.Artifacts {
			bodies[TextPath(edition, artifact)] = artifact.Body
		}
	}
	return bodies
}

// populations names which halves answered, in a fixed order.
func populations(in Input) []string {
	found := []string{}
	if in.Customer != nil {
		found = append(found, "customer")
	}
	if in.Staff != nil {
		found = append(found, "staff")
	}
	return found
}

// editionRecords renders every edition with its preimage, ordered by document
// then label so the list reads as two lineages rather than as a query result.
func editionRecords(editions map[string]Edition) []editionRecord {
	records := make([]editionRecord, 0, len(editions))
	for _, edition := range editions {
		records = append(records, editionRecordOf(edition))
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Document != records[j].Document {
			return records[i].Document < records[j].Document
		}
		if records[i].Label != records[j].Label {
			return records[i].Label < records[j].Label
		}
		return records[i].ID < records[j].ID
	})
	return records
}

func editionRecordOf(edition Edition) editionRecord {
	locales := legal.Locales(edition.Artifacts)
	record := editionRecord{
		Document:      edition.Document,
		ID:            edition.ID,
		Label:         edition.Label,
		PublishedAs:   publishedAs(edition.Gating),
		EffectiveDate: edition.EffectiveDate.UTC().Format("2006-01-02"),
		ContentHash:   edition.ContentHash,
		Locales:       make([]string, 0, len(locales)),
		Preimage:      make([]preimageField, 0, len(edition.Artifacts)+len(locales)),
	}
	for _, locale := range locales {
		code := localeCode(locale)
		record.Locales = append(record.Locales, code)
		record.Preimage = append(record.Preimage, preimageField{
			Field:  "locale",
			Locale: code,
			Bytes:  len(code),
		})
		for _, artifact := range legal.InLocale(edition.Artifacts, locale) {
			ordinal := artifact.Ordinal
			record.Preimage = append(record.Preimage, preimageField{
				Field:   "artifact",
				Locale:  code,
				Bytes:   len(artifact.Body),
				Slug:    artifact.Slug,
				Ordinal: &ordinal,
				Path:    TextPath(edition, artifact),
			})
		}
	}
	return record
}

// publishedAs renders the one edition property the pack may carry.
func publishedAs(gating bool) string {
	if gating {
		return "gating"
	}
	return "correction"
}

// marshalRecord renders record.json.
//
// INDENTED, because a human opens this file too and an evidence document that
// arrives as one line is one nobody reads. HTML escaping OFF, so a `&` in a
// user agent or an origin URL reads as itself rather than as `&` — both
// are deterministic, and only one is legible.
func marshalRecord(record recordFile) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		return nil, fmt.Errorf("evidence: render record.json: %w", err)
	}
	// Encode appends a newline; keep it. A text file that ends without one is
	// the kind of thing a shell pipeline silently changes.
	return buf.Bytes(), nil
}

// ContainsDigest reports whether a value looks like a Staff Digest — 32 hex
// characters — and exists so the pack's own tests can assert the absence #548
// requires without reimplementing the shape.
//
// IT IS A TEST AID AND NOTHING GENERATES ONE HERE. This package has no digester
// and cannot mint a digest; the guarantee is structural, and this only lets a
// test say so.
func ContainsDigest(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}
