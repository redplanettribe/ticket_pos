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
  → V6  Sale import (Direct off-platform sales from CSV/Excel; builds shared sales spine)
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
| V5 | Customer completes guest checkout online; capacity reflects POS and online together | Online + in-person sales both required at launch | F1–F10, D6, D9 | Done |
| V6 | Staff imports Direct off-platform sales (cash/transfer) from CSV/Excel; capacity reduces or batch fails cleanly; buyers emailed a Sale Confirmation; latest batch undoable | Off-platform sales scenario | D1–D5, G1–G10 | Done |
| V7 | Org Admin invites members and assigns Event Staff to specific Events | Event Staff delegated catalog + sales on assigned Events | H1–H10 | Partial |
| V8 | Integration Partner manages events, catalog, and sales via API keys | Partner-managed events scenario | I1–I8, I10 | Not started |
| V9 | Production email and payment, deploy pipeline, full-path E2E smoke | Success criteria: zero to selling in one session | J1–J7 | Not started |
| VC | Customer signs in to the Storefront and sees the Ticket Sales they own, or opens one straight from their Sale Confirmation | Buyers get a durable identity; the foundation reminders and preferences hang off | K1–K9 | Done |

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
Shipped (spec [#81](https://github.com/redplanettribe/ticket_pos/issues/81), tickets #82–#88):
the `PaymentProvider` boundary with stub and real PayPhone implementations (ADR 0012), the Payment
aggregate, Capacity Holds derived from pending Payments (ADR 0013), guest checkout through the
Storefront redirect flow, and Online Sales in the staff Sales list with Payment Method `payphone`.
Deliberately deferred from the slice: a rescue reconciler for paid-but-never-returned Customers
(PayPhone's 5-minute auto-reversal is the v1 safety net) and programmatic refunds (manual via the
PayPhone dashboard; the boundary reserves the operation).

**V6 - Sale import (Direct source first).**
Synchronous `.csv`/`.xlsx` upload parsed server-side, in a single transaction, all-or-nothing on oversell.
Ships the `direct` Sales Source (the Organization's own cash/transfer sales); `external_platform` reuses the same pipeline later.
Deliberately pulls the shared **sales spine** forward — sales core (D1–D5), the Sale Confirmation artifact, and the `EmailSender`-backed delivery path — because imported buyers are emailed a confirmation just like online sales. That spine is reused by V5 and V8.
See [issue #17](https://github.com/redplanettribe/ticket_pos/issues/17). Staged: spine → import UI → undo.

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

**VC - Customer login.**
Lettered rather than numbered because it does not sit in the V0–V9 line: it shipped ahead of the
online purchase it will eventually attach to, and deliberately so.
Because *any* Ticket Sale mints a Customer — a Sale Import or a door sale, not only a future
checkout — the login is useful the day it lands rather than dark code waiting on V5.
It changes nothing about how a purchase is made.

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
| B13 | OTP rate limiting and attempt lockout                         | Done        |
| B14 | Sliding 14-day session expiry                                 | Partial     |
| B15 | Dev seed organization and pre-provisioned member              | Done        |
| B16 | Auth E2E tests (Playwright)                                   | Not started |


Related PRD: [prd-staff-authentication.md](./prd-staff-authentication.md).

B13 covers per-email, per-IP, and verify-attempt limits. It is now exceeded rather than merely met:
the OTP primitive moved to a platform package with a `purpose` scope and a platform-wide outbound
ceiling when Customer login landed (section K).

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

Vertical slices: originally scoped to **V4**, but **V6** built D1–D5 first (the import feature pulled the shared sales spine forward — see [issue #17](https://github.com/redplanettribe/ticket_pos/issues/17)); the Sales list initiative (#36–#39) shipped D8, and **V5** completed D6 and D9 for online (the spine was extracted channel-agnostic in the process, and D9 now subtracts live Capacity Holds per ADR 0013).


| #   | Feature                                               | Status      |
| --- | ----------------------------------------------------- | ----------- |
| D1  | Sales DB schema (`ticket_sales`, `ticket_sale_lines`) | Done        |
| D2  | Record sale service (insert sale and lines)           | Done        |
| D3  | Atomic capacity decrement (single ticket type)        | Done        |
| D4  | Multi-type cart: row locks and single transaction     | Done        |
| D5  | `CAPACITY_EXCEEDED` domain error and envelope details | Done        |
| D6  | Idempotency key storage and replay for sale creation  | Done        |
| D7  | Sales channel enum (online, in_person, import)        | Done        |
| D8  | List sales for an event (staff)                       | Done        |
| D9  | Remaining capacity on ticket type responses           | Done        |
| D10 | Concurrent sales integration tests                    | Done        |


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

Vertical slice: **V5**. Spec: [issue #81](https://github.com/redplanettribe/ticket_pos/issues/81) (ADRs 0012, 0013).

Shipped beyond the original rows: the real PayPhone provider behind the boundary, Capacity Holds
derived from pending Payments, and the `payphone` Payment Method in the staff Sales list and its
filter. Deferred from the slice: a rescue reconciler for lost redirects and programmatic refunds
(both manual/operator concerns for now — see the V5 slice note).


| #   | Feature                                            | Status      |
| --- | -------------------------------------------------- | ----------- |
| F1  | Payment provider boundary interface in Go          | Done        |
| F2  | Dev or stub payment implementation                 | Done        |
| F3  | Public API: initiate checkout                      | Done        |
| F4  | Public API: confirm payment and record online sale | Done        |
| F5  | Guest checkout (no customer account)               | Done        |
| F6  | Storefront: ticket selection and cart UI           | Done        |
| F7  | Storefront: checkout flow                          | Done        |
| F8  | Storefront: sold-out and capacity errors in UI     | Done        |
| F9  | Online sale idempotency (retry-safe checkout)      | Done        |
| F10 | Online sale E2E test                               | Done        |


---



## G. Sale import (Direct source)

Vertical slice: **V6**. Spec: [issue #17](https://github.com/redplanettribe/ticket_pos/issues/17).
Builds the `direct` Sales Source on top of the sales spine (section D). `external_platform` source is deferred.
Extended by the Manually Recorded Sale (G11-G14): the same Direct Sale typed one at a time instead of
uploaded, belonging to no batch. Spec: [issue #366](https://github.com/redplanettribe/ticket_pos/issues/366)
(ADR 0052). This is deliberately **not** section E - a hand-typed sale is a Direct Sale on the `import`
channel, and the in-person POS on the `in_person` channel remains unstarted.


| #   | Feature                                                                       | Status      |
| --- | ----------------------------------------------------------------------------- | ----------- |
| G1  | `sale_import_batches` schema (batch record, source, actor, counts, status)    | Done        |
| G2  | Server-side `.csv` + `.xlsx` parser (add xlsx lib; columns per technical design) | Done        |
| G3  | Per-event `.xlsx` template with locked ticket-type dropdown + hidden id        | Done        |
| G4  | Staff API: preview (all row errors at once + soft duplicate flags + capacity)  | Done        |
| G5  | Staff API: commit (synchronous, single transaction, idempotency key)           | Done        |
| G6  | All-or-nothing batch: oversell blocked; `IMPORT_BATCH_FAILED`; raise-capacity  | Done        |
| G7  | Confirmation email per sale on commit (via `EmailSender`)                       | Done        |
| G8  | Undo latest batch: reverse sales, restore capacity, opt-in void email          | Done        |
| G9  | Staff UI: Event "Import sales" flow + per-event history; `/imports` → picker    | Done        |
| G10 | Sale import integration tests (+ repository concurrency exception)             | Done        |
| G11 | Staff API: record one sale + preview (batchless, Purchase Limit enforced)      | Not started |
| G12 | Staff UI: record-a-sale modal with a Keep adding session + receipt             | Not started |
| G13 | Sale origin derived (batch / typed / correction) on the Sales list and Export  | Not started |
| G14 | Manually Recorded Sale integration tests                                        | Not started |


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
| J2  | Production payment provider selection and wiring (PayPhone via Secret Manager/Terraform) | Done |
| J3  | Production hosting and deploy pipeline                              | Not started |
| J4  | E2E smoke: organization to event to online sale to correct capacity | Not started |
| J5  | E2E smoke: in-person sale and import to correct capacity            | Not started |
| J6  | Structured logging audit across all modules                         | Partial     |
| J7  | Staff dashboard (events overview and quick links)                   | Not started |


---



## K. Customer identity (Storefront login)

Vertical slice: **VC**.

Reverses the earlier "guest checkout only" stance: a **Customer** is now a real, platform-global
record created *by* the sale rather than by a signup form, and buyers can prove they own it.
Nothing here changes checkout — guest purchase remains, and V5 is unaffected.


| #   | Feature                                                                    | Status |
| --- | -------------------------------------------------------------------------- | ------ |
| K1  | Trusted client IP through the BFFs (fixes a live per-IP rate-limit bypass)  | Done   |
| K2  | OTP primitive extracted to a platform package with a `purpose` scope        | Done   |
| K3  | Platform-wide outbound OTP ceiling and its operational signal               | Done   |
| K4  | `customers` schema; every Ticket Sale creates or reuses a Customer          | Done   |
| K5  | Customer OTP sign-in and the Customer Session (sliding 180 days)            | Done   |
| K6  | Customer Area read: upcoming and past Ticket Sales across all Organizations | Done   |
| K7  | Storefront sign-in page, Customer Area, and header sign-in state            | Done   |
| K8  | Confirmation Link in every Sale Confirmation (sale-scoped, short session)   | Done   |
| K9  | Customer auth E2E (Playwright)                                             | Not started |


Related PRD: [prd-customer-login.md](./prd-customer-login.md). Identity model: [ADR 0010](./adr/0010-customer-identity-platform-global-and-separate-from-staff.md).

K9 is deferred deliberately: B16 (staff auth E2E) is still not started, so there is no auth E2E
pattern to follow yet.


---



## Out of scope (not on this roadmap)

The following are explicitly deferred per business intent and technical design.


| Topic                                                                         | Notes                                     |
| ----------------------------------------------------------------------------- | ----------------------------------------- |
| Per-event Integration scope                                                   | Org-wide at launch                        |
| Different permission sets for Org Admin, Event Owner, and Integration Partner | Equivalent at launch                      |
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

