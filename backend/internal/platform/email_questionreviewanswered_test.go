package platform

import (
	"strings"
	"testing"
)

// What the question review verdict mail says around its verdict list (#703).
//
// The closing paragraph is its own paragraph: a blank line separates it from
// the last verdict, so it is never read as part of that item's reason.

func questionReviewAnswered(locale Locale) QuestionReviewAnswered {
	return QuestionReviewAnswered{
		To:               "member@example.com",
		Locale:           locale,
		OrganizationName: "Club Andino",
		EventName:        "Carrera del Cotopaxi",
		AnsweredBy:       "operator@example.com",
		Items: []QuestionReviewAnsweredItem{
			{QuestionLabel: "Shirt size", Verdict: "approved"},
			{QuestionLabel: "ID number", Verdict: "refused", Reason: "Not needed"},
		},
	}
}

func TestQuestionReviewAnsweredSeparatesTheClosingParagraphFromTheVerdicts(t *testing.T) {
	for _, tc := range []struct {
		locale  Locale
		closing string
	}{
		{LocaleEN, "Approved questions are asked from now on."},
		{LocaleES, "Las preguntas aprobadas se hacen desde ahora."},
	} {
		t.Run(string(tc.locale), func(t *testing.T) {
			text := questionReviewAnswered(tc.locale).Text()
			// The last verdict ends with its reason, "Not needed".
			want := "Not needed\n\n" + tc.closing
			if !strings.Contains(text, want) {
				t.Fatalf("text = %q, want the last verdict and the closing paragraph separated by a blank line: %q", text, want)
			}
		})
	}
}
