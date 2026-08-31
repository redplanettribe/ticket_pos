package service

import (
	"encoding/base64"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// WHAT THESE PIN (#565, ADR 0067): the surface's own three rules — the cursor's
// encoding, the unparseable-cursor-is-absent rule, and the standing default —
// with no database and no HTTP.

// A cursor round-trips, and it is a plain base64 of the address rather than a
// secret. That is exactly why the cursor travels in a POSTED BODY and never in
// a query string: it IS an email, and an encoding is not a disguise.
func TestACursorRoundTripsAndIsMerelyAnEncoding(t *testing.T) {
	encoded := encodeAcceptanceCursor("ana@example.com")
	if encoded == "ana@example.com" {
		t.Fatal("the cursor must be encoded for transport")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || string(raw) != "ana@example.com" {
		t.Fatalf("the cursor is base64 RawURL of the email, following the public event feed; decoded %q (%v)", raw, err)
	}
	if got := decodeAcceptanceCursor(encoded); got != "ana@example.com" {
		t.Fatalf("decodeAcceptanceCursor = %q, want the email back", got)
	}
	// No padding: RawURL, so a cursor is safe in a body, a header or (were it
	// ever put there, which it is not) a path.
	if got := encodeAcceptanceCursor("ab"); got != base64.RawURLEncoding.EncodeToString([]byte("ab")) {
		t.Fatalf("cursor encoding drifted: %q", got)
	}
}

// AN UNPARSEABLE CURSOR IS TREATED AS ABSENT and serves the first page — the
// public event feed's rule (catalog/service.decodeCursor). A bad cursor means a
// stale bookmark or a truncated copy-paste, and the useful answer is the top of
// the list rather than an error page.
func TestAnUnparseableCursorIsTreatedAsAbsent(t *testing.T) {
	for _, cursor := range []string{"", "   ", "!!!not-base64!!!", "%%%%", "a b c"} {
		if got := decodeAcceptanceCursor(cursor); got != "" {
			t.Fatalf("decodeAcceptanceCursor(%q) = %q, want \"\" (first page)", cursor, got)
		}
	}
}

// EMPTY MEANS OUTSTANDING — both browsers default to it, because "who owes
// something" is the question the feature exists to answer.
func TestAnAbsentStandingDefaultsToOutstanding(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		standing, err := parseBrowseStanding(raw)
		if err != nil {
			t.Fatalf("parseBrowseStanding(%q) errored: %v", raw, err)
		}
		if standing != legal.StandingOutstanding {
			t.Fatalf("parseBrowseStanding(%q) = %q, want %q", raw, standing, legal.StandingOutstanding)
		}
	}
}

// AN UNKNOWN STANDING IS REFUSED AND NEVER WIDENED. Defaulting it to
// Outstanding would hide a client bug behind a plausible screen; widening it to
// "everybody" would answer "who owes an acceptance?" with the whole customer
// base, which on a screen with no total is indistinguishable from a re-gate.
func TestAnUnknownStandingIsRefusedRatherThanWidened(t *testing.T) {
	_, err := parseBrowseStanding("withdrawn")
	if err == nil {
		t.Fatal("an unknown standing must be refused")
	}
	domain, ok := err.(apperror.DomainError)
	if !ok || domain.Code() != "LEGAL_STANDING_UNKNOWN" {
		t.Fatalf("want LEGAL_STANDING_UNKNOWN, got %v", err)
	}
	details, _ := domain.Details().(map[string]string)
	if details["standing"] != "withdrawn" {
		t.Fatalf("the refusal must name the offending token, got %v", domain.Details())
	}
}

func TestTheFourStandingsAreAccepted(t *testing.T) {
	for _, raw := range []string{"current", "outstanding", "never_seen", "former"} {
		if _, err := parseBrowseStanding(raw); err != nil {
			t.Fatalf("parseBrowseStanding(%q) errored: %v", raw, err)
		}
	}
}

// The search fragment is folded the way every stored address was folded on the
// way in, so an operator who types an address the way a human writes it still
// finds the person.
func TestTheSearchFragmentIsFoldedLikeAStoredAddress(t *testing.T) {
	if got := normalizeAcceptanceSearch("  Ana@Example.COM "); got != "ana@example.com" {
		t.Fatalf("normalizeAcceptanceSearch = %q", got)
	}
}
