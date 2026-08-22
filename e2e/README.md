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

The default `baseURL` is `http://localhost:64300` (Storefront), overridable with `PLAYWRIGHT_BASE_URL`.
Start the full stack with `make dev` before running browser tests against a live environment.

The override exists for the case `make dev` cannot serve: a build of a branch, running on its own port,
which is the only way to point these specs at code the dev stack does not have. Note that a production
build needs `STOREFRONT_STUB_PAYMENTS=1` for `checkout.spec.ts` — the stub Payment Provider's
interstitial is a 404 without it (`apps/storefront/lib/stub-payments.ts`) — and that the API it talks to
must be built from the same branch, since it is what decides both the Event fields the Storefront reads
and the origin the Payment Provider redirects back to.

## Ticket Assignment journey

`tests/ticket-assignment.spec.ts` walks a buyer of two tickets giving one away and the Holder answering a
Ticket Question from the mailed link. It needs both features open on the dev stack — set
`TICKET_QUESTIONS_ENABLED=true` and `TICKET_ASSIGNMENT_ENABLED=true` in the root `.env` and recreate the
backend (`docker compose up -d backend`) — and it reads the Assignment Link out of the API log the same way
the sign-in specs read a passcode.

It does not use the dev-seed Event: a database restored with `make prod-to-local` has none, so the spec
provisions its own Organizer, Organization and Event under `e2e-` identifiers through the staff API on the
first run and finds them on every run after. The Organizer's session is cached in `e2e/.e2e-state/`
(gitignored) because passcodes are rationed to three per address per quarter-hour; when that ration is
spent the spec skips rather than fails.

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
