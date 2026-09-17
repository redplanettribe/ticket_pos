package catalog

import (
	"reflect"
	"testing"
	"time"
)

// What a Ticket discloses about its Holder to the Organization (ADR 0047, #334,
// #637), tested at its own seam. The Holder List, Holder Export and holder
// address purge integration suites pin what an Organization observes through
// HTTP; this pins the rule those observations are made of, so a second read (the Customer Dossier, #640) is held to the same
// thing without a Holder List fixture.
func TestDiscloseHolder(t *testing.T) {
	assigned := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	accepted := assigned.Add(2 * time.Hour)
	purged := assigned.Add(72 * time.Hour)

	acceptedTicket := HolderAssignment{
		HolderEmail: "carla@example.com", HolderFirstName: "Carla", HolderLastName: "Ruiz",
		HolderCustomerID: "cus-carla",
		AssignedAt:       timePtr(assigned), AcceptedAt: timePtr(accepted),
	}
	assignedTicket := HolderAssignment{
		HolderEmail: "carla@example.com", AssignedAt: timePtr(assigned),
	}
	// Migration 080 refuses a Customer id beside an unaccepted assignment, and
	// the rule refuses to pass one on regardless (#640): the id is what links a
	// person's Customer Dossier, and a link to somebody nobody accepted would
	// disclose them as surely as their name.
	assignedWithCustomer := assignedTicket
	assignedWithCustomer.HolderCustomerID = "cus-carla"
	assignedWithCustomer.HolderFirstName = "Carla"
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
		{"assigned, with a customer id and a name beside it", true, assignedWithCustomer, HolderDisclosure{State: TicketAssigned}},
		{"accepted", true, acceptedTicket, HolderDisclosure{
			State: TicketAccepted, HolderFirstName: "Carla", HolderLastName: "Ruiz",
			HolderEmail: "carla@example.com", HolderCustomerID: "cus-carla", AcceptedAt: timePtr(accepted),
		}},
		{"purged before acceptance", true, purgedTicket, HolderDisclosure{
			State: TicketAssigned, NeverAccepted: true,
		}},
		// Acceptance outranks the purge marker.
		{"accepted with a purge marker", true, acceptedAndMarked, HolderDisclosure{
			State: TicketAccepted, HolderFirstName: "Carla", HolderLastName: "Ruiz",
			HolderEmail: "carla@example.com", HolderCustomerID: "cus-carla", AcceptedAt: timePtr(accepted),
		}},
	} {
		if got := DiscloseHolder(tc.enabled, tc.ticket); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
