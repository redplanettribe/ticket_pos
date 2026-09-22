package server

import (
	"net/http"
	"strings"

	"github.com/peter/ticket_pos/backend/internal/affiliates"
	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/consent"
	"github.com/peter/ticket_pos/backend/internal/identity"
	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// pathIDs names the UUID wildcards of one namespace's routes, and the error
// each one's resource answers when it is not found.
//
// A wildcard is keyed by the literal segment in front of it and its own name,
// as it is written in the route pattern: "events/{id}". The segment is part of
// the key because a bare name does not say what it names ("{id}" is an Event
// under /events and a Tax Invoice under /invoices). A key prefixed with a
// method ("DELETE assignments/{memberID}") overrides the bare one for that
// method, where one resource's routes answer "not found" about different
// things.
type pathIDs map[string]func(value string) error

// WHY THIS EXISTS: every id in a path reaches a Postgres uuid column, and
// Postgres refuses to compare a uuid with text that is not one. A handler that
// passed the raw path value down turned GET /events/not-a-uuid/holder-list into
// a 500, and dozens of handlers did. A malformed id names a resource that
// cannot exist, so it is refused as that resource's 404 *_NOT_FOUND, the answer
// a well-formed id that names nothing gets (see the api-errors skill: path ids
// are not validation failures, query and body ids are). It is refused BEFORE
// the handler reads its body, because no body can make the id name something.
//
// The rule is enforced here, once, rather than in each handler, so that a route
// added tomorrow inherits it. TestEveryRouteRefusesAMalformedPathIDAsNotFound
// walks the registered route table and fails for any id wildcard of any route
// that answers a malformed id with a 5xx, which is what catches a wildcard
// missing from these tables.

// staffPathIDs are the id wildcards under /api/v1/staff.
var staffPathIDs = pathIDs{
	"events/{id}":                 func(string) error { return catalog.ErrEventNotFound() },
	"events/{eventID}":            func(string) error { return catalog.ErrEventNotFound() },
	"ticket-types/{ticketTypeId}": func(string) error { return catalog.ErrTicketTypeNotFound() },
	"questions/{questionId}":      func(string) error { return catalog.ErrTicketQuestionNotFound() },
	"options/{optionId}":          func(string) error { return catalog.ErrTicketQuestionOptionNotFound() },
	"tickets/{ticketId}":          func(string) error { return catalog.ErrTicketNotFound() },
	"answers/{questionId}":        func(string) error { return catalog.ErrTicketQuestionNotFound() },
	// NOT "ticket-sales/{ticketSaleId}": the one route under it is a read
	// whose answer for a Sale it cannot find is an empty list, not a 404, and
	// its service gives a malformed id that same empty list itself.
	"sales/{saleId}":              func(v string) error { return sales.ErrTicketSaleIDNotFound(v) },
	"sale-imports/{batchId}":      func(v string) error { return sales.ErrImportBatchNotFound(v) },
	"affiliate-links/{linkId}":    func(string) error { return affiliates.ErrAffiliateLinkNotFound() },
	"customers/{customerId}":      func(string) error { return catalog.ErrCustomerNotFoundAtEvent() },
	"question-reviews/{reviewId}": func(string) error { return catalog.ErrQuestionReviewNotFound() },
	"members/{memberID}":          func(string) error { return identity.ErrMemberNotFound() },
	"assignments/{memberID}":      func(string) error { return identity.ErrMemberNotFound() },
	// Removing an assignment reports the assignment missing, not the Member.
	"DELETE assignments/{memberID}": func(string) error { return identity.ErrAssignmentNotFound() },
	"payout-requests/{requestId}":   func(string) error { return sales.ErrPayoutRequestNotFound() },
}

// operatorPathIDs are the id wildcards under /api/v1/operator.
var operatorPathIDs = pathIDs{
	"organizations/{orgID}":         func(string) error { return identity.ErrOrganizationNotFound() },
	"events/{eventID}":              func(string) error { return catalog.ErrEventNotFound() },
	"invoices/{id}":                 func(string) error { return invoicing.ErrInvoiceNotFound() },
	"customers/{customerID}":        func(string) error { return consent.ErrLegalSubjectNotFound() },
	"payout-requests/{requestID}":   func(string) error { return sales.ErrPayoutRequestNotFound() },
	"question-reviews/{reviewID}":   func(string) error { return catalog.ErrQuestionReviewNotFound() },
	"ticket-questions/{questionID}": func(string) error { return catalog.ErrTicketQuestionNotFound() },
}

// requireWellFormedPathIDs refuses a request whose path carries a malformed
// id, reading the matched route's pattern for which wildcards are ids.
//
// It goes INSIDE a namespace's gate, after the caller is authenticated and
// authorised, so a caller the gate refuses hears 401 or 403 as before.
func requireWellFormedPathIDs(ids pathIDs) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := ids.notFoundForMalformed(r); err != nil {
				_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// notFoundForMalformed returns the not-found error of the leftmost id wildcard in r's
// matched pattern whose value is not a UUID, or nil.
func (ids pathIDs) notFoundForMalformed(r *http.Request) error {
	path := r.Pattern
	if _, afterMethod, hasMethod := strings.Cut(path, " "); hasMethod {
		path = afterMethod
	}
	segments := strings.Split(path, "/")
	for i := 1; i < len(segments); i++ {
		wildcard := segments[i]
		if !strings.HasPrefix(wildcard, "{") || !strings.HasSuffix(wildcard, "}") {
			continue
		}
		key := segments[i-1] + "/" + wildcard
		notFound, isID := ids[r.Method+" "+key]
		if !isID {
			notFound, isID = ids[key]
		}
		if !isID {
			continue
		}
		value := r.PathValue(strings.TrimSuffix(strings.TrimPrefix(wildcard, "{"), "}"))
		if !catalog.IsUUID(value) {
			return notFound(value)
		}
	}
	return nil
}
