package catalog

import "time"

// HolderAssignment is what a Ticket's row says about its assignment, in plain
// values so any read can supply it — the Holder List's roster row and the
// Customer Dossier's ticket row alike (#637). A NULL column is "" or nil.
type HolderAssignment struct {
	HolderEmail     string
	HolderFirstName string
	HolderLastName  string
	// HolderCustomerID is the Customer whose acceptance the Ticket records
	// (`tickets.holder_customer_id`).
	HolderCustomerID string
	AssignedAt       *time.Time
	AcceptedAt       *time.Time
	// HolderAddressPurgedAt is migration 081's marker: when the retention purge
	// took an address nobody accepted.
	HolderAddressPurgedAt *time.Time
}

// HolderDisclosure is what an Organization may be told about a Ticket's Holder.
// The zero value — State "" — means nothing about assignment is disclosed.
type HolderDisclosure struct {
	State         TicketAssignmentState
	NeverAccepted bool
	// Filled only when State is TicketAccepted.
	HolderFirstName string
	HolderLastName  string
	HolderEmail     string
	// HolderCustomerID is what links the accepted Holder's Customer Dossier
	// (#640), and AcceptedAt when they accepted. Filled on the same terms as the
	// name: a link to a person is as much a disclosure of them as their name.
	HolderCustomerID string
	AcceptedAt       *time.Time
}

// DiscloseHolder is THE rule for what a Ticket discloses about its Holder to the
// Organization (ADR 0047; the never-accepted presentation is #334). Every
// Organization-facing read of a Holder — the Holder List, the Holder Export, the
// Customer Dossier — comes through here, so none can disclose more than another.
//
//   - With TICKET_ASSIGNMENT_ENABLED closed, nothing: the zero value, so every
//     `omitempty` field stays off the wire as on a build without the feature
//     (ADR 0045).
//   - The state is derived through AssignmentState, never re-decided.
//   - ONE presentation-level exception: a Ticket whose unaccepted address the
//     retention purge took reads `assigned` with NeverAccepted set. It is not a
//     fourth state, and AssignmentState still reads such a Ticket `unassigned`
//     everywhere else (the buyer's page). Nothing personal is disclosed: the
//     address is gone by definition.
//   - The Holder's name and address only once ACCEPTED. An address a buyer typed
//     and its owner never accepted has no consent moment behind it, so the
//     Organization is told the Ticket is `assigned` and not to whom. (The buyer's
//     own row shows the address from the moment it is typed — a different reader,
//     and not this rule.)
//
// The repository's holder filter predicates are this rule's query-side twin and
// must agree with it (the holder search and `assignment_state` predicates in
// repository/outstanding_answer_repository.go).
func DiscloseHolder(assignmentEnabled bool, ticket HolderAssignment) HolderDisclosure {
	if !assignmentEnabled {
		return HolderDisclosure{}
	}
	state := AssignmentState(ticket.HolderEmail, ticket.AssignedAt, ticket.AcceptedAt)
	if state != TicketAccepted {
		if ticket.HolderAddressPurgedAt != nil {
			return HolderDisclosure{State: TicketAssigned, NeverAccepted: true}
		}
		return HolderDisclosure{State: state}
	}
	return HolderDisclosure{
		State:           state,
		HolderFirstName: ticket.HolderFirstName,
		HolderLastName:  ticket.HolderLastName,
		HolderEmail:     ticket.HolderEmail,
		// The Customer id and the acceptance time only here, once accepted.
		HolderCustomerID: ticket.HolderCustomerID,
		AcceptedAt:       ticket.AcceptedAt,
	}
}
