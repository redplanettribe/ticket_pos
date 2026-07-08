# E2E tests

Playwright smoke and integration tests for Ticket POS.

## Local development

```bash
pnpm install
pnpm test
```

The default `baseURL` is `http://localhost:3000` (Storefront).
Start the full stack with `make dev` before running browser tests against a live environment.

## CI

Fast PR checks do not run E2E here.
Full E2E runs on `main` and nightly via Docker Compose, exercising the complete stack (Postgres, Go API, Storefront, Staff).
