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
// operator route must be an id in these tables, a uuid deliberately left to its
// service, or a non-id in the exemption tables below, or registering the route
// panics and the server does not start (RequireDeclaredPathIDs). TestEveryRouteRefusesAMalformedPathIDAsNotFound
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

// staffUnguardedIDs are uuid wildcards under /api/v1/staff that the guard
// deliberately leaves to their service, keyed as pathIDs are. Each says why.
var staffUnguardedIDs = map[string]bool{
	// A Sale id, but the one route under it is a read whose answer for a Sale
	// it cannot find is an empty list, not a 404, and its service gives a
	// malformed id that same empty list itself: malformed already equals
	// unknown there, and a 404 from the guard would break that.
	"ticket-sales/{ticketSaleId}": true,
}

// nonIDWildcard is a wildcard that is not an id at all, with a well-formed
// example of what it holds. The example documents the wildcard and is the
// value the integration sweep fills it with (NonIDPathWildcard).
type nonIDWildcard struct{ example string }

// staffNonIDWildcards are the wildcards under /api/v1/staff that are not ids
// at all, keyed as pathIDs are. There are none today.
var staffNonIDWildcards = map[string]nonIDWildcard{}

// operatorNonIDWildcards are the wildcards under /api/v1/operator that are
// not ids at all, keyed as pathIDs are. Each says why.
var operatorNonIDWildcards = map[string]nonIDWildcard{
	// A Sale Confirmation reference, the human-readable code a buyer quotes,
	// not a uuid; the service looks it up as text.
	"sales/{confirmationRef}": {example: "NOSUCHREF"},
	// A legal document's name ("terms", "policy"), a closed set the handler
	// validates.
	"documents/{document}": {example: "policy"},
	// The same document name, on the Customer and Staff acceptance browsers.
	"customers/{document}": {example: "policy"},
	"staff/{document}":     {example: "policy"},
	// An edition's version number, an integer the handler parses.
	"publications/{version}": {example: "1"},
	// The opaque keyed digest of a staff member's email that the Staff legal
	// record and its Evidence Pack are looked up by, not a uuid.
	"staff/{digest}": {example: strings.Repeat("0", 64)},
}

// pathIDNamespace is one guarded namespace's declarations: its ids, the uuids
// it leaves to their service, and its non-id wildcards.
type pathIDNamespace struct {
	ids          pathIDs
	unguardedIDs map[string]bool
	nonIDs       map[string]nonIDWildcard
}

// guardedNamespace returns the declarations for a route path, or nil for a
// path outside /api/v1/staff and /api/v1/operator. Those namespaces have no
// guard, and their handlers answer a malformed id themselves.
func guardedNamespace(path string) *pathIDNamespace {
	switch {
	case strings.HasPrefix(path, "/api/v1/staff/"):
		return &pathIDNamespace{ids: staffPathIDs, unguardedIDs: staffUnguardedIDs, nonIDs: staffNonIDWildcards}
	case strings.HasPrefix(path, "/api/v1/operator/"):
		return &pathIDNamespace{ids: operatorPathIDs, nonIDs: operatorNonIDWildcards}
	}
	return nil
}

// pathWildcard is one wildcard of a route pattern.
type pathWildcard struct {
	// segment is the wildcard as the pattern writes it: "{id}", "{path...}".
	segment string
	// name is what r.PathValue reads it by: "id", "path".
	name string
	// key is the literal segment in front of it and the wildcard, which is how
	// every table here is keyed: "events/{id}".
	key string
	// rest is whether it is a "{name...}" wildcard, which matches the whole
	// remainder of the path and so can never be one id.
	rest bool
}

// routeWildcards splits a route pattern into its method ("" when it has none),
// its path and its wildcards, left to right. It is the one walk of a pattern
// here: the registration check and the request guard both read it.
//
// "{$}" is not a wildcard. It only anchors a pattern to the end of the path
// and matches nothing, so there is nothing to declare or check.
func routeWildcards(pattern string) (method, path string, wildcards []pathWildcard) {
	method, path, hasMethod := strings.Cut(pattern, " ")
	if !hasMethod {
		method, path = "", pattern
	}
	segments := strings.Split(path, "/")
	for i := 1; i < len(segments); i++ {
		segment := segments[i]
		if segment == "{$}" || !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		name, rest := strings.CutSuffix(strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}"), "...")
		wildcards = append(wildcards, pathWildcard{
			segment: segment,
			name:    name,
			key:     segments[i-1] + "/" + segment,
			rest:    rest,
		})
	}
	return method, path, wildcards
}

// notFoundFor returns the not-found error of the id keyed key under method:
// the method-specific entry where there is one, else the bare one.
func (ids pathIDs) notFoundFor(method, key string) (func(value string) error, bool) {
	if notFound, ok := ids[method+" "+key]; ok {
		return notFound, true
	}
	notFound, ok := ids[key]
	return notFound, ok
}

// RequireDeclaredPathIDs returns mux wrapped so that registering a staff or
// operator route panics unless every wildcard in its path is declared: an id in
// that namespace's pathIDs table, a uuid it leaves to its service, or a non-id
// in its exemption table.
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
// operator pattern has a wildcard its namespace does not declare.
func mustDeclarePathIDs(pattern string) {
	method, path, wildcards := routeWildcards(pattern)
	ns := guardedNamespace(path)
	if ns == nil {
		return
	}
	for _, wc := range wildcards {
		_, isID := ns.ids.notFoundFor(method, wc.key)
		_, isNonID := ns.nonIDs[wc.key]
		switch {
		case wc.rest && (isID || ns.unguardedIDs[wc.key]):
			panic(fmt.Sprintf(
				"route %q: path wildcard %s (keyed %q) matches the rest of the path, "+
					"so it can never be one id; take it out of the namespace's id tables "+
					"in internal/server/path_ids.go and declare it in its non-id exemptions",
				pattern, wc.segment, wc.key))
		case isID, isNonID, ns.unguardedIDs[wc.key]:
			continue
		case wc.rest:
			panic(fmt.Sprintf(
				"route %q: path wildcard %s (keyed %q) is not declared. It matches the "+
					"rest of the path, so it can never be one id the guard could check; "+
					"declare it in the namespace's non-id exemptions in "+
					"internal/server/path_ids.go with a comment saying why",
				pattern, wc.segment, wc.key))
		default:
			panic(fmt.Sprintf(
				"route %q: path wildcard %s (keyed %q) is not declared; add it to the "+
					"namespace's pathIDs table in internal/server/path_ids.go with the "+
					"error its unknown id answers, or, if it is not an id, to the "+
					"namespace's non-id exemptions with a comment saying why",
				pattern, wc.segment, wc.key))
		}
	}
}

// NonIDPathWildcard reports how the guard declares the wildcard named name in
// a route pattern. guarded is whether the pattern is in a guarded namespace at
// all (staff or operator); nonID is whether that namespace declares the
// wildcard not an id, and example is then a well-formed value for it.
//
// It is exported for the integration sweep, which probes every id wildcard of
// every route and must read which ones are not ids from here rather than keep
// a second copy of these tables.
func NonIDPathWildcard(pattern, name string) (example string, nonID, guarded bool) {
	_, path, wildcards := routeWildcards(pattern)
	ns := guardedNamespace(path)
	if ns == nil {
		return "", false, false
	}
	for _, wc := range wildcards {
		if wc.name != name {
			continue
		}
		if declared, ok := ns.nonIDs[wc.key]; ok {
			return declared.example, true, true
		}
		break
	}
	return "", false, true
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

// notFoundForMalformed returns the not-found error of the leftmost id wildcard
// in r's matched pattern whose value is not a UUID, or nil.
func (ids pathIDs) notFoundForMalformed(r *http.Request) error {
	_, _, wildcards := routeWildcards(r.Pattern)
	for _, wc := range wildcards {
		notFound, isID := ids.notFoundFor(r.Method, wc.key)
		if !isID {
			continue
		}
		if value := r.PathValue(wc.name); !catalog.IsUUID(value) {
			return notFound(value)
		}
	}
	return nil
}
