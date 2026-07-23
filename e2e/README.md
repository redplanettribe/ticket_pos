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

## Parity suite

`parity/` is a second suite with its own config (`playwright.parity.config.ts`), run separately so
`pnpm test` stays runnable without Docker:

```bash
make prod          # start the production-parity stack
make test-parity   # or: pnpm --filter @ticket-pos/e2e test:parity
```

It has one project per app: `storefront` targets `http://localhost:64603` (override with `STOREFRONT_URL`)
and `staff` targets `http://localhost:64604` (override with `STAFF_URL`). Both assert that the **shipped
container image** serves real pages. That is not redundant with the dev-server suite: standalone output
traces its dependencies out of the pnpm workspace at build time, and getting that wrong fails at container
start rather than at build, so it can only be caught by exercising the image.

The Staff spec follows the signed-out redirect to `/login` rather than asserting a page renders: Staff's
middleware reads the httpOnly session cookie, and both middleware execution and cookie flags differ
between `next dev` and a production build.

## CI

Fast PR checks do not run E2E here.
Full E2E runs on `main` and nightly via Docker Compose, exercising the complete stack (Postgres, Go API, Storefront, Staff).
