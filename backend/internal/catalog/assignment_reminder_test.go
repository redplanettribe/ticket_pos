package catalog

import (
	"testing"
	"time"
)

// The rationing of the Assignment Reminder (#362, parent #361, ADR 0051):
// table-driven over every fact MayRemindAssignment reads, with the happy case
// first so that each refusal below is one clause away from it.

func assignmentRemindNow() AssignmentReminderInputs {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	return AssignmentReminderInputs{
		SaleChannel:       SalesChannelOnline,
		SaleStatus:        TicketSaleStatusActive,
		SaleCreatedAt:     now.Add(-2 * 24 * time.Hour),
		TicketCount:       3,
		UnassignedTickets: 2,
		EventStartsAt:     now.Add(30 * 24 * time.Hour),
		Now:               now,
		RemindersSent:     0,
		LastRemindedAt:    time.Time{},
	}
}

func TestMayRemindAssignment(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(in *AssignmentReminderInputs)
		want   bool
		why    string
	}{
		{
			name:   "a day-old online sale with unassigned tickets on an upcoming event",
			mutate: func(*AssignmentReminderInputs) {},
			want:   true,
			why:    "this is exactly what the mail is for",
		},
		{
			name:   "an import sale",
			mutate: func(in *AssignmentReminderInputs) { in.SaleChannel = "import" },
			want:   true,
			why:    "ADR 0055 widened the audience: the imported buyer holds Ticket 1 and nobody is named for the rest",
		},
		{
			name:   "an in-person sale",
			mutate: func(in *AssignmentReminderInputs) { in.SaleChannel = "in_person" },
			want:   false,
			why:    "a door sale has no buyer surface to assign from, so the reminder would point at nothing",
		},
		{
			name:   "a reversed sale",
			mutate: func(in *AssignmentReminderInputs) { in.SaleStatus = "reversed" },
			want:   false,
			why:    "a reversed Sale's Tickets have ceased to exist",
		},
		{
			name:   "a single-ticket sale",
			mutate: func(in *AssignmentReminderInputs) { in.TicketCount = 1; in.UnassignedTickets = 0 },
			want:   false,
			why:    "the one Ticket is the buyer's own Self-held Ticket; there is nobody to name",
		},
		{
			name:   "every ticket assigned",
			mutate: func(in *AssignmentReminderInputs) { in.UnassignedTickets = 0 },
			want:   false,
			why:    "the buyer has acted, and the platform stops",
		},
		{
			name:   "a sale made an hour ago",
			mutate: func(in *AssignmentReminderInputs) { in.SaleCreatedAt = in.Now.Add(-time.Hour) },
			want:   false,
			why:    "a reminder an hour after the receipt is the receipt again",
		},
		{
			name:   "a sale made exactly 24 hours ago",
			mutate: func(in *AssignmentReminderInputs) { in.SaleCreatedAt = in.Now.Add(-AssignmentReminderMinSaleAge) },
			want:   true,
			why:    "the day is the threshold, inclusive",
		},
		{
			name:   "a sale with no creation time",
			mutate: func(in *AssignmentReminderInputs) { in.SaleCreatedAt = time.Time{} },
			want:   false,
			why:    "a zero time is not 'a very long time ago'; it is a row this rule does not understand",
		},
		{
			name:   "the event has started",
			mutate: func(in *AssignmentReminderInputs) { in.EventStartsAt = in.Now.Add(-time.Minute) },
			want:   false,
			why:    "the choice no longer matters",
		},
		{
			name:   "the event starts this instant",
			mutate: func(in *AssignmentReminderInputs) { in.EventStartsAt = in.Now },
			want:   false,
			why:    "the doors opening is the moment it goes quiet, not a moment after",
		},
		{
			name:   "an unscheduled event",
			mutate: func(in *AssignmentReminderInputs) { in.EventStartsAt = time.Time{} },
			want:   false,
			why:    "silence is the safe direction for an Event nobody has placed in time",
		},
		{
			name: "reminded six days ago",
			mutate: func(in *AssignmentReminderInputs) {
				in.RemindersSent = 1
				in.LastRemindedAt = in.Now.Add(-6 * 24 * time.Hour)
			},
			want: false,
			why:  "inside the week",
		},
		{
			name: "reminded exactly seven days ago",
			mutate: func(in *AssignmentReminderInputs) {
				in.RemindersSent = 1
				in.LastRemindedAt = in.Now.Add(-AssignmentReminderInterval)
			},
			want: true,
			why:  "the week is the threshold, inclusive",
		},
		{
			name: "reminded twice already",
			mutate: func(in *AssignmentReminderInputs) {
				in.RemindersSent = MaxAssignmentReminders
				in.LastRemindedAt = in.Now.Add(-60 * 24 * time.Hour)
			},
			want: false,
			why:  "no passage of time buys a third",
		},
		{
			name: "a count without a last send",
			mutate: func(in *AssignmentReminderInputs) {
				in.RemindersSent = 1
				in.LastRemindedAt = time.Time{}
			},
			want: true,
			why:  "the ledger is the count; an absent timestamp beside a count is tolerated as never-in-the-window",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := assignmentRemindNow()
			tc.mutate(&in)
			if got := MayRemindAssignment(in); got != tc.want {
				t.Fatalf("MayRemindAssignment = %v, want %v: %s", got, tc.want, tc.why)
			}
		})
	}
}

func TestMayRemindAssignmentRefusesTheZeroValue(t *testing.T) {
	if MayRemindAssignment(AssignmentReminderInputs{}) {
		t.Fatal("a zero-valued input must be refused, so an unchecked candidate can never pass by accident")
	}
}

func TestAssignmentReminderCandidateInputsCarryEveryFact(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	c := AssignmentReminderCandidate{
		TicketSaleID:      "sale",
		SaleChannel:       SalesChannelOnline,
		SaleStatus:        TicketSaleStatusActive,
		SaleCreatedAt:     now.Add(-48 * time.Hour),
		TicketCount:       4,
		UnassignedTickets: 1,
		EventStartsAt:     now.Add(time.Hour),
		RemindersSent:     1,
		LastRemindedAt:    now.Add(-8 * 24 * time.Hour),
	}
	in := c.Inputs(now)
	if !MayRemindAssignment(in) {
		t.Fatal("a candidate one week past its first reminder, on an event an hour away, is due its second")
	}
	if in.Now != now || in.TicketCount != 4 || in.UnassignedTickets != 1 || in.RemindersSent != 1 {
		t.Fatalf("Inputs dropped a fact: %+v", in)
	}
}
