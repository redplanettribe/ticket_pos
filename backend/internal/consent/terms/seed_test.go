package terms_test

import (
	"regexp"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/migrations"
)

// The Terms' counterpart of the policy seed drift test, and the same guarantee:
// the fingerprint a Terms Version was published under has to be recomputable
// from the text that is actually served. Edit a word of the body or the
// acceptance label without publishing a new edition and this fails — on the
// machine of whoever edited the text, with no Docker and no Postgres, because
// it reads the seed migration rather than the database.
//
// It follows the CURRENT edition: publishing an edition moves this constant to
// that migration, and the superseded row's hash stays as the record of a text
// this binary no longer serves.
const seedMigration = "105_terms_versions.sql"

var (
	seedHashPattern  = regexp.MustCompile(`'([0-9a-f]{64})'`)
	seedLabelPattern = regexp.MustCompile(`(?m)^\s*'([^']+)',\s*$`)
)

func TestSeededTermsVersionHashMatchesTheEmbeddedArtifacts(t *testing.T) {
	t.Parallel()

	sql := readSeedMigration(t)

	match := seedHashPattern.FindStringSubmatch(sql)
	if match == nil {
		t.Fatalf("%s seeds no 64-character hex content hash", seedMigration)
	}
	seeded := match[1]

	if computed := terms.ContentHash(); seeded != computed {
		t.Fatalf(
			"the seeded Terms Version's content hash no longer matches the embedded Terms artifacts.\n"+
				"  seeded in %s: %s\n"+
				"  computed from backend/internal/consent/terms/artifacts: %s\n\n"+
				"The Terms text and its Terms Version are one artifact: they change in the same commit.\n"+
				"If this edit changes a PUBLISHED edition, do not touch that row — publish a new one, and\n"+
				"read the warning at the top of the migration first: a new row re-gates every Customer\n"+
				"and every member of the Staff platform.",
			seedMigration, seeded, computed,
		)
	}
}

// Edition "1" is the real, published edition — the row whose insert performs
// the one-time re-gate (#533, ADR 0066). A placeholder label reappearing here
// would mean draft prose being quoted in an audit as though accepted.
func TestSeededTermsVersionIsTheRealEdition(t *testing.T) {
	t.Parallel()

	match := seedLabelPattern.FindStringSubmatch(readSeedMigration(t))
	if match == nil {
		t.Fatalf("%s seeds no version label", seedMigration)
	}
	if label := match[1]; label != "1" {
		t.Fatalf("seeded version label = %q, want %q", label, "1")
	}
}

func readSeedMigration(t *testing.T) string {
	t.Helper()

	raw, err := migrations.Files.ReadFile(seedMigration)
	if err != nil {
		t.Fatalf("read %s: %v", seedMigration, err)
	}
	return string(raw)
}
