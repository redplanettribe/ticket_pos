# Ticket POS

Multi-tenant event ticketing platform with a Go API, public Storefront, and Staff POS app.

## Documentation

- [CONTEXT.md](./CONTEXT.md) - domain vocabulary and ubiquitous language
- [docs/business-intent.md](./docs/business-intent.md) - product goals and scope
- [docs/technical-design.md](./docs/technical-design.md) - architecture, stack, and engineering agreements
- [docs/prd-staff-authentication.md](./docs/prd-staff-authentication.md) - PRD for staff OTP auth (M3T-165)

## Local development

Start the full stack (Postgres, Go API, Storefront, Staff):

```bash
make dev
```

With the backend running, open [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) for interactive API docs.
After changing handlers or request/response types, run `make swagger` to refresh the spec.

Other common commands:

```bash
make down      # stop Docker Compose services
make test      # Go unit tests + JS package tests
make ci        # fast PR-level checks
make migrate   # apply database migrations
make swagger   # regenerate API docs from Go annotations
pnpm install   # install JS workspace dependencies
```

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
