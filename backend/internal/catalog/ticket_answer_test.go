package catalog_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
)

// answerPtr is the one-liner these table tests need to state "this field was
// sent" apart from "this field was absent", which is the whole shape
// catalog.SubmittedAnswer is built on.
func answerPtr[T any](v T) *T { return &v }

// Every one of the seven kinds accepts the shape it takes, and what it stores is
// what its kind IS — a number canonicalised as a number and a date as a date,
// never as whatever text happened to arrive.
func TestParseAnswerAcceptsEachKindsOwnShape(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		kind      catalog.TicketQuestionKind
		submitted catalog.SubmittedAnswer
		want      catalog.AnswerValue
	}{
		{
			name:      "short_text",
			kind:      catalog.TicketQuestionKindShortText,
			submitted: catalog.SubmittedAnswer{Text: answerPtr("  Medium  ")},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindShortText, Text: "Medium"},
		},
		{
			name:      "long_text",
			kind:      catalog.TicketQuestionKindLongText,
			submitted: catalog.SubmittedAnswer{Text: answerPtr("Coeliac, and no shellfish")},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindLongText, Text: "Coeliac, and no shellfish"},
		},
		{
			name:      "number",
			kind:      catalog.TicketQuestionKindNumber,
			submitted: catalog.SubmittedAnswer{Number: answerPtr(" 3 ")},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindNumber, Number: "3"},
		},
		{
			name:      "number with a fraction",
			kind:      catalog.TicketQuestionKindNumber,
			submitted: catalog.SubmittedAnswer{Number: answerPtr("-12.50")},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindNumber, Number: "-12.50"},
		},
		{
			name:      "date",
			kind:      catalog.TicketQuestionKindDate,
			submitted: catalog.SubmittedAnswer{Date: answerPtr(" 2026-09-01 ")},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindDate, Date: "2026-09-01"},
		},
		{
			name:      "checkbox ticked",
			kind:      catalog.TicketQuestionKindCheckbox,
			submitted: catalog.SubmittedAnswer{Checked: answerPtr(true)},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindCheckbox, Checked: true},
		},
		{
			// FALSE IS AN ANSWER, not an absence. Somebody who read "I will
			// attend the dinner" and left it unticked has said no, and that is a
			// different fact from never having been asked — which is what an
			// Outstanding Answer is.
			name:      "checkbox unticked",
			kind:      catalog.TicketQuestionKindCheckbox,
			submitted: catalog.SubmittedAnswer{Checked: answerPtr(false)},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindCheckbox, Checked: false},
		},
		{
			name:      "single_choice",
			kind:      catalog.TicketQuestionKindSingleChoice,
			submitted: catalog.SubmittedAnswer{OptionIDs: []string{"option-m"}},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindSingleChoice, OptionIDs: []string{"option-m"}},
		},
		{
			// Several Options in ONE Answer: the property multi_choice exists
			// for, and the reason an Answer's chosen Options are rows rather
			// than a column.
			name:      "multi_choice takes several",
			kind:      catalog.TicketQuestionKindMultiChoice,
			submitted: catalog.SubmittedAnswer{OptionIDs: []string{"option-veg", "option-nuts"}},
			want:      catalog.AnswerValue{Kind: catalog.TicketQuestionKindMultiChoice, OptionIDs: []string{"option-veg", "option-nuts"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			value, problem := catalog.ParseAnswer(tc.kind, tc.submitted)
			if problem != catalog.AnswerOK {
				t.Fatalf("problem=%v, want AnswerOK", problem)
			}
			if value.Kind != tc.want.Kind || value.Text != tc.want.Text ||
				value.Number != tc.want.Number || value.Date != tc.want.Date ||
				value.Checked != tc.want.Checked {
				t.Fatalf("value=%+v, want %+v", value, tc.want)
			}
			if len(value.OptionIDs) != len(tc.want.OptionIDs) {
				t.Fatalf("option ids=%v, want %v", value.OptionIDs, tc.want.OptionIDs)
			}
			for i, id := range tc.want.OptionIDs {
				if value.OptionIDs[i] != id {
					t.Fatalf("option ids=%v, want %v", value.OptionIDs, tc.want.OptionIDs)
				}
			}
		})
	}
}

// A value of the wrong shape is REFUSED rather than coerced. A caller sending
// text to a number question believed something about that question that is not
// true, and storing "three" — or quietly dropping it — would let them keep
// believing it.
func TestParseAnswerRefusesWhatDoesNotFitTheKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		kind      catalog.TicketQuestionKind
		submitted catalog.SubmittedAnswer
		want      catalog.AnswerProblem
	}{
		{"text sent to a number question", catalog.TicketQuestionKindNumber,
			catalog.SubmittedAnswer{Text: answerPtr("three")}, catalog.AnswerWrongShape},
		{"number that is not one", catalog.TicketQuestionKindNumber,
			catalog.SubmittedAnswer{Number: answerPtr("three")}, catalog.AnswerNotANumber},
		{"number in exponent form", catalog.TicketQuestionKindNumber,
			catalog.SubmittedAnswer{Number: answerPtr("1e6")}, catalog.AnswerNotANumber},
		{"number with too many digits", catalog.TicketQuestionKindNumber,
			catalog.SubmittedAnswer{Number: answerPtr("1234567890123456")}, catalog.AnswerTooLong},
		{"date that is not one", catalog.TicketQuestionKindDate,
			catalog.SubmittedAnswer{Date: answerPtr("next tuesday")}, catalog.AnswerNotADate},
		// One shape and no near misses. Every producer of a date here is a date
		// picker or an export, all of which emit the padded form, so accepting a
		// second spelling would only widen what the column can be handed.
		{"date without its leading zeros", catalog.TicketQuestionKindDate,
			catalog.SubmittedAnswer{Date: answerPtr("2026-9-1")}, catalog.AnswerNotADate},
		{"date that never happened", catalog.TicketQuestionKindDate,
			catalog.SubmittedAnswer{Date: answerPtr("2026-02-30")}, catalog.AnswerNotADate},
		{"an instant sent to a date question", catalog.TicketQuestionKindDate,
			catalog.SubmittedAnswer{Date: answerPtr("2026-09-01T18:00:00Z")}, catalog.AnswerNotADate},
		{"text sent to a checkbox", catalog.TicketQuestionKindCheckbox,
			catalog.SubmittedAnswer{Text: answerPtr("yes")}, catalog.AnswerWrongShape},
		{"options sent to a text question", catalog.TicketQuestionKindShortText,
			catalog.SubmittedAnswer{OptionIDs: []string{"option-m"}}, catalog.AnswerWrongShape},
		{"text sent to a choice question", catalog.TicketQuestionKindSingleChoice,
			catalog.SubmittedAnswer{Text: answerPtr("Medium")}, catalog.AnswerWrongShape},
		{"two shapes at once", catalog.TicketQuestionKindShortText,
			catalog.SubmittedAnswer{Text: answerPtr("Medium"), Number: answerPtr("3")}, catalog.AnswerWrongShape},
		{"nothing at all", catalog.TicketQuestionKindShortText,
			catalog.SubmittedAnswer{}, catalog.AnswerMissing},
		{"blank text", catalog.TicketQuestionKindShortText,
			catalog.SubmittedAnswer{Text: answerPtr("   ")}, catalog.AnswerMissing},
		{"no option chosen", catalog.TicketQuestionKindSingleChoice,
			catalog.SubmittedAnswer{OptionIDs: []string{}}, catalog.AnswerMissing},
		{"two options on a single choice", catalog.TicketQuestionKindSingleChoice,
			catalog.SubmittedAnswer{OptionIDs: []string{"option-s", "option-m"}}, catalog.AnswerOneOptionOnly},
		{"the same option twice", catalog.TicketQuestionKindMultiChoice,
			catalog.SubmittedAnswer{OptionIDs: []string{"option-veg", "option-veg"}}, catalog.AnswerDuplicateOption},
		{"more options than the question could ever offer", catalog.TicketQuestionKindMultiChoice,
			catalog.SubmittedAnswer{OptionIDs: distinctIDs(catalog.MaxTicketQuestionOptions + 1)}, catalog.AnswerTooManyOptions},
		{"overlong short text", catalog.TicketQuestionKindShortText,
			catalog.SubmittedAnswer{Text: answerPtr(repeatString("a", catalog.MaxShortTextAnswerLength+1))}, catalog.AnswerTooLong},
		{"overlong long text", catalog.TicketQuestionKindLongText,
			catalog.SubmittedAnswer{Text: answerPtr(repeatString("a", catalog.MaxLongTextAnswerLength+1))}, catalog.AnswerTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, problem := catalog.ParseAnswer(tc.kind, tc.submitted); problem != tc.want {
				t.Fatalf("problem=%v, want %v", problem, tc.want)
			}
		})
	}
}

// long_text holds more than short_text, and the cap counts CHARACTERS: an
// answer written with accents must not buy a shorter field than one without.
func TestShortTextAnswerIsShorterThanLongTextAndCountsCharacters(t *testing.T) {
	t.Parallel()

	if catalog.MaxShortTextAnswerLength >= catalog.MaxLongTextAnswerLength {
		t.Fatal("short_text must hold less than long_text")
	}
	atCap := repeatString("é", catalog.MaxShortTextAnswerLength)
	if _, problem := catalog.ParseAnswer(catalog.TicketQuestionKindShortText,
		catalog.SubmittedAnswer{Text: &atCap}); problem != catalog.AnswerOK {
		t.Fatalf("problem=%v at exactly the cap, want AnswerOK", problem)
	}
}

// The edit window: answerable and re-answerable until the Event STARTS, refused
// once it has, and refused on a Ticket whose Ticket Sale is reversed.
func TestAnswerWindow(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 1, 19, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		saleStatus string
		startsAt   *time.Time
		now        time.Time
		want       catalog.AnswerRefusal
	}{
		{"open well before the doors", "active", &start, start.Add(-48 * time.Hour), catalog.AnswerWindowOpen},
		{"open a second before the doors", "active", &start, start.Add(-time.Second), catalog.AnswerWindowOpen},
		{"closed exactly as the doors open", "active", &start, start, catalog.AnswerRefusedEventStarted},
		{"closed after the doors open", "active", &start, start.Add(time.Hour), catalog.AnswerRefusedEventStarted},
		// An Event with no start yet has not started. A draft Event's Ticket
		// Type cannot have sold anything, so this is a shape rather than a
		// situation — but a nil start read as "started" would freeze every
		// Answer under it, which is the wrong way to be wrong.
		{"an Event with no start has not started", "active", nil, start, catalog.AnswerWindowOpen},
		{"reversed before the doors", "reversed", &start, start.Add(-48 * time.Hour), catalog.AnswerRefusedSaleReversed},
		// Reversal is reported ahead of the clock when both are true: the Ticket
		// does not stand at all, which is the more fundamental fact and the more
		// useful thing to tell somebody.
		{"reversed and started", "reversed", &start, start.Add(time.Hour), catalog.AnswerRefusedSaleReversed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := catalog.AnswerWindow(tc.saleStatus, tc.startsAt, tc.now); got != tc.want {
				t.Fatalf("AnswerWindow=%v, want %v", got, tc.want)
			}
		})
	}
}

func repeatString(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for range n {
		out = append(out, s...)
	}
	return string(out)
}

func distinctIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, "option-"+string(rune('a'+i%26))+repeatString("x", i/26))
	}
	return ids
}
