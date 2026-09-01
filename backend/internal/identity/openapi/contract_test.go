package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIContract(t *testing.T) {
	t.Parallel()

	specPath := openAPISpecPath(t)
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	var doc struct {
		OpenAPI    string `yaml:"openapi"`
		Components struct {
			SecuritySchemes map[string]any    `yaml:"securitySchemes"`
			Schemas         map[string]schema `yaml:"schemas"`
		} `yaml:"components"`
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse spec: %v", err)
	}

	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version = %q, want 3.1.0", doc.OpenAPI)
	}
	if _, ok := doc.Components.SecuritySchemes["BearerAuth"]; !ok {
		t.Fatal("missing BearerAuth security scheme")
	}
	if _, ok := doc.Components.Schemas["openapi.EnvelopeSession"]; !ok {
		t.Fatal("missing typed session envelope schema")
	}
	if _, ok := doc.Paths["/api/v1/auth/otp/request"]; !ok {
		t.Fatal("missing auth OTP request path")
	}
	// The terms step the staff sign-in gate holds a proven email at (#538).
	if _, ok := doc.Paths["/api/v1/auth/terms/accept"]; !ok {
		t.Fatal("missing auth terms accept path")
	}
	if _, ok := doc.Components.Schemas["openapi.EnvelopeAcceptTerms"]; !ok {
		t.Fatal("missing typed accept-terms envelope schema")
	}

	// The Adulthood Declaration on both staff gates (#587, ADR 0069). It is on
	// the contract in three places and each of them is load-bearing: the two
	// bodies that carry the answer, and the view whose field's PRESENCE is how
	// a surface knows to draw the second box at all. A generated spec that lost
	// any of them would be a client shipped without a mandatory checkbox.
	for schemaName, field := range map[string]string{
		"handler.acceptTermsBody":    "adulthood_declaration",
		"handler.staffTermsGateBody": "adulthood_declaration",
		"service.TermsRequiredView":  "adulthood_declaration_label",
	} {
		properties := doc.Components.Schemas[schemaName].Properties
		if properties == nil {
			t.Fatalf("missing %s schema", schemaName)
		}
		if _, ok := properties[field]; !ok {
			t.Fatalf("%s does not carry %s", schemaName, field)
		}
	}

	// And it is OPTIONAL on the view, which is the whole of "this edition does
	// not ask". A required field here would be a spec asserting that every
	// edition asks, which is the one thing ADR 0069 spent a paragraph refusing:
	// the label is served when present and absent otherwise, and the box is
	// drawn iff the edition in effect carries the Artifact.
	for _, name := range doc.Components.Schemas["service.TermsRequiredView"].Required {
		if name == "adulthood_declaration_label" {
			t.Fatal("adulthood_declaration_label is required on the terms-required view; " +
				"its absence is how an edition says it does not ask")
		}
	}
}

// schema is as much of one component schema as this contract cares about.
type schema struct {
	Properties map[string]any `yaml:"properties"`
	Required   []string       `yaml:"required"`
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
