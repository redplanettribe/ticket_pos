package server

import (
	"fmt"
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
// added tomorrow inherits it. It fails closed: every wildcard of a staff or
// operator route must be either an id in these tables or a non-id in the
// exemption tables below, or registering the route panics and the server does
// not start (RequireDeclaredPathIDs). TestEveryRouteRefusesAMalformedPathIDAsNotFound
// then walks the registered route table and holds every id wildcard of every
// route to the answer a well-formed unknown id gets.

// staffPathIDs are the id wildcards under /api/v1/staff.
var staffPathIDs = pathIDs{
	"events/{id}":                 func(string) error { return catalog.ErrEventNotFound() },
	"events/{eventID}":            func(string) error { return catalog.ErrEventNotFound() },
	"ticket-types/{ticketTypeId}": func(string) error { return catalog.ErrTicketTypeNotFound() },
	"questions/{questionId}":      func(string) error { return catalog.ErrTicketQuestionNotFound() },
	"options/{optionId}":          func(string) error { return catalog.ErrTicketQuestionOptionNotFound() },
	"tickets/{ticketId}":          func(string) error { return catalog.ErrTicketNotFound() },
	"answers/{questionId}":        func(string) error { return catalog.ErrTicketQuestionNotFound() },
	// Removing an Answer reports the Answer missing, not the question: an
	// unknown question id simply has no Answer on the Ticket to remove.
	"DELETE answers/{questionId}": func(string) error { return catalog.ErrAnswerNotFound() },
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

// staffNonIDWildcards are the wildcards under /api/v1/staff that the guard
// deliberately does not check, keyed as pathIDs are. Each says why.
var staffNonIDWildcards = map[string]bool{
	// A Sale id, but the one route under it is a read whose answer for a Sale
	// it cannot find is an empty list, not a 404, and its service gives a
	// malformed id that same empty list itself: malformed already equals
	// unknown there, and a 404 from the guard would break that.
	"ticket-sales/{ticketSaleId}": true,
}

// operatorNonIDWildcards are the wildcards under /api/v1/operator that are
// not ids at all, keyed as pathIDs are. Each says why.
var operatorNonIDWildcards = map[string]bool{
	// A Sale Confirmation reference, the human-readable code a buyer quotes,
	// not a uuid; the service looks it up as text.
	"sales/{confirmationRef}": true,
	// A legal document's name ("terms", "policy"), a closed set the handler
	// validates.
	"documents/{document}": true,
	// The same document name, on the Customer and Staff acceptance browsers.
	"customers/{document}": true,
	"staff/{document}":     true,
	// An edition's version number, an integer the handler parses.
	"publications/{version}": true,
	// The opaque keyed digest of a staff member's email that the Staff legal
	// record and its Evidence Pack are looked up by, not a uuid.
	"staff/{digest}": true,
}

// RequireDeclaredPathIDs returns mux wrapped so that registering a staff or
// operator route panics unless every wildcard in its path is declared: an id in
// that namespace's pathIDs table, or a non-id in its exemption table.
//
// It makes the guard fail closed. Without it, a route added with an id wildcard
// nobody put in the table would pass a malformed id straight through to a uuid
// column, and nothing would say so until a request 500ed. RegisterRoutes
// registers every route through it, so the server cannot start with such a
// route. Routes outside /api/v1/staff and /api/v1/operator pass through
// unchecked: those namespaces have no guard, and their handlers answer a
// malformed id themselves.
func RequireDeclaredPathIDs(mux Router) Router {
	return declaredPathIDsRouter{next: mux}
}

type declaredPathIDsRouter struct{ next Router }

func (d declaredPathIDsRouter) Handle(pattern string, handler http.Handler) {
	mustDeclarePathIDs(pattern)
	d.next.Handle(pattern, handler)
}

func (d declaredPathIDsRouter) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	mustDeclarePathIDs(pattern)
	d.next.HandleFunc(pattern, handler)
}

// mustDeclarePathIDs panics, naming the route and the wildcard, when a staff or
// operator pattern has a wildcard neither its ids nor its exemptions declare.
func mustDeclarePathIDs(pattern string) {
	method, path, hasMethod := strings.Cut(pattern, " ")
	if !hasMethod {
		method, path = "", pattern
	}
	var ids pathIDs
	var nonIDs map[string]bool
	switch {
	case strings.HasPrefix(path, "/api/v1/staff/"):
		ids, nonIDs = staffPathIDs, staffNonIDWildcards
	case strings.HasPrefix(path, "/api/v1/operator/"):
		ids, nonIDs = operatorPathIDs, operatorNonIDWildcards
	default:
		return
	}
	segments := strings.Split(path, "/")
	for i := 1; i < len(segments); i++ {
		wildcard := segments[i]
		if !strings.HasPrefix(wildcard, "{") || !strings.HasSuffix(wildcard, "}") {
			continue
		}
		key := segments[i-1] + "/" + wildcard
		_, isMethodID := ids[method+" "+key]
		_, isID := ids[key]
		if isMethodID || isID || nonIDs[key] {
			continue
		}
		panic(fmt.Sprintf(
			"route %q: path wildcard %s (keyed %q) is not declared; add it to the "+
				"namespace's pathIDs table in internal/server/path_ids.go with the "+
				"error its unknown id answers, or, if it is not an id, to the "+
				"namespace's non-id exemptions with a comment saying why",
			pattern, wildcard, key))
	}
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
