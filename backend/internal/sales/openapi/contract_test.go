package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPublicCheckoutContract asserts the public checkout endpoints are present
// in the generated OpenAPI spec so the published contract stays in sync with
// the code.
func TestPublicCheckoutContract(t *testing.T) {
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
		"/api/v1/public/organizations/{slug}/events/{eventSlug}/checkout",
		"/api/v1/public/checkout/{clientTransactionId}/confirm",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %q in OpenAPI spec", path)
		}
	}
}

// TestSalesExportContract asserts the Sales Export endpoint is present in the
// generated OpenAPI spec. The file leaves the platform, so the shape of the call
// that produces it is part of the published contract like its neighbours.
func TestSalesExportContract(t *testing.T) {
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

	const path = "/api/v1/staff/events/{id}/sales/export"
	if _, ok := doc.Paths[path]; !ok {
		t.Fatalf("missing path %q in OpenAPI spec", path)
	}
}

// TestSalesTrendsContract asserts the Sales Trends endpoint is present in the
// generated spec AND that its success envelope carries a real data type rather
// than a bare envelope. The staff app builds its chart straight off the
// generated api-client, so the shape of the day × Ticket Type matrix — days with
// their lines, and the catalog the legend is built from — is part of the
// published contract and not merely of the handler.
func TestSalesTrendsContract(t *testing.T) {
	t.Parallel()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Paths      map[string]any `yaml:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	const path = "/api/v1/staff/events/{id}/sales/trends"
	if _, ok := doc.Paths[path]; !ok {
		t.Fatalf("missing path %q in OpenAPI spec", path)
	}

	for schema, wantProps := range map[string][]string{
		"openapi.EnvelopeSalesTrends": {"data", "error", "request_id"},
		"service.SalesTrends":         {"timezone", "currency", "ticket_types", "days", "reversed_count"},
		"service.TrendsDay":           {"date", "lines"},
		"service.TrendsLine":          {"ticket_type_id", "quantity", "takings_cents"},
		"service.TrendsTicketType":    {"id", "name", "sort_order"},
	} {
		got, ok := doc.Components.Schemas[schema]
		if !ok {
			t.Fatalf("missing schema %q in OpenAPI spec", schema)
		}
		for _, prop := range wantProps {
			if _, ok := got.Properties[prop]; !ok {
				t.Fatalf("schema %q is missing property %q; got %v", schema, prop, got.Properties)
			}
		}
	}

	// The money figure is named Takings, never Net Proceeds: the two are
	// different numbers on the same Event and the contract must not let a reader
	// mistake one for the other (ADR 0040).
	if _, ok := doc.Components.Schemas["service.TrendsLine"].Properties["net_proceeds_cents"]; ok {
		t.Fatal("service.TrendsLine carries net_proceeds_cents; the Trends figure is Takings (ADR 0040)")
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
