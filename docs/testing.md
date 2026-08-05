# Integration Testing

Ticket POS verifies business rules and API contracts through HTTP integration tests against real PostgreSQL.
This document is the canonical guide for writing and running those tests.

The initiative PRD lives in [prd-integration-testing.md](./prd-integration-testing.md).
API response shapes and error codes are defined in [technical-design.md](./technical-design.md) and the [api-errors skill](../.cursor/skills/api-errors/SKILL.md).
Domain vocabulary lives in [CONTEXT.md](../CONTEXT.md).

## Three layers

| Layer | Location | When to use |
|-------|----------|-------------|
| **HTTP integration (default)** | `backend/integration/` | Auth, catalog, sales, import, integration partner routes - any feature where HTTP is the stable customer-facing seam |
| **Repository integration (exception)** | `backend/internal/<domain>/repository/` | Concurrency and locking only: atomic capacity decrement, idempotency replay races, Sale Import batch locking |
| **E2E smoke** | `e2e/` with Playwright and Docker Compose | Thin cross-runtime journeys only; see [E2E boundary](#e2e-boundary) |

E2E smoke has two suites. `e2e/tests/` runs against the dev stack (`make dev`). `e2e/parity/` runs against
the production-parity stack (`make prod`, then `make test-parity`) and is the only layer that exercises the
shipped container images — Next standalone output traces its dependencies out of the pnpm workspace at
build time, and a mistrace fails at container start rather than at build, so nothing else can catch it.

HTTP integration is the default.
Repository tests are a narrow exception and must always be paired with at least one HTTP test that proves the user-visible outcome (correct capacity, conflict response, import rejection envelope, and similar).

## Package layout

All HTTP integration tests live in one Go package: `backend/integration/`.

| File | Role |
|------|------|
| `harness_test.go` | `TestMain`, shared Postgres lifecycle, truncate between tests, HTTP client helpers |
| `auth_test.go` | Staff identity and Session scenarios (reference pattern) |
| `catalog_test.go`, `sales_test.go`, `import_test.go`, … | Domain scenarios as features land |

Repository exception tests stay in the owning domain under `backend/internal/<domain>/repository/`, not in the integration package.

## Harness

`harness_test.go` owns shared test infrastructure for the integration package.

**Startup (`TestMain`):**

- Starts exactly one PostgreSQL container via testcontainers for the entire package run.
- Applies migrations once using the same migration runner the API uses in development.
- Constructs shared dependencies: database connection, application wiring, `httptest` server, capture email sender, fixed clock, and HTTP helpers.

**Per test (`setupTest`):**

- Truncates all application tables to provide clean state.
- Preserves migration bookkeeping tables so migrations are not reapplied between tests.
- Resets test doubles (captured OTP codes, fixed clock, and similar).

**Execution:**

- Tests run serially.
- Do not use `t.Parallel()` in the integration package.
- Truncate-based isolation depends on serial execution; flakiness is fixed by correcting setup, not by adding sleeps.

Integration tests require Docker for testcontainers.
`make test` succeeds without Docker; `make test-integration` requires it.

## Authoring rules

### HTTP in, HTTP out

The primary assertion surface is HTTP: request in, response out.
Every meaningful test exercises routing, middleware, handlers, services, repositories, migrations, and envelope mapping in one pass.

### Required assertions

At minimum, assert:

1. HTTP status code
2. Response envelope shape (`data`, `error`, `request_id`)
3. When applicable: `error.code` and meaningful fields in `data` or `error.details`

On success, `error` must be `null` and `data` holds the bare resource.
On failure, `data` must be `null` and `error.code` must match the contract.
See the [api-errors skill](../.cursor/skills/api-errors/SKILL.md) for envelope and error conventions.

### API helpers first

Prefer high-level helpers that encapsulate multi-step flows over raw `httptest` boilerplate.

Existing example: `verifyOTP` in `auth_test.go` requests and verifies OTP, then returns a Session ID.
Future helpers follow the same pattern: `createEvent`, `createTicketType`, `completeOnlineSale`, `uploadSaleImport`, and similar.

Helpers return parsed response bodies and relevant side-effect handles such as session tokens or cookies.

### SQL only when the API cannot

Use direct SQL seed or read only when the public API cannot establish preconditions or when verifying a side effect the API does not expose.
Document SQL usage with a brief comment explaining why the API was insufficient.

Example: `seedMultiMembership` in `auth_test.go` inserts multiple Organizations because no staff API exists yet for that setup.

### Forbidden patterns

Do not:

- Test private helper functions or unexported methods
- Assert SQL query string contents or exact query text
- Assert internal service call order between layers
- Use `t.Parallel()` in the integration package
- Duplicate integration scenarios in Playwright E2E tests

### Vocabulary

Test names and scenarios use domain terms from [CONTEXT.md](../CONTEXT.md): Organization, Member, Session, Active Member, Event, Ticket Type, Ticket Sale, Ticket Sale Line, Online Sale, In-Person Sale, Sale Import, Integration Partner, and similar.
Avoid deprecated synonyms such as "user" for Member or "order" for Ticket Sale.

## Commands

| Target | Purpose |
|--------|---------|
| `make test` | Fast local feedback: non-integration Go tests (excluding `backend/integration/`) plus JavaScript tests via `pnpm turbo test` |
| `make test-integration` | Full HTTP integration suite: `go test ./integration/...` (requires Docker) |
| `make test-parity` | Playwright smoke tests against the production-parity stack (requires `make prod`) |
| `make ci` | Pre-push parity with CI: full Go suite including integration, `go vet`, and `pnpm turbo lint typecheck build` |

A package that owns a Postgres testcontainer must never run concurrently with another that does.
`go test` runs separate packages in parallel, so `go test ./...` in one invocation tears one container down under the other and the integration package fails with `connection reset by peer` — a failure about the runner, not the code.
`make ci` therefore runs the non-integration packages and `./integration/...` as two invocations.
Today the only container owners are the integration harness and the repository-level concurrency test for the Follow Digest drain; adding a third means keeping it out of the same invocation as the others.

Pull request CI runs the full Go test suite including integration tests with Docker available.
Integration test failure blocks merge.

## E2E boundary

Integration tests own business rules, domain invariants, and API contracts for the Go API.
E2E smoke tests own thin cross-runtime journeys using Playwright against Docker Compose.

| Layer | Owns |
|-------|------|
| HTTP integration | OTP and Session rules, catalog CRUD, sale capacity, import envelopes, authorization, rate limits |
| E2E smoke | Staff login cookie path through the BFF, Storefront checkout happy path, wiring across Postgres + Go API + Next.js apps |

E2E must not re-assert exhaustive domain rules already covered by integration tests.
When a scenario fails, the layer should point to the right owner: business logic failures belong in integration tests; broken wiring across runtimes belongs in E2E.

E2E runs on `main` and nightly CI, not on every pull request.
See [e2e/README.md](../e2e/README.md) for local Playwright setup.

## Example test skeleton

Use `setupTest` at the start of every test function.
Use `verifyOTP` (or future domain helpers) for multi-step flows.

```go
func TestCatalogCreateEvent(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "owner@example.com")

	resp, body := env.post(t, "/api/v1/events", map[string]string{
		"name": "Summer Concert",
		"slug": "summer-concert",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create event status=%d error=%+v", resp.StatusCode, body.Error)
	}
	if body.Error != nil {
		t.Fatalf("expected no error, got %+v", body.Error)
	}

	var event struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body.Data, &event); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.Slug != "summer-concert" {
		t.Fatalf("slug=%q", event.Slug)
	}
}

func TestCatalogDuplicateSlug(t *testing.T) {
	env := setupTest(t)
	sessionID := verifyOTP(t, env, "owner@example.com")

	// ... create first event via API ...

	resp, body := env.post(t, "/api/v1/events", map[string]string{
		"name": "Duplicate",
		"slug": "summer-concert",
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	if body.Error == nil || body.Error.Code != "EVENT_SLUG_TAKEN" {
		t.Fatalf("expected EVENT_SLUG_TAKEN, got %+v", body.Error)
	}
}
```

For error-path tests, always assert both HTTP status and `error.code`.
For success-path tests, assert envelope shape, decode `data`, and check meaningful fields.

## Checklist for new API endpoints

- [ ] Add or extend a test in the matching domain file under `backend/integration/`
- [ ] Call `setupTest(t)` at the start of each test function
- [ ] Use API helpers for multi-step setup where they exist
- [ ] Assert HTTP status, envelope shape, and `error.code` on failures
- [ ] Use SQL seed only with a comment explaining why the API was insufficient
- [ ] If adding a repository exception test for concurrency, pair it with an HTTP test for the user-visible outcome

## Related documents

- [prd-integration-testing.md](./prd-integration-testing.md) - initiative PRD and user stories
- [technical-design.md](./technical-design.md) - architecture and CI agreements
- [roadmap.md](./roadmap.md) - item A13 (integration test harness)
- [e2e/README.md](../e2e/README.md) - Playwright smoke tests
- [api-errors skill](../.cursor/skills/api-errors/SKILL.md) - response envelope and error codes
