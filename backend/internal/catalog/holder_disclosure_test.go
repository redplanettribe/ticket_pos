package catalog

import (
	"testing"
	"time"
)

// The Holder Disclosure rule (ADR 0047, #334, #637) at its own seam. The Holder
// List, Holder Export and holder address purge integration suites pin what an
// Organization observes through HTTP; this pins the rule those observations are
// made of, so a second read (the Customer Dossier, #640) is held to the same
// thing without a Holder List fixture.
func TestDiscloseHolder(t *testing.T) {
	assigned := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	accepted := assigned.Add(2 * time.Hour)
	purged := assigned.Add(72 * time.Hour)

	acceptedTicket := HolderAssignment{
		HolderEmail: "carla@example.com", HolderFirstName: "Carla", HolderLastName: "Ruiz",
		AssignedAt: timePtr(assigned), AcceptedAt: timePtr(accepted),
	}
	assignedTicket := HolderAssignment{
		HolderEmail: "carla@example.com", AssignedAt: timePtr(assigned),
	}
	purgedTicket := HolderAssignment{HolderAddressPurgedAt: timePtr(purged)}
	acceptedAndMarked := acceptedTicket
	acceptedAndMarked.HolderAddressPurgedAt = timePtr(purged)

	for _, tc := range []struct {
		name    string
		enabled bool
		ticket  HolderAssignment
		want    HolderDisclosure
	}{
		// ADR 0045: a closed flag discloses nothing about assignment at all.
		{"closed flag, accepted", false, acceptedTicket, HolderDisclosure{}},
		{"closed flag, purged", false, purgedTicket, HolderDisclosure{}},
		{"nothing given", true, HolderAssignment{}, HolderDisclosure{State: TicketUnassigned}},
		// An address nobody accepted is reported as a state, never as a person.
		{"assigned, not accepted", true, assignedTicket, HolderDisclosure{State: TicketAssigned}},
		{"accepted", true, acceptedTicket, HolderDisclosure{
			State: TicketAccepted, HolderFirstName: "Carla", HolderLastName: "Ruiz",
			HolderEmail: "carla@example.com",
		}},
		{"purged before acceptance", true, purgedTicket, HolderDisclosure{
			State: TicketAssigned, NeverAccepted: true,
		}},
		// Acceptance outranks the purge marker.
		{"accepted with a purge marker", true, acceptedAndMarked, HolderDisclosure{
			State: TicketAccepted, HolderFirstName: "Carla", HolderLastName: "Ruiz",
			HolderEmail: "carla@example.com",
		}},
	} {
		if got := DiscloseHolder(tc.enabled, tc.ticket); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
