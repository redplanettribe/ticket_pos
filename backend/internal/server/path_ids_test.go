package server_test

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/server"
)

// The path-id guard fails closed: a staff or operator route whose path carries a
// wildcard that is neither a declared id nor a declared non-id exemption cannot
// be registered, so the server does not start with an id the guard would let
// through to a uuid column. RegisterRoutes registers every route through
// server.RequireDeclaredPathIDs, which is the seam these tests use to register a
// route the real table does not have.

var nothing = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

// registerPanic registers pattern through the guard and returns what it
// panicked with, or nil.
func registerPanic(pattern string, viaFunc bool) (recovered any) {
	defer func() { recovered = recover() }()
	mux := server.RequireDeclaredPathIDs(http.NewServeMux())
	if viaFunc {
		mux.HandleFunc(pattern, nothing)
	} else {
		mux.Handle(pattern, nothing)
	}
	return nil
}

func TestARouteWithAnUndeclaredWildcardCannotBeRegistered(t *testing.T) {
	for _, tc := range []struct {
		pattern  string
		wildcard string
		viaFunc  bool
	}{
		{"GET /api/v1/staff/events/{id}/widgets/{widgetId}", "{widgetId}", false},
		{"DELETE /api/v1/staff/events/{id}/widgets/{widgetId}", "{widgetId}", true},
		{"POST /api/v1/operator/widgets/{widgetID}/polish", "{widgetID}", false},
		// A declared name under a segment it is not declared for is not declared:
		// "{id}" is an Event under events/ and says nothing about gadgets/.
		{"GET /api/v1/operator/gadgets/{id}", "{id}", false},
		// ServeMux takes any run of spaces or tabs after the method, and so
		// does the guard: the route is still in its namespace.
		{"GET\t/api/v1/staff/events/{id}/widgets/{widgetId}", "{widgetId}", false},
		{"GET   /api/v1/staff/events/{id}/widgets/{widgetId}", "{widgetId}", false},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			recovered := registerPanic(tc.pattern, tc.viaFunc)
			if recovered == nil {
				t.Fatal("registered, want a panic")
			}
			message := fmt.Sprint(recovered)
			if !strings.Contains(message, strconv.Quote(tc.pattern)) || !strings.Contains(message, tc.wildcard) {
				t.Fatalf("panic %q does not name the route %q and the wildcard %s", message, tc.pattern, tc.wildcard)
			}
		})
	}
}

func TestADeclaredOrExemptedWildcardRegisters(t *testing.T) {
	for _, pattern := range []string{
		// Declared ids.
		"GET /api/v1/staff/events/{id}/widgets",
		"DELETE /api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId}",
		"GET /api/v1/operator/invoicing/invoices/{id}",
		// Declared non-id exemptions.
		"GET /api/v1/operator/sales/{confirmationRef}",
		"GET /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets",
		// Outside the staff and operator namespaces the guard does not apply.
		"GET /api/v1/customer/widgets/{widgetId}",
		"GET /api/v1/public/organizations/{slug}/widgets/{widgetSlug}",
		// No wildcard at all.
		"GET /api/v1/staff/widgets",
		// "{$}" only anchors the end of a path; it matches nothing and is not a
		// wildcard to declare.
		"GET /api/v1/staff/widgets/{$}",
		"GET /api/v1/staff/events/{id}/{$}",
	} {
		if recovered := registerPanic(pattern, false); recovered != nil {
			t.Errorf("%s panicked: %v", pattern, recovered)
		}
	}
}

// A "{rest...}" wildcard matches the remainder of the path, so it can never be
// one id and the guard can never check it as one. An undeclared one still
// fails closed, but the panic says what it is and where it belongs, rather than
// asking for an id table entry that could never be right.
func TestAnUndeclaredRemainderWildcardIsRefusedAsNeverAnID(t *testing.T) {
	const pattern = "GET /api/v1/staff/events/{id}/files/{path...}"
	recovered := registerPanic(pattern, false)
	if recovered == nil {
		t.Fatal("registered, want a panic")
	}
	message := fmt.Sprint(recovered)
	if !strings.Contains(message, pattern) || !strings.Contains(message, "{path...}") {
		t.Fatalf("panic %q does not name the route and the wildcard", message)
	}
	if !strings.Contains(message, "rest of the path") || !strings.Contains(message, "non-id exemptions") ||
		strings.Contains(message, "pathIDs table") {
		t.Fatalf("panic %q should say the wildcard spans the rest of the path and belongs in the non-id exemptions, not the id table", message)
	}
}

// The integration sweep reads which wildcards are not ids from the guard's own
// exemptions, rather than restating them, and gets a well-formed value for each.
func TestNonIDPathWildcardReadsTheGuardsExemptions(t *testing.T) {
	for _, tc := range []struct {
		pattern, name  string
		nonID, guarded bool
	}{
		// A declared non-id.
		{"GET /api/v1/operator/sales/{confirmationRef}", "confirmationRef", true, true},
		// A declared id.
		{"GET /api/v1/staff/events/{id}/widgets", "id", false, true},
		// A uuid the guard leaves to its service is still an id to probe.
		{"GET /api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets", "ticketSaleId", false, true},
		// Outside the staff and operator namespaces nothing is declared.
		{"GET /api/v1/public/organizations/{slug}", "slug", false, false},
	} {
		t.Run(tc.pattern+" "+tc.name, func(t *testing.T) {
			example, nonID, guarded := server.NonIDPathWildcard(tc.pattern, tc.name)
			if nonID != tc.nonID || guarded != tc.guarded {
				t.Fatalf("nonID=%v guarded=%v, want nonID=%v guarded=%v", nonID, guarded, tc.nonID, tc.guarded)
			}
			if nonID && example == "" {
				t.Fatal("a non-id wildcard has no example value")
			}
		})
	}
}
