# Ticket POS

Multi-tenant event ticketing platform with a Go API, public Storefront, and Staff POS app.

## Documentation

- [CONTEXT.md](./CONTEXT.md) - domain vocabulary and ubiquitous language
- [docs/business-intent.md](./docs/business-intent.md) - product goals and scope
- [docs/technical-design.md](./docs/technical-design.md) - architecture, stack, and engineering agreements
- [docs/prd-staff-authentication.md](./docs/prd-staff-authentication.md) - PRD for staff OTP auth (M3T-165)
- [docs/prd-integration-testing.md](./docs/prd-integration-testing.md) - PRD for integration testing harness and guidance
- [docs/testing.md](./docs/testing.md) - canonical HTTP integration testing guide

## Local development

Start the full stack (Postgres, Go API, Storefront, Staff):

```bash
make dev
```

With the backend running, open [http://localhost:64080/swagger/index.html](http://localhost:64080/swagger/index.html) for interactive API docs.
After changing handlers or request/response types, run `make swagger` to refresh the spec.

Host ports are namespaced away from Docker's defaults so `make dev` doesn't collide with other local dev stacks:

| Service | Host port | Container port |
|---|---|---|
| Postgres | 64432 | 5432 |
| MinIO API | 64900 | 9000 |
| MinIO Console | 64901 | 9001 |
| Backend (Go API) | 64080 | 8080 |
| Storefront | 64300 | 3000 |
| Staff | 64301 | 3001 |

Other common commands:

```bash
make down      # stop Docker Compose services
make test              # fast Go unit tests (no integration) + JS package tests
make test-integration  # HTTP integration tests (requires Docker)
make ci                # full pre-push checks (Go suite including integration, vet, lint, build)
make migrate   # apply database migrations
make swagger   # regenerate API docs from Go annotations
make api-client  # regenerate TypeScript client from openapi/openapi.yaml
make openapi   # run swagger + api-client
pnpm install   # install JS workspace dependencies
```

`make migrate` applies the SQL migrations in `backend/migrations/`. It defaults to the local Docker Compose Postgres (host port 64432), so that container must be running (`make dev`, or just the Postgres service). It's idempotent — safe to re-run. Point it at another database by exporting `DATABASE_URL`, e.g. `DATABASE_URL=postgres://user:pass@host:5432/db?sslmode=disable make migrate`.

## Monorepo layout

| Path | Purpose |
|------|---------|
| `backend/` | Go API (modular monolith) |
| `apps/storefront/` | Public Next.js ticket sales |
| `apps/staff/` | Staff Next.js BFF and POS |
| `packages/` | Shared TypeScript packages |
| `openapi/` | OpenAPI contract |
| `e2e/` | Playwright end-to-end tests |
# ticket_pos
