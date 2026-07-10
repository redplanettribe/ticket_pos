# PRD: Integration Testing Guidance and Harness

Tracker: [GitHub #4](https://github.com/redplanettribe/ticket_pos/issues/4)

## Problem Statement

Ticket POS has a growing Go API surface spanning identity, catalog, sales, and integrations.
Business rules such as staff Session handling, Ticket Type capacity under concurrency, and Sale Import batch integrity must stay correct as domains evolve.

The project already has comprehensive staff authentication HTTP integration tests, but they start a fresh Postgres container per test, which is slow and inconsistent with how other domains will be tested.
There is no shared harness, no canonical authoring guide, and no clear boundary between HTTP integration tests, repository-level exceptions, and Playwright E2E smoke tests.

Makefile targets treat all Go tests the same, so developers cannot run fast unit tests locally without also paying the cost of integration tests.
CI runs the full Go suite on every PR, but the roadmap still lists integration testing as "Not started" and documentation is scattered across technical design and individual PRDs.

Without a unified integration testing package, harness, commands, and documentation, new features will duplicate setup code, test the wrong seams, or overlap with E2E coverage.

## Solution

Establish a single backend integration testing package with a shared TestMain harness, domain-organized test files, and clear authoring rules.
HTTP integration against real PostgreSQL via testcontainers is the default seam for verifying API contracts and business rules.
Repository integration tests are reserved for concurrency and locking scenarios that HTTP alone cannot stress reliably, always paired with at least one HTTP test proving the user-visible outcome.

Provide Makefile targets that separate fast local feedback from full integration and CI runs.
Publish canonical guidance in `docs/testing.md` and an agent skill at `.cursor/skills/integration-testing/SKILL.md`, cross-linked from `technical-design.md` and roadmap item A13.

Keep E2E smoke tests thin: Playwright plus Docker Compose validates cross-runtime journeys only, without duplicating business rule coverage owned by integration tests.

## User Stories

1. As a backend developer, I want a single integration test package with a shared harness, so that I do not reimplement Postgres startup and HTTP client setup in every domain.
2. As a backend developer, I want domain tests organized in separate files such as `auth_test.go` and `catalog_test.go`, so that I can find and extend tests for the area I am working on.
3. As a backend developer, I want one Postgres instance shared across all tests in the integration package via TestMain, so that test runs are faster than starting a container per test.
4. As a backend developer, I want the database truncated between tests, so that each test starts from a clean state without order-dependent failures.
5. As a backend developer, I want integration tests to run serially without `t.Parallel()`, so that truncate-based isolation remains reliable.
6. As a backend developer, I want HTTP helpers for common API flows such as `verifyOTP` and future `createEvent`, so that tests read as user journeys rather than raw request boilerplate.
7. As a backend developer, I want to assert HTTP status, response envelope shape, `error.code`, and response data, so that API contract regressions are caught at the boundary customers and clients depend on.
8. As a backend developer, I want guidance to seed or read state via SQL only when the API cannot set up the scenario, so that tests stay black-box and resilient to internal refactors.
9. As a backend developer, I want explicit rules against testing SQL strings, private helpers, or internal call order, so that tests do not break on harmless implementation changes.
10. As a backend developer working on staff identity, I want existing auth integration tests refactored onto the shared harness, so that auth remains the reference pattern for other domains.
11. As a backend developer working on catalog, I want HTTP integration tests for Event and Ticket Type CRUD and visibility rules, so that Organization-scoped catalog behavior is verified end to end through the API.
12. As a backend developer working on sales, I want HTTP integration tests for Online Sale and In-Person Sale flows, so that Ticket Sale creation and capacity effects are validated at the API seam.
13. As a backend developer working on Sale Import, I want HTTP integration tests for batch upload success and failure envelopes, so that all-or-nothing import semantics are visible to Event Staff through the API.
14. As a backend developer working on Integration Partner access, I want HTTP integration tests for integration-scoped routes, so that org-wide programmatic access rules are enforced at the HTTP boundary.
15. As a backend developer working on concurrency-sensitive sales logic, I want repository integration tests for atomic capacity decrement, so that row-level locking behavior is verified under parallel database access.
16. As a backend developer working on idempotency, I want repository integration tests for replay races, so that duplicate requests cannot double-sell capacity.
17. As a backend developer working on Sale Import locking, I want repository integration tests for batch locking behavior, so that concurrent imports cannot corrupt Ticket Type sold counts.
18. As a backend developer writing repository exception tests, I want a requirement to also add at least one HTTP test proving the user-visible outcome, so that low-level correctness never diverges from what the API reports.
19. As a backend developer, I want `make test` to run only fast non-integration Go tests plus JavaScript tests, so that I get quick feedback during iterative development.
20. As a backend developer, I want `make test-integration` to run `go test ./integration/...`, so that I can explicitly run the full integration suite when my change touches API behavior or persistence.
21. As a backend developer, I want `make ci` to run the full Go suite, vet, and turbo lint/typecheck/build, so that my local pre-push check matches what CI enforces.
22. As a CI maintainer, I want pull request workflows to run the full Go test suite including integration tests with Docker available, so that regressions in business rules are caught before merge.
23. As a CI maintainer, I want integration tests to use testcontainers against real PostgreSQL, so that schema drift and migration issues surface in CI the same way they would in production.
24. As a frontend developer, I want integration tests to own API contracts and business rules, so that I can rely on stable HTTP behavior without duplicating backend assertions in JS unit tests.
25. As a frontend developer, I want E2E smoke tests limited to cross-runtime journeys, so that Playwright runs stay fast and focused on wiring rather than exhaustive domain coverage.
26. As a QA-minded developer, I want a Staff login cookie smoke test in Playwright, so that the Staff BFF httpOnly cookie path from browser to Go API is verified at least once.
27. As a QA-minded developer, I want a storefront checkout smoke test in Playwright, so that the Storefront to Go API happy path is verified across runtimes without re-testing every sale edge case.
28. As a QA-minded developer, I want a clear rule that integration and E2E must not duplicate coverage, so that failures point to the right layer and CI time stays bounded.
29. As a new contributor, I want `docs/testing.md` as the canonical integration testing guide, so that I can onboard without hunting through multiple PRDs and design docs.
30. As a new contributor, I want the guide to explain when to choose HTTP integration versus repository exception tests, so that I pick the right seam on the first try.
31. As a new contributor, I want examples that mirror the `auth_test.go` patterns updated for the harness, so that I can copy established conventions confidently.
32. As an AI coding agent, I want a `.cursor/skills/integration-testing/SKILL.md` checklist with links to the canonical guide, so that I follow project testing rules automatically when adding or changing tests.
33. As an AI coding agent, I want the skill to list required assertions and forbidden patterns, so that generated tests match team standards without re-deriving them from conversation.
34. As a tech lead, I want `technical-design.md` to cross-link the integration testing guide, so that architecture readers see testing as a first-class engineering agreement.
35. As a tech lead, I want roadmap item A13 updated to reflect HTTP integration as the default with repository exceptions, so that planning status matches reality once auth tests and the harness land.
36. As a platform engineer, I want `harness_test.go` to own TestMain, Postgres lifecycle, truncate logic, and shared HTTP helpers, so that behavioral changes to test infrastructure happen in one place.
37. As a platform engineer, I want migrations applied once when the shared Postgres starts, so that every test sees the same schema the production deploy uses.
38. As a platform engineer, I want truncate to reset all application tables while preserving migration metadata, so that tests remain isolated without re-running migrations per test.
39. As a domain owner for identity, I want auth tests to continue covering OTP request and verify, Session lifecycle, Organization creation, Member selection, and staff-scoped authorization, so that staff authentication remains comprehensively guarded.
40. As a domain owner for catalog, I want integration tests to use Organization and Event vocabulary from CONTEXT.md, so that test names and scenarios align with ubiquitous language.
41. As a domain owner for sales, I want integration tests to refer to Ticket Sale, Ticket Sale Line, and Ticket Type capacity, so that sales scenarios are readable to product and engineering alike.
42. As a domain owner for integrations, I want tests to distinguish Integration Partner access from External Platform Sale Import, so that programmatic management and imported sales are not conflated.
43. As a release engineer, I want E2E smoke tests to run on main and nightly only, so that pull request CI stays fast while cross-runtime regressions are still caught regularly.
44. As a release engineer, I want E2E to run against Docker Compose, so that the smoke environment mirrors local development topology.
45. As a developer fixing a flaky test, I want serial execution and truncate isolation documented as non-negotiable defaults, so that flakiness is addressed by fixing setup rather than adding arbitrary sleeps.
46. As a developer adding a new API endpoint, I want a checklist item to add or extend an integration test in the matching domain file, so that new endpoints never ship without boundary coverage.
47. As a code reviewer, I want pull requests that change HTTP handlers to include integration test updates in the same package, so that review can verify behavior not just implementation.
48. As a code reviewer, I want to reject tests that reach into unexported helpers or assert raw SQL, so that the test suite stays maintainable.
49. As a developer running tests locally without Docker, I want docs to state that integration tests require Docker for testcontainers, so that failures are understood immediately.
50. As a developer on a laptop, I want `make test` to succeed without Docker, so that unit-level work is never blocked by integration infrastructure.
51. As a staff app developer, I want confidence that Session and active Member rules are integration-tested on the Go API, so that BFF proxy code can stay thin.
52. As a storefront developer, I want checkout business rules verified in integration tests, so that the Storefront only needs to handle presentation and client-side validation.
53. As a data engineer touching migrations, I want integration tests to fail when schema changes break repositories, so that migration mistakes are caught before merge.
54. As a performance-conscious developer, I want shared Postgres via TestMain instead of per-test containers, so that the integration suite scales as domain files grow.
55. As a maintainer of `auth_test.go` prior art, I want a documented migration path from per-test containers to the harness, so that the refactor is intentional and complete rather than half-applied.
56. As a contributor writing catalog integration tests for roadmap item C14, I want to extend the same package and harness, so that catalog coverage follows one pattern.
57. As a contributor writing concurrent sales tests for roadmap item D10, I want repository exception tests colocated under `internal/sales/repository` with paired HTTP tests, so that concurrency and user-visible outcomes stay linked.
58. As a contributor writing Sale Import tests for roadmap item G8, I want HTTP tests for batch envelopes plus repository tests for locking if needed, so that import integrity is covered at both seams appropriately.
59. As a documentation reader, I want Testing Decisions in domain PRDs to defer to `docs/testing.md` for harness details, so that guidance has a single source of truth.
60. As a project historian, I want Further Notes to record that auth integration tests predated the harness, so that future readers understand why `auth_test.go` was the first comprehensive suite.

## Implementation Decisions

### Package layout

- All HTTP integration tests live in a single Go package: `backend/integration/`.
- Shared infrastructure lives in `harness_test.go` within that package.
- Domain scenarios live in separate test files named by domain: `auth_test.go`, `catalog_test.go`, `sales_test.go`, `import_test.go`, and similar as domains grow.
- Repository exception tests live under `backend/internal/<domain>/repository/` in that domain's repository test files, not in the integration package.

### Harness behavior

- `harness_test.go` defines TestMain for the integration package.
- TestMain starts exactly one PostgreSQL container via testcontainers for the entire package run.
- TestMain applies migrations once at startup using the same migration runner the API uses in development.
- TestMain constructs shared dependencies: database connection, application wiring, httptest server, capture email sender, fixed clock, and HTTP client helpers.
- Before each test function, the harness truncates all application tables to provide clean state.
- Truncate preserves migration bookkeeping tables so migrations are not reapplied between tests.
- Tests run serially.
- `t.Parallel()` is forbidden in the integration package to avoid race conditions against shared database state.

### HTTP test authoring

- The primary assertion surface is HTTP: request in, response out.
- Every meaningful test asserts at minimum: HTTP status code, response envelope shape (`data`, `error`, `request_id`), and when applicable `error.code` and payload fields.
- Prefer high-level API helper functions that encapsulate multi-step flows.
- Existing example: `verifyOTP` helper for staff authentication flows.
- Future helpers follow the same pattern: `createEvent`, `createTicketType`, `completeOnlineSale`, `uploadSaleImport`, and similar.
- Helpers return parsed response bodies and relevant side-effect handles such as session cookies.
- Use direct SQL seed or read only when the public API cannot establish preconditions or when verifying a side effect the API does not expose.
- Document SQL usage with a brief comment explaining why the API was insufficient.

### Repository exception authoring

- Repository tests are allowed only for concurrency, locking, and race scenarios.
- Approved cases: atomic Ticket Type capacity decrement under parallel updates, idempotency key replay races, Sale Import batch locking under concurrent uploads.
- Each repository exception must have at least one companion HTTP integration test demonstrating the user-visible outcome such as correct remaining capacity, conflict response, or import rejection envelope.
- Repository tests use the same testcontainers Postgres approach but may use a separate TestMain in the repository package if isolation from HTTP tests is cleaner.
- Repository tests still must not assert SQL string contents or private function invocation order.

### Refactor of existing auth tests

- `auth_test.go` already provides comprehensive staff authentication HTTP coverage using testcontainers.
- Current implementation starts a new Postgres container per test via `setupTestEnv`.
- Refactor auth tests to use the shared TestMain harness and truncate-between-tests pattern without reducing scenario coverage.
- Preserve existing scenarios: OTP request and verify, Session read and logout, Organization creation, Member selection, authorization failures, rate limits, and duplicate slug handling.
- `auth_test.go` becomes the reference example cited in `docs/testing.md` after refactor.

### Makefile targets

- `make test` runs fast checks only: non-integration Go tests (excluding `backend/integration/`) plus JavaScript tests via `pnpm turbo test`.
- `make test-integration` runs `go test` against `backend/integration/...` only.
- `make ci` runs the full suite: `go test ./...` including integration, `go vet ./...`, and `pnpm turbo lint typecheck build`.
- The decision requirement is behavioral: local default is fast; integration is opt-in; CI is complete.

### CI

- Pull request CI runs the full Go test suite including integration tests.
- Docker is available in CI for testcontainers.
- Integration test failure blocks merge.
- E2E smoke remains on main and nightly workflows, not on every PR, consistent with technical design.

### Documentation deliverables

- `docs/testing.md` is the canonical human-readable integration testing guide covering layers, harness usage, authoring rules, commands, and E2E boundary.
- `.cursor/skills/integration-testing/SKILL.md` provides an agent-oriented checklist, forbidden patterns, and links to `docs/testing.md` and this PRD.
- `docs/technical-design.md` gains a cross-link to `docs/testing.md` in the testing section.
- `docs/roadmap.md` item A13 description is updated to reflect HTTP integration testing harness and guidance, noting repository tests as a documented exception rather than the default.

### E2E boundary

- Integration tests own business rules, domain invariants, and API contracts for the Go API.
- E2E smoke tests own thin cross-runtime journeys using Playwright against Docker Compose.
- Planned E2E smokes: Staff login with httpOnly cookie through the BFF, and Storefront checkout happy path.
- E2E must not re-assert exhaustive domain rules already covered by integration tests.
- `e2e/tests/smoke.spec.ts` is currently a placeholder and will be replaced with real smokes when the stack is wired.

### Vocabulary

- Tests, docs, and helper names use domain terms from CONTEXT.md: Organization, Member, Session, Active Member, Event, Ticket Type, Ticket Sale, Ticket Sale Line, Online Sale, In-Person Sale, Sale Import, Integration Partner, Integration, External Platform, Storefront, and Sales Channel.
- Avoid deprecated synonyms listed in CONTEXT.md such as "user" for Member or "order" for Ticket Sale.

## Testing Decisions

### Layer matrix

| Layer | Location | When to use |
|-------|----------|-------------|
| HTTP integration (default) | `backend/integration/` | Most API features: auth, catalog, sales, import, integration partner routes |
| Repository integration (exception) | `backend/internal/<domain>/repository/` | Concurrency and locking only; always pair with HTTP test for user-visible outcome |
| E2E smoke | `e2e/` with Playwright and Compose | Thin cross-runtime journeys; main and nightly CI only |

### What makes a good integration test

- Test external behavior at the highest stable seam: HTTP requests in, HTTP responses out.
- Assert status, envelope, `error.code`, and meaningful response data fields.
- Express scenarios as actor journeys: a Member verifies OTP, an Event Staff creates an Event, a Customer completes an Online Sale.
- Prefer API helpers over raw httptest boilerplate.
- Use SQL only as an escape hatch with documented justification.

### What to avoid

- Do not test private helper functions or unexported methods.
- Do not assert SQL query string contents or exact query text.
- Do not assert internal service call order between layers.
- Do not use `t.Parallel()` in the integration package.
- Do not duplicate integration scenarios in Playwright E2E tests.

### Primary test seam

Go API HTTP integration tests against real PostgreSQL via testcontainers.
This matches `technical-design.md` and `prd-staff-authentication.md`.
It exercises routing, middleware, handlers, services, repositories, migrations, and envelope mapping in one pass.

### Verification commands

- Local fast feedback: `make test`
- Local integration: `make test-integration` (requires Docker)
- Local full pre-push: `make ci`
- CI pull request: full Go suite including integration, vet, turbo lint/typecheck/build

### Success criteria for this initiative

- `harness_test.go` exists with TestMain, shared Postgres, truncate, and HTTP helpers.
- `auth_test.go` refactored to use the harness with no loss of scenario coverage.
- `docs/testing.md` and agent skill published and cross-linked.
- Makefile targets behave as specified: fast test, explicit integration, full ci.
- Roadmap A13 status and description align with delivered harness and guidance.
- Placeholder E2E smoke plan documented with clear non-duplication rules even before smokes are implemented.

## Out of Scope

- Playwright E2E implementation beyond documenting the smoke boundary and replacing the placeholder spec when the stack is ready.
- Visual regression testing or screenshot comparison.
- Load testing and performance benchmarking suites.
- Contract testing against external payment providers or email services beyond pluggable test doubles already in the platform layer.
- Frontend unit or integration tests in Staff or Storefront apps beyond existing `pnpm turbo test` scope.
- Testing Next.js BFF proxy handlers in isolation unless they accumulate non-trivial logic worth unit testing.
- Multi-database or multi-region test environments.
- Parallel integration test execution or sharded test packages.
- In-memory SQLite or other substitutes for PostgreSQL in integration tests.
- Automatic test generation from OpenAPI without human-authored scenario assertions.
- Coverage percentage gates or mandated coverage targets.
- Repository integration tests for CRUD-only repositories with no concurrency concern.
- Testing migration file contents or migration ordering beyond "migrations apply cleanly on fresh Postgres" exercised by the harness startup.

## Further Notes

This PRD consolidates decisions from a completed grilling session and supersedes ambiguous roadmap wording that labels A13 as "Repository integration tests" only.
The intended default is HTTP integration; repository tests are a narrow exception.

Prior art exists today.
`backend/integration/auth_test.go` already delivers comprehensive staff authentication HTTP integration coverage with testcontainers, CaptureEmailSender, fixed clock, and httptest.
That file is the behavioral template but not yet the structural template: per-test Postgres startup should migrate to the shared TestMain and truncate pattern described here.

Roadmap item A13 is marked "Not started" despite auth integration tests existing.
Completing this initiative includes updating A13 to "In progress" or "Done" when the harness, docs, Makefile split, and auth refactor land.
Related roadmap items C14, D10, and G8 should consume this harness rather than inventing parallel infrastructure.

`technical-design.md` already states that repository integration tests run against real Postgres in CI to catch schema drift.
This PRD aligns that statement with HTTP integration as the primary seam and clarifies when repository tests are appropriate.

`prd-staff-authentication.md` Testing Decisions should remain valid at the scenario level.
Harness and package-structure details defer to `docs/testing.md` once published to avoid duplication.

The agent skill at `.cursor/skills/integration-testing/SKILL.md` should be kept concise: a checklist, links, and anti-patterns.
Long prose belongs in `docs/testing.md`.

E2E runs on main and nightly per technical design CI agreement.
PR CI stays fast by not running Playwright on every pull request.

When truncate is implemented, enumerate tables explicitly or use a maintained list tied to migrations so new tables cannot silently leak state between tests.

Future domains should add rows to the layer matrix only by exception review, not by defaulting new features to repository tests.
