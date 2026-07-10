---
name: integration-testing
description: >-
  Ticket POS HTTP integration testing against real PostgreSQL via testcontainers.
  Use when adding or changing integration tests, the shared harness in
  backend/integration/, repository concurrency tests, or reviewing test seams.
  Trigger on phrases like "integration test", "testcontainers", "harness",
  "setupTest", "verifyOTP", "make test-integration", or when implementing API
  behavior that needs boundary coverage in backend/.
---

# Integration Testing

HTTP integration against real PostgreSQL is the default way to verify API contracts and business rules in Ticket POS.

Read the canonical guide in [docs/testing.md](../../../docs/testing.md) before adding or changing tests.
The initiative PRD is [docs/prd-integration-testing.md](../../../docs/prd-integration-testing.md).
API envelope and `error.code` conventions live in the [api-errors skill](../api-errors/SKILL.md).

## Three layers

| Layer | Location | Default? |
|-------|----------|----------|
| HTTP integration | `backend/integration/` | Yes - use for almost everything |
| Repository exception | `backend/internal/<domain>/repository/` | Only concurrency/locking; pair with HTTP test |
| E2E smoke | `e2e/` | Cross-runtime wiring only; do not duplicate integration coverage |

## Package layout

- Shared harness: `backend/integration/harness_test.go` (`TestMain`, Postgres, truncate, HTTP helpers)
- Domain tests: `backend/integration/<domain>_test.go` (e.g. `auth_test.go`)
- Reference pattern: `auth_test.go` after harness refactor

## Quick checklist

Before submitting integration test changes:

- [ ] Test lives in `backend/integration/<domain>_test.go` (or repository package for concurrency exceptions)
- [ ] Starts with `env := setupTest(t)` - never spin up Postgres per test
- [ ] No `t.Parallel()` in the integration package
- [ ] Asserts HTTP status, envelope (`data`, `error`, `request_id`), and `error.code` on failures
- [ ] Uses API helpers (`verifyOTP`, future `createEvent`, etc.) instead of raw boilerplate
- [ ] SQL seed/read only when API cannot set up the scenario - comment why
- [ ] Domain vocabulary from [CONTEXT.md](../../../CONTEXT.md) in test names and assertions
- [ ] Repository exception tests have a companion HTTP test for user-visible outcome

## Commands

| Target | When |
|--------|------|
| `make test` | Fast local loop (no integration, no Docker required) |
| `make test-integration` | After API or persistence changes (requires Docker) |
| `make ci` | Pre-push full suite matching CI |

## Forbidden patterns

Do **not**:

- Start a Postgres container per test - use shared `TestMain` harness
- Use `t.Parallel()` in `backend/integration/`
- Test unexported helpers or private methods
- Assert SQL query string contents or internal call order
- Reach past HTTP for business-rule assertions when a public route exists
- Duplicate integration scenarios in Playwright E2E tests
- Use in-memory SQLite or mocks instead of real Postgres for integration tests

## Example skeleton

```go
func TestExample(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "staff@example.com")

	resp, body := env.post(t, "/api/v1/...", reqBody, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}
	// decode body.Data and assert fields
}
```

For error paths, assert status **and** `body.Error.Code`.

## When updating guidance

If harness behavior or layer rules change, update [docs/testing.md](../../../docs/testing.md) first, then align this skill.
Domain PRDs should defer harness details to `docs/testing.md`.
