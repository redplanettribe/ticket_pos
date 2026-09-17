package openapi_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestCatalogCustomerDossierContract asserts the Customer Dossier read is in the
// published contract (#638).
func TestCatalogCustomerDossierContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(openAPISpecPath(t))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	path := "/api/v1/staff/events/{id}/customers/{customerId}/dossier"
	if _, ok := doc.Paths[path]["get"]; !ok {
		t.Fatalf("missing GET %q in OpenAPI spec", path)
	}
}
