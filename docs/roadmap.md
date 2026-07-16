# Roadmap

## Purpose

This document is a loose, feature-by-feature implementation plan for Ticket POS.
It is ordered by dependency and aligned with [business-intent.md](./business-intent.md) and [technical-design.md](./technical-design.md).
The canonical domain vocabulary lives in [CONTEXT.md](../CONTEXT.md).

**Vertical slices** (V0–V9) are the recommended build sequence: each slice ships one business outcome across backend, API, and UI.
Sections A–J are horizontal checklists keyed by slice in the vertical slices table.

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

Vertical slices deliver one business outcome end to end.
Horizontal sections A–J below are the feature checklist each slice pulls from.
Order is loose; shift slices when implementation learns something new.

```text
V0  Platform client adoption (api-client in Staff + Storefront)
  → V1  Staff auth complete (OTP hardening, session, auth E2E)
  → V2  Staff catalog (create Event + Ticket Types in Staff UI)
  → V3  Public catalog (storefront event page with real data)
  → V4  In-person sale (POS records sale, capacity decrements)
  → V5  Online sale (guest checkout, shared capacity with POS)
  → V6  Sale import (CSV from External Platforms)
  → V7  Event Staff delegation (assignments + scoped permissions)
  → V8  Integration Partner API (programmatic catalog + sales)
  → V9  Production launch (email, payment, deploy, smoke E2E)
```

### What's next (loose)

The repo is between V0 and V2.
Finish V0 and V1 in parallel if helpful, then start **V2** (staff catalog).
V2 is the first slice that matches a core business-intent goal: an Organization defines Events and Ticket Types.
V3 makes that catalog public; together they are the first user-visible milestone.

---

## Vertical slices

Each slice cuts through schema, API, UI, and tests for one outcome.
Business scenarios in [business-intent.md](./business-intent.md) map to slices as noted.

| Slice | Outcome | Business intent | Refs | Status |
| ----- | ------- | --------------- | ---- | ------ |
| V0 | Staff and Storefront call the Go API through the generated TypeScript client | Platform spine before feature work | A13 | Partial |
| V1 | OTP rate limits, sliding sessions, and Playwright auth coverage | Staff can sign in reliably before selling | B13–B16 | Partial |
| V2 | Org Admin signs in and creates an Event with Ticket Types in Staff UI | Event and catalog management | C1–C8, C13–C14 | Not started |
| V3 | Customer explores events on the global explorer, org page, and event page (read-only) | Catalog visible on the Storefront | C9–C12, C14 | Done |
| V4 | Staff sells at the door on POS; remaining capacity updates immediately | Door sales scenario | D1–D10, E1–E7 | Not started |
| V5 | Customer completes guest checkout online; capacity reflects POS and online together | Online + in-person sales both required at launch | F1–F10, D6, D9 | Not started |
| V6 | Staff uploads a CSV of off-platform sales; capacity reduces or batch fails cleanly | Off-platform sales scenario | G1–G8 | Not started |
| V7 | Org Admin invites members and assigns Event Staff to specific Events | Event Staff delegated catalog + sales on assigned Events | H1–H10 | Partial |
| V8 | Integration Partner manages events, catalog, and sales via API keys | Partner-managed events scenario | I1–I8, I10 | Not started |
| V9 | Production email and payment, deploy pipeline, full-path E2E smoke | Success criteria: zero to selling in one session | J1–J7 | Not started |

### Milestone groupings

These are informal checkpoints, not scope locks.

1. **Catalog live** (V2 + V3) - sign in, create an Event and Ticket Types, public storefront event page renders real data.
2. **In-person selling** (V4) - sell at the door; remaining capacity is accurate.
3. **Online selling** (V5) - guest checkout; capacity reflects online and in-person sales together.
4. **Unified capacity** (V6) - imports count the same way native sales do.
5. **Delegation** (V7) - Event Staff scoped to assigned Events.
6. **Partner access** (V8) - Integration Partner parity with Org Admin without the web UI.
7. **Launch** (V9) - production providers and smoke tests.

### Slice notes

**V0 - Platform client adoption.**
Staff and Storefront still hand-roll fetch calls.
Adopting `packages/api-client` everywhere unblocks consistent error handling and typed requests for catalog and sales work.

**V1 - Staff auth complete.**
Identity is usable but not hardened.
Finish rate limiting, sliding expiry, and auth E2E before leaning on Staff UI for catalog CRUD.

**V2 - Staff catalog.**
Thin vertical: migration, repository, staff routes, Staff UI list/create/edit for Events and Ticket Types.
Org Admin only; permission checks beyond active-member scoping can wait until V7.
At launch, Org Admin and Event Owner are equivalent, so V2 only needs org-scoped access.

**V3 - Public catalog.**
Wire public org/event routes and replace the storefront placeholder with API-backed SSR.
SEO metadata can land in the same slice.
Depends on V2 having at least one real event to render.

**V4 - In-person sale.**
Introduce sales schema and atomic capacity logic, then POS UI.
Manual payment confirmation only (no card capture).
This is the first slice that proves capacity math; include concurrent integration tests here.

**V5 - Online sale.**
Payment provider interface plus dev stub, checkout API, storefront cart.
Reuse sales core and idempotency from V4.
Success criteria expect online and in-person in the same session; V5 completes that pair.

**V6 - Sale import.**
Synchronous CSV upload in a single transaction.
All-or-nothing on oversell matches the External Platform scenario in business intent.

**V7 - Event Staff delegation.**
Members, event assignments, and route-level permission checks.
Deferred until core selling works under Org Admin so V2–V6 stay unblocked.
H10 (org switcher polish) can ride along.

**V8 - Integration Partner API.**
Separate auth from staff sessions; org-wide scope at launch.
Parity with staff catalog and sales routes, not a separate product surface.

**V9 - Production launch.**
Real email and payment providers, hosting, and cross-channel E2E smoke.
Staff dashboard (J7) is nice-to-have before launch, not a blocker for V4–V6.

---



## A. Platform and repo spine

Vertical slice: **V0** (api-client adoption); also underpins all later slices.

The spine is largely in place.
OpenAPI is generated from Go handler annotations (swag), copied to `openapi/openapi.yaml`, and served at `/swagger/` in dev.
The TypeScript client package generates from that spec, but Staff and Storefront still hand-roll fetch calls.
CI runs Go (including integration tests), turbo lint/typecheck/build, and the OpenAPI sync check.

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
| A9  | OpenAPI 3.1 spec (identity routes) and Swagger UI                    | Done        |
| A10 | Typed OpenAPI envelope schemas (`identity/openapi`)                  | Done        |
| A11 | OpenAPI contract test (`identity/openapi/contract_test.go`)          | Done        |
| A12 | Makefile `swagger`, `api-client`, and `openapi-sync-check` targets   | Done        |
| A13 | TypeScript client package (`packages/api-client`, `openapi-fetch`)   | Partial     |
| A14 | Shared UI package (`packages/ui`, Tailwind + shadcn primitives)      | Done        |
| A15 | CI: Go test and vet (includes integration), turbo lint and build     | Done        |
| A16 | CI: OpenAPI to client sync check                                     | Done        |
| A17 | HTTP integration test harness (testcontainers Postgres)              | Done        |
| A18 | Integration testing guide (`docs/testing.md`)                        | Done        |


---



## B. Staff identity and tenancy

Vertical slice: **V1**.


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

Vertical slices: **V2** (staff), **V3** (public storefront).


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
| C9  | Public API: get organization by slug and its discoverable events       | Done        |
| C10 | Public API: get event and ticket types by slugs; global explorer list  | Done        |
| C11 | Storefront SSR event page, org page, and global explorer               | Done        |
| C12 | Storefront SEO metadata (title, Open Graph, canonical URL)             | Done        |
| C13 | Catalog domain errors (`EVENT_NOT_FOUND`, duplicate name, and similar) | Done        |
| C14 | Catalog integration tests                                              | Partial     |
| C15 | Per-event `discoverable` flag (staff UI toggle still pending)          | Partial     |


---



## D. Sales core (shared by all channels)

Vertical slices: **V4** (introduced), **V5** (idempotency for online), **V6** (import).


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

Vertical slice: **V4**.


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

Vertical slice: **V5**.


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

Vertical slice: **V6**.


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

Vertical slice: **V7**.


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

Vertical slice: **V8**.


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

Vertical slice: **V9**.
Platform spine items map to **V0**; staff auth hardening maps to **V1**.


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
- [testing.md](./testing.md) - HTTP integration testing guide (A17, A18)
- [prd-integration-testing.md](./prd-integration-testing.md) - integration testing initiative PRD

