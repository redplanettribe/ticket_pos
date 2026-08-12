package policy_test

import (
	"regexp"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/migrations"
)

// The acceptance criterion of #250, and the reason the Privacy Policy text is
// embedded in this binary at all: the fingerprint a Policy Version was
// published under has to be recomputable from the text that is actually served.
//
// This test does the recomputing. Edit a word of the policy body, the Short
// Notice or any consent label without publishing a new edition and it fails —
// which is the whole guarantee. A hash nobody checks is a hash that quietly
// stops being true, and this one is the evidence a compliance officer would
// stand on to say "here is what that person was shown".
//
// It reads the migration rather than the database on purpose. It is the SEED
// that must be right, so this runs in `make test` with no Docker and no
// Postgres, and fails on the machine of whoever edited the text rather than in
// a container job downstream. The integration suite separately proves the row
// that seed produces reaches the endpoint (backend/integration/policy_test.go).
const seedMigration = "060_policy_versions.sql"

var (
	seedHashPattern  = regexp.MustCompile(`'([0-9a-f]{64})'`)
	seedLabelPattern = regexp.MustCompile(`(?m)^\s*'([^']+)',\s*$`)
)

func TestSeededPolicyVersionHashMatchesTheEmbeddedArtifacts(t *testing.T) {
	t.Parallel()

	sql := readSeedMigration(t)

	match := seedHashPattern.FindStringSubmatch(sql)
	if match == nil {
		t.Fatalf("%s seeds no 64-character hex content hash", seedMigration)
	}
	seeded := match[1]

	if computed := policy.ContentHash(); seeded != computed {
		t.Fatalf(
			"the seeded Policy Version's content hash no longer matches the embedded Privacy Policy artifacts.\n"+
				"  seeded in %s: %s\n"+
				"  computed from backend/internal/consent/policy/artifacts: %s\n\n"+
				"The policy text and its Policy Version are one artifact: they change in the same commit.\n"+
				"If this edit is a correction to unpublished placeholder text, update the hash in the migration.\n"+
				"If this edit changes a PUBLISHED edition, do not touch that row — publish a new one, and read\n"+
				"the warning at the top of the migration first: a new row re-gates every Customer.",
			seedMigration, seeded, computed,
		)
	}
}

// The placeholder edition has to be recognisable as one. The real legal text is
// a content drop before go-live, and a version label that did not say so is how
// placeholder prose ends up quoted in an audit as though somebody had reviewed
// it.
func TestSeededPolicyVersionIsMarkedAsThePlaceholderEdition(t *testing.T) {
	t.Parallel()

	match := seedLabelPattern.FindStringSubmatch(readSeedMigration(t))
	if match == nil {
		t.Fatalf("%s seeds no version label", seedMigration)
	}
	if label := match[1]; label != "0-placeholder" {
		t.Fatalf("seeded version label = %q, want %q", label, "0-placeholder")
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
