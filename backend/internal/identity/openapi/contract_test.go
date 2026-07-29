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
			SecuritySchemes map[string]any `yaml:"securitySchemes"`
			Schemas         map[string]any `yaml:"schemas"`
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
