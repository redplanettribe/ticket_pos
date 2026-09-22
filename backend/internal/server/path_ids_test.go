package server_test

import (
	"fmt"
	"net/http"
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
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			recovered := registerPanic(tc.pattern, tc.viaFunc)
			if recovered == nil {
				t.Fatal("registered, want a panic")
			}
			message := fmt.Sprint(recovered)
			if !strings.Contains(message, tc.pattern) || !strings.Contains(message, tc.wildcard) {
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
	} {
		if recovered := registerPanic(pattern, false); recovered != nil {
			t.Errorf("%s panicked: %v", pattern, recovered)
		}
	}
}
