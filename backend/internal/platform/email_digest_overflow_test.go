package platform

import (
	"strings"
	"testing"
)

// The "+N more" line of a capped section (#222).
//
// The cap, the carry and the English line are proved where they matter, over
// HTTP against a real database (integration/follow_digest_cap_test.go). What is
// left here is the pair of rendering branches that suite cannot reach without
// staging a section's worth of Events twice over: the Spanish copy, and the
// degradation for a deployment with no Storefront origin. Both are pure
// functions of a composed message, so they are cheapest to state here.

func TestFollowDigestOverflowLineIsWrittenInTheReadersLanguage(t *testing.T) {
	digest := FollowDigest{
		To:           "ana@example.com",
		CustomerName: "Ana",
		Locale:       LocaleES,
		New:          []FollowDigestEvent{{Name: "Seguida Fest"}},
		NewOverflow:  FollowDigestOverflow{Count: 7, URL: "https://storefront.example/?tags=musica"},
	}

	text := digest.Text()
	if !strings.Contains(text, "+7 más") {
		t.Fatalf("a Spanish Digest's overflow line is not in Spanish; body:\n%s", text)
	}
	if !strings.Contains(text, "https://storefront.example/?tags=musica") {
		t.Fatalf("the overflow line carries no link; body:\n%s", text)
	}
}

// A deployment with no Storefront origin still ADMITS TO THE CAP. The count with
// nowhere to go is thin, but the alternative is a section that looks complete
// and is not, which is the silent truncation this feature exists to prevent.
func TestFollowDigestOverflowStillSaysHowManyWithNoLink(t *testing.T) {
	digest := FollowDigest{
		To:           "ana@example.com",
		CustomerName: "Ana",
		Locale:       DefaultLocale,
		New:          []FollowDigestEvent{{Name: "Followed Fest"}},
		NewOverflow:  FollowDigestOverflow{Count: 4},
	}

	text := digest.Text()
	if !strings.Contains(text, "+4 more") {
		t.Fatalf("a capped section with no link to offer says nothing about the cap; body:\n%s", text)
	}
}

// A section that fitted prints no overflow line at all — the ordinary week, and
// the one a stray "+0 more" would make look broken.
func TestFollowDigestPrintsNoOverflowLineWhenTheSectionFitted(t *testing.T) {
	digest := FollowDigest{
		To:           "ana@example.com",
		CustomerName: "Ana",
		Locale:       DefaultLocale,
		New:          []FollowDigestEvent{{Name: "Followed Fest"}},
	}

	if text := digest.Text(); strings.Contains(text, "more") {
		t.Fatalf("an uncapped section announces an overflow; body:\n%s", text)
	}
}
