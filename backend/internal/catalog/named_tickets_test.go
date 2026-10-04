package catalog

import (
	"reflect"
	"testing"
	"time"
)

func TestNamedTicketsApply(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	for _, tc := range []struct {
		name               string
		requires, assigned bool
		startsAt           *time.Time
		want               bool
	}{
		{"required, open, before the doors", true, true, &later, true},
		{"required, open, no start set", true, true, nil, true},
		{"the setting is off", false, true, &later, false},
		{"Ticket Assignment is dark", true, false, &later, false},
		{"the doors opened at this instant", true, true, &now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NamedTicketsApply(tc.requires, tc.assigned, tc.startsAt, now); got != tc.want {
				t.Fatalf("NamedTicketsApply = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOwedByNamedTickets(t *testing.T) {
	asked := []AskedQuestion{
		{ID: "size", TicketTypeID: "ga", Kind: TicketQuestionKindShortText, Required: true},
		{ID: "meal", TicketTypeID: "ga", Kind: TicketQuestionKindShortText},
		{ID: "badge", TicketTypeID: "vip", Kind: TicketQuestionKindShortText, Required: true},
	}
	answered := []HeldAnswer{
		{TicketTypeID: "ga", TicketIndex: 2, TicketQuestionID: "size"},
	}
	tickets := []NamedTicket{
		{TicketTypeID: "vip", TicketIndex: 1, SelfHeld: true},
		{TicketTypeID: "ga", TicketIndex: 1, HolderEmail: "ana@example.com"},
		{TicketTypeID: "ga", TicketIndex: 2, HolderEmail: "ana@example.com"},
		{TicketTypeID: "ga", TicketIndex: 3},
	}

	got := OwedByNamedTickets(tickets, asked, answered)

	want := []OwedTicket{
		// The buyer's own Ticket owes no address, and still owes its Answers.
		{TicketTypeID: "vip", TicketIndex: 1, MissingQuestionIDs: []string{"badge"}},
		// The optional meal question is owed by nobody.
		{TicketTypeID: "ga", TicketIndex: 1, MissingQuestionIDs: []string{"size"}},
		{TicketTypeID: "ga", TicketIndex: 3, HolderEmailMissing: true, MissingQuestionIDs: []string{"size"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("owed = %+v, want %+v", got, want)
	}
}

func TestOwedByNamedTicketsReportsAnAddressAloneWithAnEmptyList(t *testing.T) {
	got := OwedByNamedTickets([]NamedTicket{{TicketTypeID: "ga", TicketIndex: 2}}, nil, nil)
	if len(got) != 1 || !got[0].HolderEmailMissing || got[0].MissingQuestionIDs == nil || len(got[0].MissingQuestionIDs) != 0 {
		t.Fatalf("owed = %+v, want an address and an empty, non-nil question list", got)
	}
}

func TestOwedByNamedTicketsOwesNothingWhenComplete(t *testing.T) {
	got := OwedByNamedTickets(
		[]NamedTicket{{TicketTypeID: "ga", TicketIndex: 1, SelfHeld: true}},
		[]AskedQuestion{{ID: "size", TicketTypeID: "ga", Required: true}},
		[]HeldAnswer{{TicketTypeID: "ga", TicketIndex: 1, TicketQuestionID: "size"}},
	)
	if len(got) != 0 {
		t.Fatalf("owed = %+v, want nothing", got)
	}
}
