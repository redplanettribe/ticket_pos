package legal_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/migrations"
)

// THE GUARANTEE THIS WHOLE FEATURE RESTS ON, and the test that replaced the two
// deleted seed drift tests (#558).
//
// Every edition this platform has ever published carries a fingerprint that
// acceptances point at — eleven Customers point at `0-placeholder` alone — and
// the text of that edition now lives in migration 109 as rows. This test
// recomputes each fingerprint from that text, in Go, and fails if a single byte
// of any edition has moved.
//
// IT IS STRICTLY STRONGER THAN WHAT IT REPLACED. `seed_test.go` compared the
// embedded artifacts against ONE hardcoded migration — a single `seedMigration`
// constant pointing at the newest edition — so every superseded edition was
// unguarded, which is exactly how the placeholder's hash came to be rewritten
// three times without CI noticing. This finds every edition any migration
// seeds, so an edition can never fall out of coverage by being superseded, and
// a new one cannot be published without its text.
//
// It needs no Docker and no Postgres: it reads the migration files, so it fails
// on the machine of whoever changed the text rather than in a container job
// downstream. Migration 109's own DO block proves the same property in SQL, on
// the database, against the rows that were actually inserted. Two independent
// implementations of one preimage rule, meeting on the same three numbers.
func TestEveryPublishedEditionsFingerprintIsReproducibleFromItsStoredText(t *testing.T) {
	t.Parallel()

	seeded := seededEditions(t)
	stored := storedArtifacts(t)

	// The three fingerprints production holds, named out loud. A seed migration
	// deleted or a label renamed would otherwise quietly shrink what this test
	// covers, and these are the values every existing acceptance points at.
	for _, known := range []struct{ document, label, hash string }{
		{"policy", "0-placeholder", "42d9c2c79c523359033bac14abcfea0c28a808e980e496e7b2d66979d55f0a2b"},
		{"policy", "1", "deddc89049d1918ab8805f2a920f2f9ead6bfefba49cb018e7ba191a7386afc0"},
		{"terms", "1", "2b9bb4bc6b2c6e8ad4ddeb9737db2eba5b94ec4c66c84c1b239bfff9a1c9af24"},
	} {
		key := editionKey{known.document, known.label}
		if got, ok := seeded[key]; !ok {
			t.Fatalf("no migration seeds the %s edition %q any more", known.document, known.label)
		} else if got != known.hash {
			t.Fatalf("the seeded %s edition %q carries %s, but production holds %s — a published fingerprint is never rewritten",
				known.document, known.label, got, known.hash)
		}
	}

	for key, hash := range seeded {
		artifacts := stored[key]
		if len(artifacts) == 0 {
			t.Errorf("the %s edition %q is seeded but migration 109 stores no text for it: nothing could serve it, and its acceptances would point at text nobody can produce",
				key.document, key.label)
			continue
		}
		if computed := legal.ContentHash(artifacts); computed != hash {
			t.Errorf("the %s edition %q does not reproduce its fingerprint:\n  stored text hashes to %s\n  the seeded row carries %s",
				key.document, key.label, computed, hash)
		}
	}
}

// The inventory, pinned. The slug set and the ordinal order are what reproduce
// the existing hashes, so a reordering is a fingerprint change wearing the
// clothes of a tidy-up.
func TestTheStoredInventoryIsTheOneThatReproducesTheHashes(t *testing.T) {
	t.Parallel()

	stored := storedArtifacts(t)
	for key, want := range map[editionKey][]string{
		{"policy", "0-placeholder"}: {"short-notice", "label-policy-acceptance", "label-marketing-consent", "label-networking-consent", "policy"},
		{"policy", "1"}:             {"short-notice", "label-policy-acceptance", "label-marketing-consent", "label-networking-consent", "policy"},
		{"terms", "1"}:              {"label-terms-acceptance", "terms"},
	} {
		artifacts := stored[key]
		locales := legal.Locales(artifacts)
		if len(locales) != 2 || locales[0] != platform.LocaleEN || locales[1] != platform.LocaleES {
			t.Errorf("%s edition %q publishes %v, want [en es]", key.document, key.label, locales)
		}
		for _, locale := range locales {
			var slugs []string
			for i, artifact := range legal.InLocale(artifacts, locale) {
				if artifact.Ordinal != i+1 {
					t.Errorf("%s edition %q (%s): ordinals are not 1..n contiguous: %d at position %d",
						key.document, key.label, locale, artifact.Ordinal, i+1)
				}
				slugs = append(slugs, artifact.Slug)
			}
			if strings.Join(slugs, ",") != strings.Join(want, ",") {
				t.Errorf("%s edition %q (%s) stores %v, want %v", key.document, key.label, locale, slugs, want)
			}
		}
	}
}

// The stored bodies are trimmed, which the column's CHECK also enforces:
// TrimSpace is what the fingerprint has always been taken over, so a trailing
// newline in a paste is a changed edition.
func TestStoredBodiesAreTrimmedAndNonEmpty(t *testing.T) {
	t.Parallel()

	for key, artifacts := range storedArtifacts(t) {
		for _, artifact := range artifacts {
			if artifact.Body == "" || artifact.Body != strings.TrimSpace(artifact.Body) {
				t.Errorf("%s edition %q, %s/%s: body is empty or not trimmed", key.document, key.label, artifact.Locale, artifact.Slug)
			}
		}
	}
}

// THE FRAMING RULE ITSELF, on text small enough to check by eye: the locale code
// is a framed field of its own before its artifacts, lengths are BYTES and not
// runes, and languages go in ascending locale code order however the rows
// arrive.
func TestThePreimageIsFramedByBytesWithTheLocaleFirst(t *testing.T) {
	t.Parallel()

	// "ñ" is two bytes and one rune: the frame says 3, not 2.
	artifacts := []legal.Artifact{
		{Locale: platform.LocaleES, Slug: "b", Ordinal: 2, Body: "dos"},
		{Locale: platform.LocaleEN, Slug: "a", Ordinal: 1, Body: "one"},
		{Locale: platform.LocaleES, Slug: "a", Ordinal: 1, Body: "añ"},
		{Locale: platform.LocaleEN, Slug: "b", Ordinal: 2, Body: "two"},
	}
	want := hashOf("2\nen" + "3\none" + "3\ntwo" + "2\nes" + "3\nañ" + "3\ndos")
	if got := legal.ContentHash(artifacts); got != want {
		t.Fatalf("content hash = %s, want %s (the preimage is not framed as documented)", got, want)
	}
}

// Framing is what stops text moving between two artifacts without moving the
// fingerprint — the reason it exists at all.
func TestMovingTextBetweenArtifactsMovesTheFingerprint(t *testing.T) {
	t.Parallel()

	before := []legal.Artifact{
		{Locale: platform.LocaleEN, Ordinal: 1, Body: "we keep your data"},
		{Locale: platform.LocaleEN, Ordinal: 2, Body: "forever"},
	}
	after := []legal.Artifact{
		{Locale: platform.LocaleEN, Ordinal: 1, Body: "we keep your data forever"},
		{Locale: platform.LocaleEN, Ordinal: 2, Body: ""},
	}
	if legal.ContentHash(before) == legal.ContentHash(after) {
		t.Fatal("the same words in different artifacts hash the same: the preimage is not framed")
	}
}

// Ordinal decides the preimage order, and it is data — so two editions with the
// same text in a different order are different editions.
func TestOrdinalOrderDecidesTheFingerprint(t *testing.T) {
	t.Parallel()

	first := []legal.Artifact{
		{Locale: platform.LocaleEN, Ordinal: 1, Body: "alpha"},
		{Locale: platform.LocaleEN, Ordinal: 2, Body: "bravo"},
	}
	swapped := []legal.Artifact{
		{Locale: platform.LocaleEN, Ordinal: 1, Body: "bravo"},
		{Locale: platform.LocaleEN, Ordinal: 2, Body: "alpha"},
	}
	if legal.ContentHash(first) == legal.ContentHash(swapped) {
		t.Fatal("reordering the artifacts left the fingerprint unchanged")
	}
}

// ContentHash reads its argument; it does not reorder the caller's slice. The
// caller holds the edition it read, and a helper that sorted it in place would
// silently change what a second reader sees.
func TestContentHashDoesNotReorderItsArgument(t *testing.T) {
	t.Parallel()

	artifacts := []legal.Artifact{
		{Locale: platform.LocaleES, Ordinal: 1, Body: "uno"},
		{Locale: platform.LocaleEN, Ordinal: 1, Body: "one"},
	}
	legal.ContentHash(artifacts)
	if artifacts[0].Locale != platform.LocaleES {
		t.Fatal("ContentHash sorted the caller's slice")
	}
}

// --- reading the migrations -------------------------------------------------

type editionKey struct{ document, label string }

var (
	// Every version row any migration seeds: the document, the label, the
	// fingerprint. Deliberately a glob over all migrations rather than a
	// hardcoded filename — an edition must not fall out of coverage by being
	// superseded, which is precisely what happened under seed_test.go.
	seedPattern = regexp.MustCompile(
		`(?s)INSERT INTO (policy|terms)_versions \(label, effective_date, content_hash\)\s*VALUES \(\s*'([^']+)',\s*DATE '[^']+',\s*'([0-9a-f]{64})'\s*\)`)
	// Every artifact migration 109 stores.
	artifactPattern = regexp.MustCompile(
		`(?s)INSERT INTO (policy|terms)_version_artifacts \(version_id, locale, slug, ordinal, body\)\nSELECT v\.id, '([a-z]+)', '([a-z-]+)', (\d+), \$legal\$(.*?)\$legal\$ FROM (?:policy|terms)_versions v WHERE v\.label = '([^']+)';`)
)

func seededEditions(t *testing.T) map[editionKey]string {
	t.Helper()

	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	sort.Strings(names)

	seeded := make(map[editionKey]string)
	for _, name := range names {
		raw, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, match := range seedPattern.FindAllStringSubmatch(string(raw), -1) {
			seeded[editionKey{match[1], match[2]}] = match[3]
		}
	}
	if len(seeded) == 0 {
		t.Fatal("no migration seeds a policy or terms edition; this test has stopped testing anything")
	}
	return seeded
}

func storedArtifacts(t *testing.T) map[editionKey][]legal.Artifact {
	t.Helper()

	raw, err := migrations.Files.ReadFile("109_legal_artifacts.sql")
	if err != nil {
		t.Fatalf("read 109_legal_artifacts.sql: %v", err)
	}
	body := string(raw)
	// The text is CARRIED, never fetched: a migration that read a file could
	// only run against a machine that still had it, which is the whole reason
	// the artifacts could not be deleted before.
	for _, reader := range []string{"pg_read_file", "pg_read_binary_file", "lo_import", "COPY FROM"} {
		if strings.Contains(body, reader) {
			t.Errorf("migration 109 uses %s: it must carry the text as literals", reader)
		}
	}

	stored := make(map[editionKey][]legal.Artifact)
	for _, match := range artifactPattern.FindAllStringSubmatch(body, -1) {
		ordinal, err := strconv.Atoi(match[4])
		if err != nil {
			t.Fatalf("ordinal %q: %v", match[4], err)
		}
		key := editionKey{match[1], match[6]}
		stored[key] = append(stored[key], legal.Artifact{
			Locale:  platform.Locale(match[2]),
			Slug:    match[3],
			Ordinal: ordinal,
			Body:    match[5],
		})
	}
	if len(stored) == 0 {
		t.Fatal("migration 109 stores no artifacts in the shape this test reads; if the statements were reformatted, fix the pattern rather than dropping the guard")
	}
	return stored
}

// PublishedText is the text of one edition as migration 109 stores it, for the
// content guards beside this file.
func publishedText(t *testing.T, document, label string, locale platform.Locale, slug string) string {
	t.Helper()

	for _, artifact := range storedArtifacts(t)[editionKey{document, label}] {
		if artifact.Locale == locale && artifact.Slug == slug {
			return artifact.Body
		}
	}
	t.Fatalf("the %s edition %q stores no %s/%s", document, label, locale, slug)
	return ""
}

// hashOf is the expected fingerprint of a preimage written out by hand, so the
// framing assertion above states the bytes rather than restating the code.
func hashOf(preimage string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(preimage)))
}
