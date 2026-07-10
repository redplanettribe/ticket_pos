# Organization Settings (Staff)

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).

## Problem Statement

Org Admins have no way to manage their Organization after onboarding.
They cannot edit the org profile, add or remove Members, assign Event access for delegated staff, or delete the Organization.
Member provisioning today requires database seeds, which blocks realistic multi-person workflows and makes delegation impossible to test without manual SQL.

## Solution

Add a **Settings** page in the Staff app (`/settings`), visible only to Org Admins.
The page has four sections: **Profile** (edit org name; slug read-only), **Members** (pre-provision members by email with role management), **Event access** (event-centric assignment UI), and **Danger zone** (delete organization with typed-name confirmation).

Backend staff APIs back each section.
A reusable permission layer enforces Org Admin access on Settings routes immediately and will gate catalog, sales, and import routes as those features land.
Event assignments use model C: Org Admins bypass assignments; all other Members need per-event assignments with a per-event role.

## User Stories

1. As an Org Admin, I want to open Settings from the Staff sidebar, so that I can manage my Organization in one place.
2. As an Org Admin, I want Settings hidden from the sidebar when I am not an Org Admin, so that I am not shown controls I cannot use.
3. As an Event Staff Member who is not an Org Admin, I want a direct visit to `/settings` to return forbidden, so that org management stays restricted.
4. As an Org Admin, I want to see my Organization name and slug on the Profile section, so that I know which org I am managing.
5. As an Org Admin, I want to edit my Organization name, so that the display name matches how we present ourselves.
6. As an Org Admin, I want the Organization slug shown as read-only, so that I understand my Storefront URL is stable and cannot be changed accidentally.
7. As an Org Admin, I want name edits saved with clear success feedback, so that I know the change took effect.
8. As an Org Admin, I want validation errors when the name is empty or invalid, so that I cannot save broken profile data.
9. As an Org Admin, I want to see a list of all Members in my Organization, so that I know who has access.
10. As an Org Admin, I want each Member row to show email, org-level role, and when they joined, so that I can audit the roster.
11. As an Org Admin, I want to add a Member by email address, so that they can sign in and join without manual database work.
12. As an Org Admin, I want to choose an org-level role when adding a Member (`org_admin`, `event_owner`, or `event_staff`), so that I can classify them appropriately.
13. As an Org Admin, I want the added Member to exist immediately (pre-provision), so that they appear in the list without waiting for an invite acceptance flow.
14. As a person pre-provisioned as a Member, I want to sign in with OTP using that email, so that I can access the Organization I was added to.
15. As an Org Admin, I want adding a duplicate email in the same Organization to fail with a clear error, so that I do not create duplicate Member rows.
16. As an Org Admin, I want to add the same email to a different Organization successfully, so that people who work multiple venues are supported.
17. As an Org Admin, I want to change a Member's org-level role, so that I can promote or adjust responsibilities.
18. As an Org Admin, I want to remove a Member from the Organization, so that people who leave lose access.
19. As an Org Admin, I want to be blocked from removing the last Org Admin in the Organization, so that the org is never left without an administrator.
20. As an Org Admin, I want to be blocked from removing myself, so that I must transfer admin responsibility before leaving.
21. As an Org Admin, I want to be blocked from demoting the last Org Admin, so that admin capability is never accidentally eliminated.
22. As an Org Admin, I want removed Members' event assignments deleted automatically, so that there is no ghost access.
23. As an Org Admin, I want an Event access section listing my Organization's Events, so that I can manage who works each Event.
24. As an Org Admin, I want to expand an Event and see its current assignments, so that I know who is already attached.
25. As an Org Admin, I want to assign a Member to an Event with a per-event role (`event_owner` or `event_staff`), so that delegated access is scoped correctly.
26. As an Org Admin, I want to remove an Event assignment, so that a Member loses access to that Event.
27. As an Org Admin, I want Org Admins to not require Event assignments, so that admin access stays org-wide without redundant rows.
28. As an Org Admin with no Events yet, I want Event access to show a helpful empty state, so that I know to create Events first.
29. As an Event Staff Member assigned to Event A only, I want to be denied access to Event B's workflows, so that delegation is enforced (when those routes exist).
30. As an Org Admin, I want to delete my Organization from a Danger zone section, so that I can tear down a test or unused org.
31. As an Org Admin, I want to type the Organization name to confirm deletion, so that accidental deletes are unlikely.
32. As an Org Admin, I want deletion to permanently remove the Organization and all dependent data, so that nothing is left orphaned.
33. As a Member whose active Session pointed at the deleted Organization, I want my active Member cleared, so that I am routed back to org selection or onboarding instead of a broken state.
34. As a Member on another Organization unaffected by the delete, I want my Session unchanged, so that unrelated work is not disrupted.
35. As an Org Admin, I want Settings API calls rejected when I am not an Org Admin, so that authorization is enforced server-side not only in the UI.
36. As a developer, I want integration tests covering Settings APIs and permission failures, so that org management behavior stays guarded as the codebase evolves.

## Implementation Decisions

### Staff UI

- Replace **Team** nav item with **Settings** (`/settings`).
- Show Settings in the sidebar only when the active Member's role is `org_admin`.
- Remove the `/team` route (or redirect to `/settings` if bookmarks exist).
- Settings page layout: single page with sections - Profile, Members, Event access, Danger zone.
- Profile form: editable name, read-only slug (with helper text explaining Storefront URL stability).
- Members section: table/list with add-member form (email + role), inline or dialog role change, remove with confirmation dialog.
- Event access section: event-centric list; expand each Event to manage assignments (member picker + per-event role).
- Danger zone: delete button opens blocking dialog; user must type org name exactly to enable confirm; destructive styling per design system.
- Non-org-admin deep link: forbidden page state driven by API 403.
- Staff BFF routes proxy new backend endpoints following existing auth proxy patterns.

### Access model

- Settings surface is **Org Admin only** (`org_admin` role on active Member).
- **Event assignment** model C:
  - Org Admins have org-wide authority without assignment rows.
  - Non-admin Members require an **Event assignment** (member + event + role of `event_owner` or `event_staff`) to act on that Event.
  - Org-level role on the Member record (`event_owner` / `event_staff`) is metadata/default when adding; effective event access for non-admins comes from assignments.
- At launch within an assigned Event, Event Owner and Event Staff remain equivalent in authority (existing launch rule).

### Schema

- `organizations`: no slug mutation; add update support for `name` only.
- `event_assignments` (new): `id`, `member_id` FK, `event_id` FK, `role` (`event_owner` | `event_staff`), `created_at`; unique `(member_id, event_id)`; cascades on member/event/org delete.
- `events` table and minimal Event persistence required for Event access UI and assignments (name, slug, organization_id, timestamps at minimum if full catalog slice not yet present).

### Staff API (identity / organization management)

All routes require Session auth + active Member scoped to the Organization.
All routes require Org Admin unless noted.

| Method | Route | Purpose |
|--------|-------|---------|
| GET | `/api/v1/staff/organization` | Read org profile (name, slug) |
| PATCH | `/api/v1/staff/organization` | Update org name |
| DELETE | `/api/v1/staff/organization` | Delete org (body: confirmation name) |
| GET | `/api/v1/staff/members` | List members |
| POST | `/api/v1/staff/members` | Add member (email, role) |
| PATCH | `/api/v1/staff/members/{id}` | Change role |
| DELETE | `/api/v1/staff/members/{id}` | Remove member |
| GET | `/api/v1/staff/events` | List org events (for Event access section) |
| GET | `/api/v1/staff/events/{id}/assignments` | List assignments for event |
| PUT | `/api/v1/staff/events/{id}/assignments/{memberId}` | Upsert assignment with per-event role |
| DELETE | `/api/v1/staff/events/{id}/assignments/{memberId}` | Remove assignment |

Email normalized to lowercase trimmed on add (match existing identity normalization).

### Member guardrails (service layer)

- Reject add when `(organization_id, email)` already exists.
- Reject remove or demote when target is the sole `org_admin` in the org.
- Reject remove when target is the acting Member (self-removal).
- On member delete: cascade delete event assignments for that member.

### Organization delete

- Confirmation: request body must include `confirmation_name` matching org name exactly (case-sensitive match on stored name).
- Hard delete organization row; FK cascades handle members, events, assignments, and downstream catalog/sales data as those tables exist.
- After delete: set `sessions.active_member_id = NULL` for any session whose active member belonged to the deleted org.
- Return success envelope; Staff UI signs out or redirects to org picker/onboarding based on remaining memberships.

### Permission infrastructure

- Introduce reusable authorization helpers/middleware (e.g. `RequireOrgAdmin`, `RequireEventAccess(eventID)`).
- Apply `RequireOrgAdmin` on all Settings routes immediately.
- `RequireEventAccess`: org_admin passes; otherwise require matching event assignment row.
- Wire `RequireEventAccess` onto catalog, sales, and import staff routes as those handlers land; do not block this slice on every downstream route existing today.

### Error codes (illustrative)

- `FORBIDDEN` - not Org Admin, or no active Member
- `MEMBER_NOT_FOUND`, `MEMBER_ALREADY_EXISTS`
- `LAST_ORG_ADMIN` - cannot remove/demote sole admin
- `CANNOT_REMOVE_SELF`
- `ORGANIZATION_DELETE_CONFIRMATION_MISMATCH`
- `EVENT_NOT_FOUND`, `ASSIGNMENT_NOT_FOUND`
- Standard validation envelope for empty name, invalid email, invalid role

### Swagger

- Regenerate OpenAPI docs after handler changes.

## Testing Decisions

### Seam

**Single primary seam: HTTP integration tests** in `backend/integration/` (new `organization_settings_test.go` or equivalent), following `auth_test.go` and `harness_test.go` patterns.

Tests exercise routing, Session middleware, Org Admin authorization, services, repositories, migrations, and response envelopes in one pass.
No unit tests on unexported helpers; no per-layer mocks.

### What makes a good test

- Request in, HTTP response out.
- Assert status, envelope shape (`data`, `error`, `request_id`), and `error.code` on failures.
- Use domain vocabulary in test names (Organization, Member, Org Admin, Event assignment).
- Prefer API helpers (`verifyOTP`, `createOrganization`, future `addMember`) over raw SQL.
- SQL seed only when no public API can set up the scenario (comment why).

### Scenarios to cover

- Org Admin reads and updates org name; slug unchanged in response.
- Non-org-admin Member receives 403 on Settings routes.
- Add member pre-provision; list includes new member; duplicate email in org fails.
- Change role; remove member; last-org-admin and self-removal guardrails.
- Create event (API or documented SQL seed); assign member with per-event role; list assignments; remove assignment.
- Delete org with wrong confirmation name fails; correct name succeeds; org and members gone; affected session `active_member_id` cleared.
- Unaffected sessions/memberships in other orgs remain intact.

### Prior art

- `backend/integration/auth_test.go` - OTP, session, org creation, membership selection, envelope assertions.
- `backend/integration/harness_test.go` - shared Postgres, truncate isolation, HTTP helpers.
- `docs/testing.md` and `docs/prd-integration-testing.md`.

### Out of scope for automated tests in this slice

- Playwright E2E of Settings UI (defer until Staff UI patterns stabilize; integration tests own API contract).
- Permission checks on catalog/sales/import routes not yet implemented (wire helper + tests when routes land).

## Out of Scope

- Invite links, invite codes, or notification emails when adding Members.
- Editing Organization slug (immutable after creation).
- Soft delete or archive for Organizations.
- Blocking org delete when Events have sales (hard delete always).
- Settings visibility or edit rights for Event Owners who are not Org Admins.
- Billing, branding, or other org metadata beyond name and slug.
- Per-event Integration scoping.
- Role divergence between Event Owner and Event Staff within an assigned Event (launch equivalence rule stands).

## Further Notes

- `CONTEXT.md` updated with **Event assignment** glossary entry and sharpened **Org Admin** definition.
- `docs/design/staff.md` should be updated to replace Team nav with Settings when implementing.
- Event access depends on Events existing in schema/API; if catalog slice is not ready, ship minimal events persistence and list endpoint to unblock assignments.
- Roadmap V7 items overlap this work but are not sequencing constraints; this spec is the source of truth for the feature.
