package integration

import (
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// THE SQL SHADOW OF platform.NormalizeEmail, PINNED TO IT IN A LIVE DATABASE
// (#398, parent #391, ADR 0055).
//
// The platform-global Customer rests on UNIQUE(email) plus exactly ONE
// normalisation rule, and some comparisons of that rule happen where Postgres
// cannot call Go: a Capacity Hold matched against a pending Payment's verbatim
// address before any Customer exists (ADR 0025), and a Holder's address compared
// against their Sale's buyer inside one query (#392). Those sites go through
// platform.NormalizeEmailSQL, which is the second implementation of the rule.
//
// A DRIFT BETWEEN THE TWO FAILS NOTHING ON ITS OWN. It does not break a build,
// it does not break a query, and it does not raise an error anywhere: it quietly
// decides whether a Holder is recognised as the buyer, and therefore whether
// somebody is written to about a reversal. This test is the thing that notices,
// and it is here rather than in a unit test because only a live Postgres can say
// what btrim and lower actually do.
//
// IT RUNS THE EXPRESSION THE PRODUCTION QUERIES RUN, built by the same function
// over a placeholder, so a change to the fold is caught by construction rather
// than by a copy of it kept in step by hand.
//
// SHIPPED MIGRATIONS ARE NOT IN SCOPE. Migrations 084 and 088 carry their own
// copy of the fold as it stood when they ran, which is correct history and is
// not edited; this pins the LIVE paths only.
func TestNormalizeEmailSQLFoldsTheSameWayAsNormalizeEmail(t *testing.T) {
	env := setupTest(t)

	// Addresses of the shape the platform actually stores and compares: typed
	// with the shift key down, pasted out of a spreadsheet cell with padding,
	// carrying the punctuation a real address carries. Non-space whitespace is
	// deliberately absent — see the known margin below.
	for _, address := range []string{
		"ana@example.com",
		"Ana@Example.com",
		"ANA@EXAMPLE.COM",
		"  ana@example.com  ",
		" Ana.Lopez+festival@Example.CO.UK ",
		"ANA_LOPEZ-99@sub.example.com",
		"",
		"   ",
	} {
		var folded string
		if err := env.db.QueryRow(
			`SELECT `+platform.NormalizeEmailSQL("$1::text"), address,
		).Scan(&folded); err != nil {
			t.Fatalf("fold %q in Postgres: %v", address, err)
		}
		if want := platform.NormalizeEmail(address); folded != want {
			t.Errorf("the SQL fold of %q is %q, where platform.NormalizeEmail gives %q.\n"+
				"These two are ONE rule written twice, and the copies have drifted. A Holder\n"+
				"whose address folds differently in the two languages stops being recognised as\n"+
				"the buyer of their own Sale, which decides whether they are mailed — and nothing\n"+
				"else in this codebase would have told you.", address, folded, want)
		}
	}

	// THE ONE KNOWN MARGIN, asserted so it is a recorded fact rather than a
	// surprise: strings.TrimSpace strips all Unicode whitespace, btrim's default
	// strips ASCII spaces alone. Nothing writes such an address — every entry
	// path normalises in Go before storing — so the divergence is unreachable
	// rather than harmless. It is stated here because a reader of the loop above
	// would otherwise conclude the two folds agree on everything.
	const tabbed = "\tana@example.com\t"
	var folded string
	if err := env.db.QueryRow(
		`SELECT `+platform.NormalizeEmailSQL("$1::text"), tabbed,
	).Scan(&folded); err != nil {
		t.Fatalf("fold the tabbed address in Postgres: %v", err)
	}
	if folded == platform.NormalizeEmail(tabbed) {
		t.Errorf("btrim now strips tabs as strings.TrimSpace does, so the two folds agree on %q.\n"+
			"That is a WIDENING and not a break: delete this half of the test and say so in\n"+
			"platform.NormalizeEmailSQL's doc, which currently records the divergence.", tabbed)
	}
}
