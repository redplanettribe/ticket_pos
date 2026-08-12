package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestConsentOpenAPIContract checks that the committed spec documents the
// public Privacy Policy read with a typed success envelope rather than a bare
// one. It fails when the handler is added or renamed without regenerating the
// spec with `make openapi`.
//
// The path is asserted verbatim because it is the address the Storefront reads
// the policy from, and — from #251 — the address every capture surface takes
// its Short Notice and checkbox labels from. Moving it moves the notice.
func TestConsentOpenAPIContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(openAPISpecPath(t))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		Components struct {
			Schemas map[string]any `yaml:"schemas"`
		} `yaml:"components"`
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	const path = "/api/v1/public/privacy-policy/{locale}"
	if _, ok := doc.Paths[path]; !ok {
		t.Fatalf("missing path %s — regenerate the spec with `make openapi`", path)
	}

	for _, schema := range []string{
		"openapi.EnvelopePrivacyPolicy",
		"service.PolicyView",
		"policy.ConsentLabels",
	} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Fatalf("missing schema %s — regenerate the spec with `make openapi`", schema)
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
