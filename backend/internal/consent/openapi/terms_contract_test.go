package openapi_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestTermsOpenAPIContract checks that the committed spec documents the public
// Terms read with a typed success envelope — the consent contract test's
// guarantee, for the Terms endpoint. The path is asserted verbatim because it
// is the address the Storefront terms page reads from, the address the capture
// surfaces (#536, #538) take the acceptance label from, and the address the
// Staff app's gate screen links out to instead of hosting a copy.
func TestTermsOpenAPIContract(t *testing.T) {
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

	const path = "/api/v1/public/terms/{locale}"
	if _, ok := doc.Paths[path]; !ok {
		t.Fatalf("missing path %s — regenerate the spec with `make openapi`", path)
	}

	for _, schema := range []string{
		"openapi.EnvelopeTerms",
		"service.TermsView",
	} {
		if _, ok := doc.Components.Schemas[schema]; !ok {
			t.Fatalf("missing schema %s — regenerate the spec with `make openapi`", schema)
		}
	}
}
