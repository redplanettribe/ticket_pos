package integration

import (
	"context"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// Tag names are read through the catalog service rather than over HTTP because
// no endpoint carries a localized name and none is meant to: a Storefront page
// still words its own chips and badges from its message catalogue (ADR 0027),
// and the only reader of a Spanish Tag name is the Follow Digest the backend
// writes (ADR 0030). The service is therefore the seam, and these tests still
// exercise the migration, the schema and the real query.

// localizedTagNames resolves Tag names in a Locale through the catalog service.
func localizedTagNames(t *testing.T, keys []string, locale platform.Locale) map[string]string {
	t.Helper()
	names, err := sharedApp.CatalogService.LocalizedTagNames(context.Background(), keys, locale)
	if err != nil {
		t.Fatalf("localized tag names (%s): %v", locale, err)
	}
	return names
}

// TestPresetTagNameResolvesInEitherLanguage proves the backend can name a
// Preset Tag in both Locales the Storefront serves — the whole point of the
// Spanish display name added to the shared pool.
func TestPresetTagNameResolvesInEitherLanguage(t *testing.T) {
	setupTest(t)

	keys := []string{"music", "arts & theatre", "nightlife"}

	spanish := localizedTagNames(t, keys, platform.LocaleES)
	for key, want := range map[string]string{
		"music":          "Música",
		"arts & theatre": "Arte y teatro",
		"nightlife":      "Vida nocturna",
	} {
		if spanish[key] != want {
			t.Fatalf("es name for %q = %q, want %q", key, spanish[key], want)
		}
	}

	english := localizedTagNames(t, keys, platform.LocaleEN)
	for key, want := range map[string]string{
		"music":          "Music",
		"arts & theatre": "Arts & Theatre",
		"nightlife":      "Nightlife",
	} {
		if english[key] != want {
			t.Fatalf("en name for %q = %q, want %q", key, english[key], want)
		}
	}
}

// TestEverySeededPresetTagHasASpanishName holds the migration to the whole
// curated tier rather than to a sample of it: a Preset Tag with no Spanish name
// is a Digest that reads half in English, and adding one should be a commit
// rather than a discovery made in somebody's inbox.
//
// SQL because the assertion is about the seed itself — every curated row,
// including any a later migration adds — and no API lists the pool's Spanish.
func TestEverySeededPresetTagHasASpanishName(t *testing.T) {
	env := setupTest(t)

	rows, err := env.db.Query(`SELECT canonical_key, display_name_es FROM tags WHERE curated = TRUE ORDER BY canonical_key`)
	if err != nil {
		t.Fatalf("read curated tags: %v", err)
	}
	defer rows.Close()

	var missing []string
	count := 0
	for rows.Next() {
		var key string
		var nameES *string
		if err := rows.Scan(&key, &nameES); err != nil {
			t.Fatalf("scan curated tag: %v", err)
		}
		count++
		if nameES == nil || *nameES == "" {
			missing = append(missing, key)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate curated tags: %v", err)
	}
	if count == 0 {
		t.Fatal("no Preset Tags seeded — the pool should survive a data reset")
	}
	if len(missing) > 0 {
		t.Fatalf("Preset Tags with no Spanish display name: %v", missing)
	}
}

// TestPresetTagWithoutASpanishNameFallsBackToEnglish proves the fallback ADR
// 0027 relies on holds on this side too. A Custom Tag promoted by flipping
// curated in production (ADR 0004) arrives with no Spanish at all, and it must
// read as the English the whole pool read yesterday rather than blank.
//
// SQL to produce the state, because only a migration or a production UPDATE can
// mint a Preset Tag and neither is reachable from the API.
func TestPresetTagWithoutASpanishNameFallsBackToEnglish(t *testing.T) {
	env := setupTest(t)

	if _, err := env.db.Exec(`UPDATE tags SET display_name_es = NULL WHERE canonical_key = 'music'`); err != nil {
		t.Fatalf("clear spanish name: %v", err)
	}
	// Preset Tags survive the truncate-based reset between tests (they are seeded
	// once, by migration), so this one has to put back what it took away.
	t.Cleanup(func() {
		if _, err := env.db.Exec(`UPDATE tags SET display_name_es = 'Música' WHERE canonical_key = 'music'`); err != nil {
			t.Fatalf("restore spanish name: %v", err)
		}
	})

	names := localizedTagNames(t, []string{"music"}, platform.LocaleES)
	if names["music"] != "Music" {
		t.Fatalf("es name for music = %q, want the English fallback %q", names["music"], "Music")
	}
}

// TestCustomTagResolvesToItsCoinedNameInEveryLanguage proves an Organization's
// own word is left exactly as it was typed, in both Locales. Nobody owes a
// Custom Tag a translation and nobody could supply one (ADR 0027).
func TestCustomTagResolvesToItsCoinedNameInEveryLanguage(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID := createDraftEvent(t, env, sessionID, "Rave", "rave")

	if _, body := setEventTags(t, env, sessionID, eventID, []string{"Techno"}); body.Error != nil {
		t.Fatalf("set event tags: %+v", body.Error)
	}

	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		names := localizedTagNames(t, []string{"techno"}, locale)
		if names["techno"] != "Techno" {
			t.Fatalf("%s name for techno = %q, want the coined %q", locale, names["techno"], "Techno")
		}
	}
}
