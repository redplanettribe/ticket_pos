package catalog_test

import (
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

func TestParseTicketQuestionKindAcceptsTheSevenKinds(t *testing.T) {
	t.Parallel()

	// The seven kinds CONTEXT.md names, and nothing else. Deliberately no file
	// upload and no purpose-built email or phone kind.
	for _, raw := range []string{
		"short_text", "long_text", "single_choice", "multi_choice",
		"number", "date", "checkbox",
	} {
		kind, ok := catalog.ParseTicketQuestionKind(raw)
		if !ok {
			t.Fatalf("ParseTicketQuestionKind(%q) refused a kind this platform offers", raw)
		}
		if string(kind) != raw {
			t.Fatalf("ParseTicketQuestionKind(%q) = %q", raw, kind)
		}
	}
}

func TestParseTicketQuestionKindRefusesAnythingElse(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "file", "email", "phone", "SHORT_TEXT", "text"} {
		if _, ok := catalog.ParseTicketQuestionKind(raw); ok {
			t.Fatalf("ParseTicketQuestionKind(%q) accepted a kind this platform does not offer", raw)
		}
	}
}

func TestOnlyChoiceKindsOfferOptions(t *testing.T) {
	t.Parallel()

	offers := map[catalog.TicketQuestionKind]bool{
		catalog.TicketQuestionKindShortText:    false,
		catalog.TicketQuestionKindLongText:     false,
		catalog.TicketQuestionKindSingleChoice: true,
		catalog.TicketQuestionKindMultiChoice:  true,
		catalog.TicketQuestionKindNumber:       false,
		catalog.TicketQuestionKindDate:         false,
		catalog.TicketQuestionKindCheckbox:     false,
	}
	for kind, want := range offers {
		if got := kind.OffersOptions(); got != want {
			t.Fatalf("%s.OffersOptions() = %v, want %v", kind, got, want)
		}
	}
}

func TestParseTicketQuestionTiming(t *testing.T) {
	t.Parallel()

	// Both timings parse today even though v1 only ever writes at_checkout: the
	// field exists so the Organization's choice can be honoured later without
	// migrating Answers.
	for _, raw := range []string{"at_checkout", "after_purchase"} {
		if _, ok := catalog.ParseTicketQuestionTiming(raw); !ok {
			t.Fatalf("ParseTicketQuestionTiming(%q) refused a timing the schema stores", raw)
		}
	}
	if _, ok := catalog.ParseTicketQuestionTiming("at_door"); ok {
		t.Fatal("ParseTicketQuestionTiming accepted an unknown timing")
	}
}

func TestTicketQuestionLabelIsKeptAsCoined(t *testing.T) {
	t.Parallel()

	// A Ticket Question's label is the Organization's own words, read as coined
	// in every Locale like a Custom Tag. Surrounding whitespace is a typing
	// accident and goes; everything inside — casing, punctuation, accents, double
	// spaces — is what they wrote and stays.
	label, ok := catalog.NormalizeTicketQuestionLabel("  ¿Talla de  Camiseta?  ")
	if !ok {
		t.Fatal("NormalizeTicketQuestionLabel refused a perfectly good label")
	}
	if label != "¿Talla de  Camiseta?" {
		t.Fatalf("label = %q, want the words as coined with only the outer whitespace gone", label)
	}
}

func TestTicketQuestionLabelRefusesEmptyAndOverlong(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "   ", "\t\n"} {
		if _, ok := catalog.NormalizeTicketQuestionLabel(raw); ok {
			t.Fatalf("NormalizeTicketQuestionLabel(%q) accepted a blank label", raw)
		}
	}
	overlong := strings.Repeat("a", catalog.MaxTicketQuestionLabelLength+1)
	if _, ok := catalog.NormalizeTicketQuestionLabel(overlong); ok {
		t.Fatal("NormalizeTicketQuestionLabel accepted a label over the cap")
	}
	atCap := strings.Repeat("a", catalog.MaxTicketQuestionLabelLength)
	if _, ok := catalog.NormalizeTicketQuestionLabel(atCap); !ok {
		t.Fatal("NormalizeTicketQuestionLabel refused a label exactly at the cap")
	}
}

// The cap is counted in characters rather than bytes, so an Organization writing
// Spanish is not given a shorter field than one writing English.
func TestTicketQuestionLabelCapCountsCharactersNotBytes(t *testing.T) {
	t.Parallel()

	accented := strings.Repeat("á", catalog.MaxTicketQuestionLabelLength)
	if _, ok := catalog.NormalizeTicketQuestionLabel(accented); !ok {
		t.Fatal("a label of accented characters at the cap was refused; the cap is counting bytes")
	}
}

func TestTicketQuestionKindIsFrozenOnceAnyAnswerExists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		from         catalog.TicketQuestionKind
		to           catalog.TicketQuestionKind
		answersExist bool
		wantFrozen   bool
	}{
		{
			name:         "a different kind is refused once an Answer exists",
			from:         catalog.TicketQuestionKindShortText,
			to:           catalog.TicketQuestionKindSingleChoice,
			answersExist: true,
			wantFrozen:   true,
		},
		{
			name:         "a different kind is allowed while nothing has answered",
			from:         catalog.TicketQuestionKindShortText,
			to:           catalog.TicketQuestionKindSingleChoice,
			answersExist: false,
			wantFrozen:   false,
		},
		{
			name:         "restating the same kind is never a change, Answers or not",
			from:         catalog.TicketQuestionKindSingleChoice,
			to:           catalog.TicketQuestionKindSingleChoice,
			answersExist: true,
			wantFrozen:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := catalog.TicketQuestionKindFrozen(tc.from, tc.to, tc.answersExist)
			if got != tc.wantFrozen {
				t.Fatalf("TicketQuestionKindFrozen(%s, %s, %v) = %v, want %v",
					tc.from, tc.to, tc.answersExist, got, tc.wantFrozen)
			}
		})
	}
}

func TestOptionCapIsTwentyAndCountsLiveOptionsOnly(t *testing.T) {
	t.Parallel()

	if catalog.MaxTicketQuestionOptions != 20 {
		t.Fatalf("MaxTicketQuestionOptions = %d, want 20", catalog.MaxTicketQuestionOptions)
	}
	if catalog.TicketQuestionOptionCapReached(19) {
		t.Fatal("a twentieth Option was refused")
	}
	if !catalog.TicketQuestionOptionCapReached(20) {
		t.Fatal("a twenty-first Option was accepted")
	}
}
