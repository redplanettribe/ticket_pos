# Organization Create and Switch (Staff)

Tracker: [#6](https://github.com/redplanettribe/ticket_pos/issues/6)

Spec synthesized from domain grilling session.
Canonical vocabulary: [CONTEXT.md](../CONTEXT.md).

## Problem Statement

Staff users can only create an Organization during first-time onboarding (zero memberships).
Once they belong to an Organization and have an active Member, there is no self-serve path to create another Organization.
Organization switching exists only as a hidden dropdown in the user menu and only appears when the user already has two or more memberships.
Single-org users cannot discover how to add a second venue, and multi-org users lack a prominent, consistent control for changing workspace context.

## Solution

Give every authenticated staff person a unified way to **create** and **switch** their active Organization.

- Click the **Organization name** in the Staff shell header (sidebar on desktop, top bar on mobile) to open an org switcher **Dialog**.
- The dialog lists all memberships (name, slug, role), marks the active one, and lets the user switch with one click.
- A **Create organization** action in the dialog navigates to a dedicated create page.
- Create and switch are also available on auth-gate pages (before an active Member is set).
- After any switch or successful create, redirect to the dashboard (`/`).
- Retire the old onboarding-only create route in favor of one canonical create page.

No new backend APIs are required; existing organization creation and session organization selection endpoints already support this behavior.

## User Stories

1. As a staff person with zero memberships, I want to create my first Organization after signing in, so that I can start using the product.
2. As a staff person who already belongs to one or more Organizations, I want to create another Organization at any time, so that I can spin up a new venue without signing out or manual database work.
3. As a staff person creating an additional Organization, I want to become Org Admin of the new Organization automatically, so that I can manage it immediately.
4. As a staff person who creates a new Organization, I want it to become my active Member, so that I land in the right workspace context without an extra selection step.
5. As a staff person with multiple memberships, I want to switch my active Organization without signing out, so that I can work across venues efficiently.
6. As a staff person with only one membership, I want a visible path to create a second Organization, so that I am not stuck in a single-org dead end.
7. As a staff person working in the app, I want to open an org switcher from the Organization name in the shell header, so that create and switch live in one obvious place.
8. As a staff person on mobile, I want the same org switcher from the mobile header, so that switching works on small screens.
9. As a staff person viewing the org switcher, I want each membership row to show Organization name, slug, and my role, so that I can tell venues apart before switching.
10. As a staff person viewing the org switcher, I want the active Organization clearly marked, so that I know which workspace I am in.
11. As a staff person switching Organization, I want to land on the dashboard afterward, so that I am not left on a page scoped to the previous Organization.
12. As a staff person who just created an Organization, I want to land on the dashboard afterward, so that I start in a neutral home for the new workspace.
13. As a staff person at the login gate with two or more memberships, I want a full-page organization picker, so that I must choose a workspace before entering the app.
14. As a staff person at the login gate with two or more memberships, I want a **Create organization** action on the picker page, so that I can create a new venue before selecting an existing one.
15. As a staff person signing in with exactly one membership, I want that Organization auto-selected, so that I am not forced through a pointless picker.
16. As a staff person signing in with zero memberships, I want to be sent to the create page, so that onboarding is frictionless.
17. As a staff person creating an Organization before I have an active Member, I want the create page in auth layout (centered card, logout available), so that the flow matches other gate pages.
18. As a staff person creating an Organization while already working in an Organization, I want the create page inside the normal app shell with a way to cancel, so that in-app create feels like normal navigation.
19. As a staff person who bookmarked the old onboarding create URL, I want to be redirected to the canonical create page, so that old links keep working.
20. As a staff person with an active Member who navigates to the gate-only picker URL, I want to be redirected to the dashboard, so that gate pages are not reachable in-app.
21. As a staff person trying to open app pages without an active Member, I want to be redirected to the correct gate page, so that the auth fork is enforced.
22. As an Org Admin who deletes my active Organization and has no remaining memberships, I want to be sent to the create page, so that I can recover from an empty account state.
23. As an Org Admin who deletes my active Organization and has one remaining membership, I want that membership auto-selected and to land on the dashboard, so that recovery matches login behavior.
24. As an Org Admin who deletes my active Organization and has two or more remaining memberships, I want to be sent to the organization picker, so that I choose the next workspace explicitly.
25. As a staff person, I want the user menu to show only logout (not a separate org switch dropdown), so that org controls are not duplicated.
26. As a developer, I want a shared auth-fork redirect helper used by login, middleware, and post-delete flows, so that routing rules stay consistent.
27. As a developer, I want an integration test proving a user can create a second Organization while already a Member of another, so that create-anytime behavior is guarded by CI.

## Implementation Decisions

### Unified org switcher (in-app)

- Make the Organization name in `StaffShell` clickable on desktop (sidebar header) and mobile (top header).
- Clicking opens a `Dialog` listing all memberships from the session.
- Each row shows Organization name (primary), slug and role (secondary, muted).
- Active membership is marked (e.g. check icon or "Current" label); inactive rows are selectable to switch.
- Footer action: **Create organization** navigates to the canonical create page.
- On successful switch: close dialog, redirect to `/`.
- Remove the legacy user-menu `<select>` org switcher; user menu retains logout only.

### Canonical create page

- Route: `/organizations/new`.
- Shared form component: Organization name (required), slug (required, pre-filled from name, editable).
- Uses existing create-organization BFF proxy and backend `POST /api/v1/staff/organizations`.
- On success: new Organization and Org Admin Member created; session active Member set to the new Member; redirect to `/`.
- Dual chrome:
  - **No active Member** (gate): `AuthCard` layout, logout in footer.
  - **Has active Member** (in-app): normal `StaffShell` with page title "Create organization"; cancel returns to `/` without creating.
- Retire `/onboarding/create-organization`; redirect that path to `/organizations/new`.

### Gate-only organization picker

- Keep `/select-organization` for authenticated users with memberships but no active Member (typically 2+ memberships at login).
- Reuse shared membership list component; gate variant uses Select buttons per row.
- Add **Create organization** action (navigates to `/organizations/new` in gate layout).
- On select: existing select-organization BFF proxy; redirect to `/`.

### Auth-fork routing

Shared helper encoding the same rules everywhere (middleware, login post-verify, organization delete redirect):

```
No active Member?
  0 memberships  → /organizations/new
  1 membership   → auto-select sole Member, then /
  2+ memberships → /select-organization

Has active Member?
  App pages           → allowed
  /organizations/new  → allowed
  /select-organization → redirect /
```

Login post-verify: single membership still auto-selects (existing behavior).
Organization delete redirect in Settings: apply the same fork using updated membership count after delete (replace hard-coded onboarding path).

### Staff route guard (Next.js middleware)

- Allow `/organizations/new` whether or not the session has an active Member.
- Allow `/select-organization` only when there is no active Member.
- Redirect `/onboarding/create-organization` to `/organizations/new`.
- When no active Member and user hits a normal app route, redirect using auth-fork helper.
- When active Member is set and user hits gate-only picker, redirect to `/`.

### Backend

- No new endpoints or schema changes.
- Existing APIs:
  - `POST /api/v1/staff/organizations` — create Organization + Org Admin Member + set active Member (works when user already has memberships).
  - `POST /api/v1/staff/session/organization` — set active Member (switch).
  - `GET /api/v1/staff/memberships` — list memberships for picker/switcher.

### Design doc updates

- Update Staff design doc: org switching moves from user menu to Organization header switcher dialog.

### Auth-fork helper (prototype shape)

```typescript
type SessionForkInput = {
  active_member: unknown | null;
  memberships: Array<{ member_id: string }>;
};

function resolveAuthForkPath(session: SessionForkInput): {
  path: string;
  autoSelectMemberId?: string;
} {
  if (session.active_member) return { path: "/" };
  if (session.memberships.length === 0) return { path: "/organizations/new" };
  if (session.memberships.length === 1) {
    return { path: "/", autoSelectMemberId: session.memberships[0].member_id };
  }
  return { path: "/select-organization" };
}
```

When `autoSelectMemberId` is set, caller must `POST` select-organization before navigating to `/`.

## Testing Decisions

### Seam

**Single primary seam: HTTP integration tests** in `backend/integration/`, extending the existing auth test suite.

This slice is predominantly Staff UI and Next.js middleware; the one backend behavior worth locking in CI is **create a second Organization while already a Member of another**. Switch and picker flows are largely covered by existing auth integration tests.

No Playwright E2E in this slice; defer Staff UI E2E until patterns stabilize (same approach as Organization Settings PRD).

### What makes a good test

- Request in, HTTP response out.
- Assert status, envelope shape (`data`, `error`, `request_id`), and `error.code` on failures.
- Use domain vocabulary in test names (Organization, Member, Org Admin, active Member).
- Prefer API helpers (`verifyOTP`, existing create-organization flow) over raw SQL.
- SQL seed only when no public API can set up the scenario (comment why).

### Scenarios to cover

- User with one existing membership creates a second Organization via `POST /api/v1/staff/organizations`; assert:
  - 200 response with session showing new Organization as active Member.
  - `GET /api/v1/staff/memberships` returns both memberships.
  - Previous membership still present (not removed).

### Prior art

- `backend/integration/auth_test.go` — OTP, org creation, membership selection, multi-membership picker, envelope assertions.
- `backend/integration/harness_test.go` — shared Postgres, truncate isolation, HTTP helpers.
- `docs/testing.md` and `docs/prd-integration-testing.md`.

### Out of scope for automated tests in this slice

- Playwright E2E of org switcher dialog, create page layouts, or middleware redirects.
- New backend endpoint coverage (no new endpoints).

## Out of Scope

- Invite links or emails when creating Organizations.
- Editing Organization slug after creation.
- Limits on how many Organizations one email may create.
- Org creation rate limiting beyond existing OTP/session limits.
- Leaving an Organization (Member self-removal); only create and switch.
- Popover or dropdown-menu switcher primitive (use existing `Dialog`).
- Backend changes to organization creation authorization (any authenticated session may create).

## Further Notes

- Completes roadmap item H10 (Switch organization polish) and extends auth onboarding beyond zero memberships.
- Related GitHub issue #3 (multi-org membership and switching); this spec adds create-anytime and unified in-app switcher UX.
- Organization Settings delete flow should adopt the shared auth-fork helper for post-delete redirects (`/organizations/new` instead of `/onboarding/create-organization`).
- `docs/design/staff.md` currently places org switching in the user menu; update to header switcher when implementing.
