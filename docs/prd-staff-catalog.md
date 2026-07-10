# Staff Catalog (V2)

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).

## Problem Statement

Org Admins can sign in and manage their Organization, but they cannot define what they sell.
The Staff **Events** page is a placeholder.
The database has a minimal `events` table (name and slug only) wired through the identity module for Settings event-assignment management.
There is no `ticket_types` table, no catalog module, and no way to schedule an Event, attach a description or cover image, define Ticket Types, or move an Event through a publish lifecycle.

Without a Staff catalog slice, the product cannot reach its first business milestone: an Organization defines Events and Ticket Types before sales or a public Storefront.

## Solution

Deliver **V2 Staff catalog**: Org Admins create and manage Events and Ticket Types in the Staff app, backed by a new **catalog** module in the Go API.

An Event is a full catalog record: scheduling (start/end, timezone, venue), markdown description, cover image, and lifecycle status (`draft`, `published`, `cancelled`).
Ticket Types belong to an Event with name, description, price (in the Organization's currency), capacity, and display order.
Publishing requires a complete minimum: name, slug, start time, timezone, and at least one Ticket Type.

Staff UI follows the design information architecture: Events list, create Event, Event detail hub for editing, Ticket Types, cover upload, and publish.
Local development gains **MinIO** (S3-compatible object storage) in Docker Compose for cover image uploads via presigned URLs.

Event assignment APIs remain in identity; Event CRUD migrates to catalog.
Access is **Org Admin only** in this slice; assignment-scoped permissions defer to V7.

## User Stories

### Events list and navigation

1. As an Org Admin, I want to open **Events** from the Staff sidebar, so that I can manage my Organization's catalog.
2. As an Org Admin, I want the Events page to list all Events in my active Organization, so that I can see what exists at a glance.
3. As an Org Admin, I want each Event row to show name, start date, and status, so that I can scan upcoming and in-progress work quickly.
4. As an Org Admin, I want a **Create event** action on the Events list, so that I can start a new catalog entry.
5. As an Org Admin, I want to click an Event row to open its detail page, so that I can edit it and manage Ticket Types.
6. As an Org Admin with no Events yet, I want a helpful empty state with a create action, so that I know where to begin.
7. As an Event Staff Member who is not an Org Admin, I want the Events nav item hidden or read-only until delegation lands, so that I am not shown controls I cannot use yet.
8. As an Event Staff Member who is not an Org Admin, I want direct API calls to catalog routes to return forbidden, so that catalog management stays restricted server-side.

### Create and edit Event

9. As an Org Admin, I want to create a new Event with at least a name, so that I can start drafting quickly.
10. As an Org Admin, I want the Event slug pre-filled from the name and editable while the Event is a draft, so that I get a sensible default Storefront URL path.
11. As an Org Admin, I want saving a new Event to create it in **draft** status and redirect me to the Event detail page, so that I can continue setup in one place.
12. As an Org Admin, I want to edit Event name, scheduling fields, venue, and description on the detail page, so that I can flesh out the catalog record.
13. As an Org Admin, I want to set `starts_at` and optional `ends_at` using the Event's timezone, so that door times are unambiguous for staff and customers.
14. As an Org Admin, I want to pick an IANA timezone (e.g. `America/New_York`) for the Event, so that local display matches how the venue thinks about time.
15. As an Org Admin, I want optional venue name and venue address fields, so that customers know where to go.
16. As an Org Admin, I want to author the Event description in Markdown, so that I can include formatted copy without a heavy rich-text editor.
17. As an Org Admin, I want draft saves to persist without meeting publish requirements, so that I can work incrementally.
18. As an Org Admin, I want clear validation errors when fields are invalid (empty name, bad slug, end before start), so that I cannot save broken data.
19. As an Org Admin, I want the Event slug locked after publish, so that a live Storefront URL cannot change accidentally in V3.
20. As an Org Admin, I want to change the slug on a draft Event, so that I can fix typos before going live.

### Cover image

21. As an Org Admin, I want to upload a cover image for an Event, so that the Event has visual identity on Staff and later on the Storefront.
22. As an Org Admin, I want cover upload to use a presigned URL flow, so that large files do not pass through the BFF.
23. As an Org Admin, I want accepted image types limited to JPEG, PNG, and WebP with a reasonable size cap, so that uploads stay safe and fast.
24. As an Org Admin, I want to see a preview of the current cover image on the Event detail page, so that I know what will display publicly.
25. As an Org Admin, I want to replace or remove a cover image on a draft or published Event, so that I can fix mistakes.

### Event lifecycle (publish and cancel)

26. As an Org Admin, I want an explicit **Publish** action on the Event detail page, so that I control when an Event becomes sellable.
27. As an Org Admin, I want publish to require name, slug, `starts_at`, timezone, and at least one Ticket Type, so that published Events are viable catalog entries.
28. As an Org Admin, I want publish blocked with a clear error when requirements are missing, so that I know what to fix.
29. As an Org Admin, I want a published Event to show **published** status on the list and detail pages, so that I can distinguish live catalog entries from drafts.
30. As an Org Admin, I want a **Cancel event** action on published Events, so that I can mark an Event as no longer active.
31. As an Org Admin, I want cancelled Events to remain in the database with **cancelled** status, so that there is an audit trail and stable identifiers.
32. As an Org Admin, I want cancelled to be a terminal status (no republish in V2), so that lifecycle rules stay simple until sales exist.

### Event and Ticket Type deletion

33. As an Org Admin, I want to delete a draft Event, so that I can discard work in progress.
34. As an Org Admin, I want deleting a draft Event to remove its Ticket Types and event assignments, so that no orphaned rows remain.
35. As an Org Admin, I want delete blocked on published or cancelled Events, so that live or historical Events are not removed accidentally.
36. As an Org Admin, I want to delete a Ticket Type while the parent Event is a draft, so that I can fix catalog mistakes before publish.
37. As an Org Admin, I want Ticket Type deletion blocked once the parent Event is published, so that published catalog structure stays stable until sales checks exist in V4.

### Ticket Types

38. As an Org Admin, I want to manage Ticket Types on the Event detail page, so that ticket categories live with their Event.
39. As an Org Admin, I want to add a Ticket Type with name, price, and capacity, so that I can define what is sold.
40. As an Org Admin, I want an optional plain-text description on a Ticket Type, so that I can clarify what each ticket includes.
41. As an Org Admin, I want Ticket Type price entered in my Organization's currency, so that amounts match how we price locally.
42. As an Org Admin, I want Ticket Types listed with name, price, capacity, and sort order, so that I can review the catalog at a glance.
43. As an Org Admin, I want to reorder Ticket Types, so that display order on Staff and later Storefront matches my intent.
44. As an Org Admin, I want to edit Ticket Type fields on draft and published Events, so that I can adjust copy and pricing before sales land.
45. As an Org Admin, I want `sold_count` to start at zero on new Ticket Types, so that capacity tracking is ready for V4/V5 without a schema change later.

### Organization currency

46. As an Org Admin, I want new Organizations to default to USD currency, so that pricing works out of the box.
47. As an Org Admin, I want to set my Organization's currency in Settings → Profile, so that I can match our market before creating Ticket Types.
48. As an Org Admin, I want currency locked after the first Ticket Type is created in the Organization, so that existing prices are not silently reinterpreted in another currency.
49. As an Org Admin, I want currency shown read-only in Settings once locked, so that I understand why it cannot change.

### Settings and assignments integration

50. As an Org Admin, I want Settings → Event access to keep listing Events from the catalog, so that assignment management still works after the catalog migration.
51. As an Org Admin, I want event assignments on draft Events I create from the Events flow to appear in Settings, so that delegation UI stays consistent.

### API and authorization

52. As an Org Admin, I want catalog staff APIs scoped to my active Member's Organization, so that I never see another org's Events.
53. As an Org Admin, I want non-org-admin Members to receive forbidden on catalog staff routes, so that authorization is enforced server-side.
54. As a developer, I want catalog domain errors with stable codes (`EVENT_NOT_FOUND`, `EVENT_SLUG_TAKEN`, publish validation codes), so that clients can handle failures consistently.

### Local development

55. As a developer, I want MinIO in Docker Compose for S3-compatible object storage, so that cover image upload works locally without cloud credentials.
56. As a developer, I want the covers bucket bootstrapped with public-read on the covers prefix, so that Staff and Storefront can render image URLs directly.

### Testing

57. As a developer, I want HTTP integration tests for the catalog staff API, so that publish rules, slug uniqueness, and delete guards stay correct in CI.

## Implementation Decisions

### Scope and access

- This slice is **roadmap V2** only: Staff catalog for Events and Ticket Types.
- **Org Admin only** (`org_admin` on active Member); reuse existing `RequireOrgAdmin` middleware on catalog routes.
- Event assignment routes remain in **identity**; Event CRUD and Ticket Type CRUD move to a new **catalog** module.
- Same staff URL paths where possible (`/api/v1/staff/events`, etc.) so Staff BFF changes stay small.

### Backend module layout

- Create `catalog` module with repository, service, handler, and domain errors for Events and Ticket Types.
- Migrate Event list/create/update/delete/publish/cancel from identity into catalog.
- Identity retains event assignment persistence and `HasEventAccess` for future V7 route guards; catalog owns the `events` and `ticket_types` tables.
- Introduce a pluggable **object storage** boundary (S3-compatible) for presigned PUT and public URL construction; MinIO in dev, configurable endpoint/credentials via environment.

### Schema

**organizations** (extend):

- Add `currency` (ISO 4217 code, default `USD`).

**events** (extend existing table):

- `status` enum: `draft`, `published`, `cancelled` (default `draft`)
- `starts_at`, `ends_at` (`TIMESTAMPTZ`, nullable until set)
- `timezone` (IANA text, nullable until set)
- `venue_name`, `venue_address` (optional text)
- `description` (optional markdown text)
- `cover_image_key` (optional text; object key in covers bucket)
- Retain `name`, `slug`, `organization_id`, `created_at`; unique `(organization_id, slug)`

**ticket_types** (new):

- `id`, `event_id` FK (cascade on event delete), `organization_id` FK (denormalized for org scoping queries)
- `name` (required), `description` (optional plain text)
- `price_cents` (required, non-negative integer)
- `capacity` (required, positive integer)
- `sold_count` (default 0, non-negative)
- `sort_order` (integer, default 0)
- `created_at`, `updated_at`

### Event lifecycle rules

Publish (`draft` → `published`) requires:

- `name`, `slug`, `starts_at`, `timezone` present and valid
- At least one Ticket Type on the Event

`cancelled` is terminal in V2 (no `cancelled` → `draft` or republish).

Slug editable only while `status = draft`; immutable after publish.

Delete Event: allowed only for `draft`; cascades Ticket Types and event assignments.

Delete Ticket Type: allowed only while parent Event is `draft`.

### Cover image upload

- `POST /api/v1/staff/events/{eventID}/cover-upload-url` returns presigned PUT URL, expected object key, and resulting public URL.
- Client uploads directly to object storage, then `PATCH` Event with `cover_image_key` (or dedicated cover attach endpoint).
- Accept `image/jpeg`, `image/png`, `image/webp`; max size ~5 MB.
- Object key pattern includes organization and event identifiers to avoid collisions.
- Public-read bucket prefix for covers; API returns stable public URL in Event responses.

### Organization currency

- Default `USD` on Organization create (no currency picker on onboarding form).
- Settings → Profile: currency dropdown editable until any Ticket Type exists in the Organization.
- After first Ticket Type: currency read-only; attempts to change return a domain error.
- Ticket Type API accepts price in cents; responses include Organization currency for display.

### Staff API (catalog)

All routes require Session auth, active Member, and Org Admin.

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/v1/staff/events` | List Events for org (summary fields for list UI) |
| POST | `/api/v1/staff/events` | Create draft Event |
| GET | `/api/v1/staff/events/{id}` | Get Event with Ticket Types |
| PATCH | `/api/v1/staff/events/{id}` | Update Event fields (respect slug/status rules) |
| DELETE | `/api/v1/staff/events/{id}` | Delete draft Event only |
| POST | `/api/v1/staff/events/{id}/publish` | Publish Event (validates requirements) |
| POST | `/api/v1/staff/events/{id}/cancel` | Cancel published Event |
| POST | `/api/v1/staff/events/{id}/cover-upload-url` | Presigned cover upload URL |
| GET | `/api/v1/staff/events/{id}/ticket-types` | List Ticket Types for Event |
| POST | `/api/v1/staff/events/{id}/ticket-types` | Create Ticket Type |
| PATCH | `/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}` | Update Ticket Type |
| DELETE | `/api/v1/staff/events/{id}/ticket-types/{ticketTypeId}` | Delete Ticket Type (draft Event only) |

Identity retains assignment routes under `/api/v1/staff/events/{id}/assignments/...`.

Settings `PATCH /api/v1/staff/organization` extended to support currency update with lock guard.

### Staff UI

- Replace Events placeholder with real list: name, formatted start date (in Event timezone), status badge, link to detail.
- `/events/new`: Event fields only; save creates draft and redirects to `/events/[id]`.
- `/events/[id]`: detail hub with sections for details form, cover image, Ticket Types list/add/edit, publish/cancel/delete actions as appropriate to status.
- Breadcrumbs: `Events / {Event name}` (and Ticket Types context within detail).
- Destructive actions use blocking Dialog confirmation per design system.
- On save success: toast and stay on page (no redirect away from edit forms).
- Non-org-admin: hide Settings-style restriction; catalog pages return forbidden from API.
- Staff BFF routes proxy catalog endpoints following existing auth proxy patterns.
- Markdown description: textarea in Staff; preview optional but not required in V2.

### Docker Compose and MinIO

- Add MinIO service with console and API ports for local dev.
- Backend environment: S3 endpoint, bucket name, access key, public base URL for cover URLs.
- Init job or entrypoint script: create bucket, set public-read policy on `covers/` prefix.
- Backend depends on MinIO for local cover upload flows.

### Error codes (illustrative)

- `EVENT_NOT_FOUND`, `EVENT_SLUG_TAKEN`
- `EVENT_NOT_DRAFT` (slug change or delete on non-draft)
- `EVENT_DELETE_FORBIDDEN` (published/cancelled delete)
- `EVENT_PUBLISH_REQUIREMENTS_NOT_MET` (with `details` listing missing fields)
- `EVENT_ALREADY_PUBLISHED`, `EVENT_ALREADY_CANCELLED`
- `TICKET_TYPE_NOT_FOUND`
- `TICKET_TYPE_DELETE_FORBIDDEN` (published parent)
- `CURRENCY_LOCKED`
- `VALIDATION_ERROR` with field map for forms
- `FORBIDDEN` for non-org-admin

### Swagger and api-client

- Regenerate OpenAPI after handler changes; Staff and tests consume generated client where applicable.

### Event status state machine (prototype shape)

```
draft --publish--> published --cancel--> cancelled
  |                    |
  delete               delete forbidden
```

## Testing Decisions

### Seam

**Single primary seam: HTTP integration tests** in `backend/integration/` (new catalog test file), following `auth_test.go`, `organization_settings_test.go`, and `harness_test.go` patterns.

Tests exercise routing, Session middleware, Org Admin authorization, catalog services, repositories, migrations, and response envelopes in one pass.
No unit tests on unexported helpers; no per-layer mocks.
No Playwright E2E in this slice.

### What makes a good test

- Request in, HTTP response out.
- Assert status, envelope shape (`data`, `error`, `request_id`), and `error.code` on failures.
- Use domain vocabulary in test names (Organization, Event, Ticket Type, Org Admin, publish, draft).
- Prefer API helpers (`verifyOTP`, create organization, create event, add ticket type) over raw SQL.
- SQL seed only when no public API can set up the scenario (comment why).
- Cover image upload may be tested at the presign endpoint boundary without full MinIO in CI if storage is mocked at the seam; if MinIO runs in test harness, assert key and URL shape.

### Scenarios to cover

- Org Admin creates draft Event; list and get return it with `draft` status.
- Update Event fields including slug on draft; slug change rejected after publish.
- Duplicate event slug in org returns `EVENT_SLUG_TAKEN`.
- Add Ticket Type; list on Event includes it with `sold_count` 0.
- Publish succeeds when name, slug, starts_at, timezone, and ≥1 Ticket Type present.
- Publish fails without Ticket Types; fails without starts_at or timezone.
- Cancel published Event; status `cancelled`; cancel/publish rejected appropriately after.
- Delete draft Event succeeds; delete published Event returns `EVENT_DELETE_FORBIDDEN`.
- Delete Ticket Type on draft Event succeeds; delete on published Event returns `TICKET_TYPE_DELETE_FORBIDDEN`.
- Non-org-admin Member receives 403 on catalog routes.
- Organization currency defaults USD; update in Settings before first Ticket Type; locked after first Ticket Type with `CURRENCY_LOCKED` on change attempt.

### Prior art

- `backend/integration/auth_test.go` — OTP, session, org creation, envelope assertions.
- `backend/integration/organization_settings_test.go` — Org Admin guards, members, events list for assignments.
- `backend/integration/harness_test.go` — shared Postgres, truncate isolation, HTTP helpers.
- `docs/testing.md` and `docs/prd-integration-testing.md`.

### Out of scope for automated tests in this slice

- Playwright E2E of Events UI, markdown editor, or cover upload widget.
- Public Storefront API (V3).
- Event assignment permission checks on catalog routes (V7).
- Full end-to-end binary upload to MinIO in CI (unless harness adds MinIO; presign contract is minimum).

## Out of Scope

- V3 public Storefront API and SSR event page with real data.
- Online Sale, In-Person Sale, POS mode, capacity decrement, and `sold_count` mutation.
- Sale Import.
- Event Staff / Event Owner assignment-scoped catalog permissions (V7).
- Ticket Type sale windows (`sales_start_at` / `sales_end_at`) and per-order min/max limits.
- Rich-text WYSIWYG description editor.
- Multiple cover images or image gallery.
- Event slug redirects when slug changes (slug locked after publish instead).
- Hard delete of published or cancelled Events.
- Republish or reopen cancelled Events.
- Per-Event or per-Ticket-Type currency (Organization currency only).
- Currency picker on Organization create form.
- Integration Partner catalog routes (V8).

## Further Notes

- Delivers roadmap **V2** (items C1–C8, C13–C14) and unblocks **V3** public catalog.
- Existing minimal Event create/list in identity moves to catalog; Settings Event access continues to use `GET /api/v1/staff/events`.
- `CONTEXT.md` updated with **Event status** and sharpened **Event** / **Ticket Type** glossary entries.
- `docs/design/staff.md` Events IA (`Events list → Event detail → Ticket Types`) is the UI source of truth.
- MinIO addition is local-dev infrastructure; production object storage provider remains TBD (S3-compatible contract).
- Roadmap status for C1–C8 should move to in progress when implementation starts.
