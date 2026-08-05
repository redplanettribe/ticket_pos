package openapi_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestFollowDigestOpenAPIContract checks that the committed spec documents the
// Follow Digest pipeline's two internal endpoints (#220, ADR 0030). It fails
// when a handler is added or renamed without regenerating the spec.
//
// These are the only INTERNAL routes with a contract test, and they have one for
// a reason the customer-facing routes do not need stated: nobody browses this
// namespace. It is called by Cloud Scheduler and by a Platform Operator with a
// curl during an incident (#226), and the generated spec is where either of them
// finds out what the endpoints are called and what the response means. A path
// that silently stopped being documented would not break a client — it would
// break a runbook.
func TestFollowDigestOpenAPIContract(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(openAPISpecPath(t))
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
		// TWO endpoints, deliberately, and a third appearing here would be the
		// contract breaking rather than growing: declaring a week and working
		// through it run on different cadences — one a week, one a minute — and a
		// single combined route could not be driven by either schedule or by a
		// test.
		"/api/v1/internal/follow-digests/enqueue",
		"/api/v1/internal/follow-digests/drain",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("missing path %s — regenerate the spec with `make swagger`", path)
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
