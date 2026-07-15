# E2E tests

Playwright smoke tests for Ticket POS cross-runtime wiring.

Business rules, API contracts, and domain invariants belong in HTTP integration tests (`backend/integration/`).
E2E owns thin journeys only - for example Staff login through the BFF httpOnly cookie path and Storefront checkout happy path.
Do not duplicate integration coverage here.
See [docs/testing.md](../docs/testing.md) for the full boundary.

## Local development

```bash
pnpm install
pnpm test
```

The default `baseURL` is `http://localhost:64300` (Storefront).
Start the full stack with `make dev` before running browser tests against a live environment.

## CI

Fast PR checks do not run E2E here.
Full E2E runs on `main` and nightly via Docker Compose, exercising the complete stack (Postgres, Go API, Storefront, Staff).
