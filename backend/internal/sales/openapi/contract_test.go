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

func openAPISpecPath(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return filepath.Join(root, "openapi", "openapi.yaml")
}
