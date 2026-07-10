# Roadmap

## Purpose

This document is a loose, feature-by-feature implementation plan for Ticket POS.
It is ordered by dependency and aligned with [business-intent.md](./business-intent.md) and [technical-design.md](./technical-design.md).
The canonical domain vocabulary lives in [CONTEXT.md](../CONTEXT.md).

This is a draft roadmap, not a commitment to dates or scope locks.
Features may shift as implementation learns more, but the sequence reflects what must exist before what.

## Status legend


| Symbol      | Meaning                |
| ----------- | ---------------------- |
| Done        | Shipped in the repo    |
| Partial     | Started but incomplete |
| Not started | Not yet implemented    |


Status reflects the repository at the time this document was written.
Update the status column as features land.

## Build order (summary)

```text
Platform spine (finish OpenAPI, client, CI, integration tests)
  → Staff identity (finish hardening + auth E2E)
  → Catalog (Events + Ticket Types, staff + public + storefront)
  → Sales core (schema, capacity logic, idempotency)
  → In-person sales (POS)
  → Online sales (storefront checkout)
  → Sale import (CSV from External Platforms)
  → Members, roles, and permissions
  → Integration Partner API
  → Production hardening and E2E smoke
```



### First three milestones

1. **Catalog live** - sign in, create an Event and Ticket Types, public storefront event page renders.
2. **In-person selling** - Event Staff sells at the door; remaining capacity is accurate.
3. **Online selling** - guest checkout on the storefront; capacity reflects online and in-person sales together.

---



## A. Platform and repo spine


| #   | Feature                                                              | Status      |
| --- | -------------------------------------------------------------------- | ----------- |
| A1  | Monorepo layout (Go API, Staff app, Storefront app, shared packages) | Done        |
| A2  | Docker Compose local stack (Postgres, backend, apps)                 | Done        |
| A3  | Makefile dev, test, and CI entrypoints                               | Done        |
| A4  | Postgres connection pool and migration runner                        | Done        |
| A5  | Platform middleware (request ID, logging, recover)                   | Done        |
| A6  | Standard API response envelope (`data`, `error`, `request_id`)       | Done        |
| A7  | Domain error to HTTP mapping (`platform/httputil`)                   | Done        |
| A8  | Pluggable `EmailSender` (dummy logs OTP in dev)                      | Done        |
| A9  | OpenAPI spec and Swagger UI                                          | Partial     |
| A10 | TypeScript client generation from OpenAPI (`packages/api-client`)    | Not started |
| A11 | CI: Go test and vet, migrations on fresh DB, turbo lint and build    | Partial     |
| A12 | CI: OpenAPI to client sync check                                     | Not started |
| A13 | HTTP integration test harness (testcontainers Postgres)              | Partial     |


---



## B. Staff identity and tenancy


| #   | Feature                                                       | Status      |
| --- | ------------------------------------------------------------- | ----------- |
| B1  | Identity schema (`organizations`, `members`, `sessions`, OTP) | Done        |
| B2  | Request OTP (`POST /api/v1/auth/otp/request`)                 | Done        |
| B3  | Verify OTP and create session                                 | Done        |
| B4  | Session read and logout                                       | Done        |
| B5  | Create organization (name and global slug)                    | Done        |
| B6  | List memberships and select active member                     | Done        |
| B7  | Session auth middleware and require active member             | Done        |
| B8  | Staff BFF auth routes (httpOnly cookie, proxy to Go)          | Done        |
| B9  | Login UI (email then code, single-page two-step)              | Done        |
| B10 | Onboarding UI (create org when zero memberships)              | Done        |
| B11 | Organization picker UI (two or more memberships)              | Done        |
| B12 | Protected staff routes and auth redirects                     | Done        |
| B13 | OTP rate limiting and attempt lockout                         | Partial     |
| B14 | Sliding 14-day session expiry                                 | Partial     |
| B15 | Dev seed organization and pre-provisioned member              | Done        |
| B16 | Auth E2E tests (Playwright)                                   | Not started |


Related PRD: [prd-staff-authentication.md](./prd-staff-authentication.md).

---



## C. Catalog - Events and Ticket Types


| #   | Feature                                                                | Status      |
| --- | ---------------------------------------------------------------------- | ----------- |
| C1  | Catalog DB schema (`events`, `ticket_types`, `sold_count`)             | Not started |
| C2  | Event CRUD service and repository (org-scoped)                         | Not started |
| C3  | Ticket Type CRUD service and repository                                | Not started |
| C4  | Staff API: list, create, and update Events                             | Not started |
| C5  | Staff API: list, create, and update Ticket Types on an Event           | Not started |
| C6  | Event slug (unique per organization) for storefront URLs               | Not started |
| C7  | Staff UI: events list and create or edit event                         | Not started |
| C8  | Staff UI: ticket type management on an event                           | Not started |
| C9  | Public API: get organization by slug                                   | Not started |
| C10 | Public API: get event and ticket types by slugs                        | Not started |
| C11 | Storefront SSR or ISR event page (`/{orgSlug}/events/{eventSlug}`)     | Partial     |
| C12 | Storefront SEO metadata (title, Open Graph, canonical URL)             | Not started |
| C13 | Catalog domain errors (`EVENT_NOT_FOUND`, duplicate name, and similar) | Not started |
| C14 | Catalog integration tests                                              | Not started |


---



## D. Sales core (shared by all channels)


| #   | Feature                                               | Status      |
| --- | ----------------------------------------------------- | ----------- |
| D1  | Sales DB schema (`ticket_sales`, `ticket_sale_lines`) | Not started |
| D2  | Record sale service (insert sale and lines)           | Not started |
| D3  | Atomic capacity decrement (single ticket type)        | Not started |
| D4  | Multi-type cart: row locks and single transaction     | Not started |
| D5  | `CAPACITY_EXCEEDED` domain error and envelope details | Not started |
| D6  | Idempotency key storage and replay for sale creation  | Not started |
| D7  | Sales channel enum (online, in_person, import)        | Not started |
| D8  | List sales for an event (staff)                       | Not started |
| D9  | Remaining capacity on ticket type responses           | Not started |
| D10 | Concurrent sales integration tests                    | Not started |


---



## E. In-person sales (POS)


| #   | Feature                                                 | Status      |
| --- | ------------------------------------------------------- | ----------- |
| E1  | Staff API: create in-person sale                        | Not started |
| E2  | Manual payment confirmation (no card capture at launch) | Not started |
| E3  | Staff POS mode UI (tablet-first)                        | Not started |
| E4  | POS: pick event, ticket types, and quantities           | Not started |
| E5  | POS: live remaining capacity feedback                   | Not started |
| E6  | POS: sale confirmation and receipt summary              | Not started |
| E7  | In-person sale E2E test                                 | Not started |


---



## F. Online sales (Storefront)


| #   | Feature                                            | Status      |
| --- | -------------------------------------------------- | ----------- |
| F1  | Payment provider boundary interface in Go          | Not started |
| F2  | Dev or stub payment implementation                 | Not started |
| F3  | Public API: initiate checkout                      | Not started |
| F4  | Public API: confirm payment and record online sale | Not started |
| F5  | Guest checkout (no customer account)               | Not started |
| F6  | Storefront: ticket selection and cart UI           | Not started |
| F7  | Storefront: checkout flow                          | Not started |
| F8  | Storefront: sold-out and capacity errors in UI     | Not started |
| F9  | Online sale idempotency (retry-safe checkout)      | Not started |
| F10 | Online sale E2E test                               | Not started |


---



## G. Sale import (External Platforms)


| #   | Feature                                                             | Status      |
| --- | ------------------------------------------------------------------- | ----------- |
| G1  | Import batch schema (batch record and line audit)                   | Not started |
| G2  | CSV parser (columns per technical design)                           | Not started |
| G3  | Staff API: upload CSV sale import (synchronous, single transaction) | Not started |
| G4  | All-or-nothing batch: rollback on any oversell                      | Not started |
| G5  | `IMPORT_BATCH_FAILED` with first failing row                        | Not started |
| G6  | Row limit (~10,000) and validation errors                           | Not started |
| G7  | Staff UI: CSV upload and success or failure feedback                | Not started |
| G8  | Sale import integration tests                                       | Not started |


---



## H. Members, roles, and permissions


| #   | Feature                                                                   | Status      |
| --- | ------------------------------------------------------------------------- | ----------- |
| H1  | Event assignment schema (member to event, role)                           | Not started |
| H2  | Staff API: add member to organization (by email)                          | Not started |
| H3  | Staff API: assign Event Owner or Event Staff to event                     | Not started |
| H4  | Permission checks on catalog routes                                       | Not started |
| H5  | Permission checks on sales routes (Event Staff scoped to assigned events) | Not started |
| H6  | Org Admin equivalent to Event Owner within event scope (launch rule)      | Not started |
| H7  | Staff UI: member list                                                     | Not started |
| H8  | Staff UI: invite or add member                                            | Not started |
| H9  | Staff UI: assign staff to events                                          | Not started |
| H10 | Switch organization polish                                                | Partial     |


At launch, Org Admin, Event Owner, and Integration Partner have equivalent authority within their scope.
Event Staff can manage catalog and sell only on assigned Events.
See [business-intent.md](./business-intent.md) for the full permission model.

---



## I. Integration Partner API


| #   | Feature                                                    | Status      |
| --- | ---------------------------------------------------------- | ----------- |
| I1  | Integration credentials schema (API keys)                  | Not started |
| I2  | Integration auth middleware (separate from staff sessions) | Not started |
| I3  | Staff API: create and revoke integration credentials       | Not started |
| I4  | Integration routes: Event CRUD                             | Not started |
| I5  | Integration routes: Ticket Type CRUD                       | Not started |
| I6  | Integration routes: record sales (parity with staff sales) | Not started |
| I7  | Integration routes: list sales and read capacity           | Not started |
| I8  | Org-wide scope enforcement (launch rule)                   | Not started |
| I9  | OAuth2 client credentials (beyond API keys, if needed)     | Deferred    |
| I10 | Partner-facing API documentation                           | Not started |


Integration Partners are distinct from External Platforms.
External Platforms are where sales happen elsewhere and get imported in.
Integration Partners push and pull management operations into Ticket POS.

---



## J. Cross-cutting polish and launch


| #   | Feature                                                             | Status      |
| --- | ------------------------------------------------------------------- | ----------- |
| J1  | Production email provider (Resend, SES, Postmark, or similar)       | Not started |
| J2  | Production payment provider selection and wiring                    | Not started |
| J3  | Production hosting and deploy pipeline                              | Not started |
| J4  | E2E smoke: organization to event to online sale to correct capacity | Not started |
| J5  | E2E smoke: in-person sale and import to correct capacity            | Not started |
| J6  | Structured logging audit across all modules                         | Partial     |
| J7  | Staff dashboard (events overview and quick links)                   | Not started |


---



## Out of scope (not on this roadmap)

The following are explicitly deferred per business intent and technical design.


| Topic                                                                         | Notes                                     |
| ----------------------------------------------------------------------------- | ----------------------------------------- |
| Per-event Integration scope                                                   | Org-wide at launch                        |
| Different permission sets for Org Admin, Event Owner, and Integration Partner | Equivalent at launch                      |
| Customer accounts                                                             | Guest checkout only                       |
| Ticket tier as separate from Ticket Type                                      | Same concept; use Ticket Type only        |
| Informational-only sale imports                                               | All imports count against capacity        |
| JSON or async sale import                                                     | CSV and synchronous at launch             |
| Subdomains or custom domains                                                  | Path-based URLs only                      |
| Postgres row-level security                                                   | Application-layer tenancy at launch       |
| Stripe Terminal or in-person card capture                                     | Record sale first; card capture later     |
| Metrics and distributed tracing                                               | Structured logs and request IDs at launch |


---



## Related documents

- [business-intent.md](./business-intent.md) - business scope, actors, and product decisions
- [technical-design.md](./technical-design.md) - technology stack and architecture
- [design/README.md](./design/README.md) - UI/UX guidelines and design system
- [CONTEXT.md](../CONTEXT.md) - canonical domain glossary
- [prd-staff-authentication.md](./prd-staff-authentication.md) - staff auth PRD
- [testing.md](./testing.md) - HTTP integration testing guide (A13)
- [prd-integration-testing.md](./prd-integration-testing.md) - integration testing initiative PRD

