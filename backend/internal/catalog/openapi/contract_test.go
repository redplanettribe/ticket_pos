package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCatalogTagContract asserts the staff tag endpoints are present in the
// generated OpenAPI spec so the published contract stays in sync with the code.
func TestCatalogTagContract(t *testing.T) {
	t.Parallel()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	for _, path := range []string{
		"/api/v1/staff/tags",
		"/api/v1/staff/events/{id}/tags",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %q in OpenAPI spec", path)
		}
	}
}

// TestCatalogTicketQuestionContract asserts the Ticket Question authoring
// endpoints are in the published contract (#309).
//
// THE SPEC DESCRIBES THEM WHETHER OR NOT THE FEATURE FLAG IS ON, and that is
// correct rather than a leak: the routes are registered in every build and
// answer 404 while the flag is off (ADR 0045), so a contract that omitted them
// would be describing a different binary. What must not appear is any
// CUSTOMER-FACING Answer surface — the Answer Link is #312's and there is still
// nothing here for a buyer to reach.
func TestCatalogTicketQuestionContract(t *testing.T) {
	t.Parallel()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	for _, path := range []string{
		"/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions",
		"/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/order",
		"/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}",
		"/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options",
		"/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}/questions/{questionId}/options/{optionId}",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %q in OpenAPI spec", path)
		}
	}
}

// TestCatalogTicketAnswerContract asserts the staff Answer endpoints are in the
// published contract (#310).
//
// Described whether or not the flag is on, for the reason its Ticket Question
// neighbour above gives: the routes are registered in every build and answer 404
// while the flag is off, so a contract that omitted them would describe a
// different binary. All four are STAFF paths — an Answer has no customer-facing
// surface until the Answer Link lands (#312).
func TestCatalogTicketAnswerContract(t *testing.T) {
	t.Parallel()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	for _, path := range []string{
		"/api/v1/staff/events/{id}/ticket-sales/{ticketSaleId}/tickets",
		"/api/v1/staff/events/{id}/tickets/{ticketId}",
		"/api/v1/staff/events/{id}/tickets/{ticketId}/answers/{questionId}",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %q in OpenAPI spec", path)
		}
	}
}

func openAPISpecPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return filepath.Join(root, "openapi", "openapi.yaml")
}
