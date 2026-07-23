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
make prod      # start the production-parity stack (see below)
make prod-down # stop the production-parity stack
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

## Production-parity stack

`make dev` runs the API from `Dockerfile.dev` with live reload, so it never exercises the image that
actually ships. `docker-compose.prod.yml` is a separate stack that runs the **production** API image and
applies migrations the way Cloud Run will — as a one-shot job using the same image's migrate entrypoint,
completing before the server starts. Use it to check that a change survives a real image build.

```bash
make prod                              # build and start (Ctrl-C to stop)
curl http://localhost:64680/health     # => {"status":"ok"}
make prod-down                         # stop
```

To start from an empty database, drop its volumes too:
`docker compose -f docker-compose.prod.yml down -v`.

It runs under its own Compose project (`ticket-pos-prod`) with its own volumes and host ports, so it can
run at the same time as `make dev`:

| Service | Host port | Container port |
|---|---|---|
| Postgres | 64632 | 5432 |
| MinIO API | 64600 | 9000 |
| MinIO Console | 64601 | 9001 |
| API | 64680 | 8080 |

The API here runs with `APP_ENV=production` and `RUN_MIGRATIONS=false`, exactly as in production: the
`migrate` service owns the schema, and the server never migrates on boot. Scope is the API only — the
Storefront and Staff apps join this stack later. See [docs/gcp-deployment.md](./docs/gcp-deployment.md).

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
