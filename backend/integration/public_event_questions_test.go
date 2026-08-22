package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The Storefront draws its checkout answer section from the public Event payload
// and from NOTHING ELSE (#311). No second feature flag on the frontend, no
// separate fetch: the one place the answer to "is this feature on, and what does
// this Ticket Type ask" lives is this payload, so a client cannot disagree with
// the backend about either (ADR 0045).

// publicTicketQuestions decodes the answer section as the event page receives
// it: the questions per Ticket Type, narrowed to what a public payload may say.
type publicTicketQuestions struct {
	TicketTypes []struct {
		ID              string `json:"id"`
		TicketQuestions []struct {
			ID       string `json:"id"`
			Label    string `json:"label"`
			Kind     string `json:"kind"`
			Required bool   `json:"required"`
			Options  []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
			} `json:"options"`
		} `json:"ticket_questions"`
	} `json:"ticket_types"`
}

func publicEventQuestions(t *testing.T, env *testEnv, eventSlug string) publicTicketQuestions {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events/"+eventSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page publicTicketQuestions
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode public event: %v", err)
	}
	return page
}

// The questions reach the page as coined, in the order they are asked, with
// their Options' identities — everything a form needs and nothing else.
func TestPublicEventPageCarriesTheCheckoutQuestions(t *testing.T) {
	env := setupTest(t)
	_, _, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	page := publicEventQuestions(t, env, "answer-fest")
	if len(page.TicketTypes) != 1 || page.TicketTypes[0].ID != ticketTypeID {
		t.Fatalf("public event has %d Ticket Types", len(page.TicketTypes))
	}
	questions := page.TicketTypes[0].TicketQuestions
	if len(questions) != 2 {
		t.Fatalf("public Ticket Type asks %d questions, want 2", len(questions))
	}
	if questions[0].ID != size.ID || questions[0].Label != "T-shirt size" || questions[0].Kind != "short_text" {
		t.Fatalf("first question = %+v", questions[0])
	}
	// Required reaches the page so the field can be MARKED. Nothing on the
	// Storefront may turn the mark into a gate (ADR 0044); that it travels at all
	// is what lets the page say "required" honestly while still letting the buyer
	// past.
	if !questions[0].Required {
		t.Fatal("the required question did not arrive marked required")
	}
	if len(questions[0].Options) != 0 {
		t.Fatalf("a short_text question offered %d options", len(questions[0].Options))
	}
	if questions[1].ID != meal.ID || len(questions[1].Options) != 2 {
		t.Fatalf("second question = %+v", questions[1])
	}
	if questions[1].Options[0].Label != "Chicken" || questions[1].Options[1].Label != "Vegetarian" {
		t.Fatalf("options = %+v", questions[1].Options)
	}
}

// A retired question and a retired Option are gone from the checkout form. This
// is a NEW list; "retired, never deleted" is a promise about the Answers that
// already chose them, not an instruction to keep offering them.
func TestPublicEventPageOmitsRetiredQuestionsAndOptions(t *testing.T) {
	env := setupTest(t)
	sessionID, eventID, ticketTypeID, size, meal := checkoutQuestionFixture(t, env)

	resp, body := env.deleteJSON(t, questionPath(eventID, ticketTypeID, size.ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire question status=%d error=%+v", resp.StatusCode, body.Error)
	}
	resp, body = env.deleteJSON(t, optionPath(eventID, ticketTypeID, meal.ID, meal.Options[0].ID), nil, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retire option status=%d error=%+v", resp.StatusCode, body.Error)
	}

	questions := publicEventQuestions(t, env, "answer-fest").TicketTypes[0].TicketQuestions
	if len(questions) != 1 || questions[0].ID != meal.ID {
		t.Fatalf("public questions = %+v, want the meal question alone", questions)
	}
	if len(questions[0].Options) != 1 || questions[0].Options[0].Label != "Vegetarian" {
		t.Fatalf("public options = %+v, want the live one alone", questions[0].Options)
	}
}

// With the flag closed the public Event page says nothing about Ticket
// Questions at all, so a Storefront reading it has no answer section to draw.
// This is how the feature ships, and the page is otherwise identical to the one
// that existed before it (ADR 0045).
func TestPublicEventPageSaysNothingAboutQuestionsWhileTheFlagIsOff(t *testing.T) {
	env := setupTest(t)
	checkoutQuestionFixture(t, env)
	sharedApp.CatalogService.WithTicketQuestions(false)
	sharedApp.SalesService.WithTicketQuestions(false)

	questions := publicEventQuestions(t, env, "answer-fest").TicketTypes[0].TicketQuestions
	if len(questions) != 0 {
		t.Fatalf("public event carried %d questions with the flag closed, want 0", len(questions))
	}
}
