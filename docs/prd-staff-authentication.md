# PRD: Staff Authentication

Tracker: [M3T-165](https://linear.app/m3t/issue/M3T-165/staff-authentication-email-otp-sessions-org-onboarding) (parent: [M3T-162 Ticket POS](https://linear.app/m3t/issue/M3T-162/ticket-pos))

## Problem Statement

Event Staff and Org Admins need a secure way to sign in to the Staff app so they can manage Events, sell tickets in person, and import sales.
Today the Staff app has only a static login form and the Go API has no identity or session support.
Without authentication, no staff-facing workflows can be built.

Organizations also need a self-service path to get started.
A new person should be able to sign up, create their Organization, and begin using the platform without manual provisioning by platform operators.

## Solution

Implement email one-time passcode (OTP) authentication for Staff, backed by server-side sessions in PostgreSQL.
The Staff Next.js app acts as a BFF: the browser never talks to the Go API directly for auth; the BFF sets an httpOnly session cookie and forwards the session token to Go on subsequent requests.

After verifying their email, a person is routed based on how many **Member** records they hold:

- **Zero memberships** → create-organization onboarding (name + slug); creator becomes **Org Admin**
- **One membership** → auto-select that Organization and land on the dashboard
- **Multiple memberships** → org picker, then dashboard

A person may hold multiple **Member** records across **Organizations** (same email).
Authorization always flows through the active **Member** on the session, never email alone.

## User Stories

1. As a new venue operator, I want to sign in with my email address, so that I can access the Staff app without needing a password.
2. As a new venue operator, I want to receive a one-time passcode by email, so that I can prove I own the email address.
3. As a new venue operator, I want to enter the passcode on the login screen, so that I can complete sign-in.
4. As a new venue operator with no existing **Member** records, I want to name and slug my **Organization** after verifying my email, so that I can start using the platform immediately.
5. As a new venue operator, I want the organization slug pre-filled from the name, so that I can accept the default or customize it before creating the org.
6. As an org creator, I want to become **Org Admin** automatically when my **Organization** is created, so that I have full authority over my Events.
7. As a returning **Member**, I want to sign in with the same email OTP flow, so that I can access my existing **Organization**(s).
8. As a **Member** of exactly one **Organization**, I want to land on the dashboard immediately after OTP verification, so that I am not asked to pick an org unnecessarily.
9. As a **Member** of multiple **Organizations**, I want to choose which **Organization** I am working in after OTP verification, so that I act in the correct tenant context.
10. As a **Member** working in one **Organization**, I want to switch to another **Organization** I belong to, so that I can manage a different venue without signing out.
11. As a **Member** who was pre-provisioned by an **Org Admin** (via seed data or future admin tooling), I want to sign in and skip org creation, so that I can join an existing **Organization** I was added to.
12. As a signed-in **Member**, I want to see my current session state (email, active org if any), so that I know who I am acting as.
13. As a signed-in **Member**, I want to sign out explicitly, so that my session is destroyed on shared devices.
14. As a signed-in **Member** without an active **Member** selected, I want to be blocked from staff workflows (catalog, sales), so that I cannot act without an **Organization** context.
15. As a signed-in **Member** with an active **Member** selected, I want staff API calls to be scoped to my **Organization** and role, so that tenancy is enforced.
16. As a user on the login page, I want a single-page two-step flow (email, then code), so that sign-in feels simple and I do not navigate between routes.
17. As a user requesting a passcode, I want the same success message regardless of whether my email is new, so that the system does not leak account existence.
18. As a user who mistypes my passcode, I want a limited number of attempts before the code is invalidated, so that brute force is prevented but typos are tolerated.
19. As a user requesting passcodes too frequently, I want to be rate-limited, so that my inbox is not flooded and abuse is prevented.
20. As a developer running the stack locally, I want OTP codes logged to the terminal, so that I can test without a production email provider.
21. As a developer, I want to seed a pre-provisioned **Member** for an existing **Organization**, so that I can test the join-existing-org login path before member-management UI exists.
22. As a **Member** creating an **Organization**, I want slug uniqueness enforced globally, so that Storefront URLs `/{orgSlug}/events/...` do not collide.
23. As a **Member** creating an **Organization**, I want org creation to fail atomically if the slug is taken, so that I am not left with partial state.
24. As an API consumer (Staff BFF), I want every Go auth response to use the standard envelope (`data`, `error`, `request_id`), so that errors are handled consistently.
25. As a **Member** who stays active in the Staff app, I want my session to extend on use, so that I am not forced to re-authenticate during a busy event week.
26. As a **Member** who is idle for two weeks, I want my session to expire, so that abandoned sessions on shared devices eventually die.
27. As a user on step 2 of login who refreshes the page, I want to return to step 1, so that I can re-enter my email and request a new code.
28. As an unauthenticated visitor, I want protected Staff routes to redirect me to login, so that I cannot access the dashboard without a session.
29. As a user who verified OTP but has not yet created or selected an org, I want access only to onboarding, org picker, and logout routes, so that the auth fork is enforced in the UI.
30. As a platform operator, I want auth endpoints versioned under `/api/v1/`, so that future breaking changes can be introduced cleanly.

## Implementation Decisions

### Architecture

- **Staff app is a BFF.**
  The browser calls Staff Next.js Route Handlers only.
  Staff proxies to the Go API and manages the httpOnly session cookie on the Staff domain (`localhost:64301` in dev).
- **Go owns session truth.**
  Sessions are stored in PostgreSQL.
  The cookie holds an opaque session token only.
- **Open signup.**
  Any valid email may request an OTP.
  There is no members-only gate at login.
- **Join existing org via pre-provisioned Member.**
  A person joins an existing **Organization** only when a **Member** row already exists for their email.
  Member provisioning UI/API is deferred; use seed data for dev.

### Session model

```
sessions row:
  id               — opaque token (also stored in cookie)
  email            — cross-org identity at login
  active_member_id — nullable FK to members; null until org created/selected
  expires_at       — sliding 14-day window, extended on authenticated requests
```

| `active_member_id` | Meaning |
|--------------------|---------|
| null (no session) | Unauthenticated |
| null (session exists) | Authenticated but no **Organization** context; onboarding or picker only |
| set | Acting as a specific **Member**; staff routes authorized |

Authorization rule: `active_member_id → members → organization_id + role`.
Authenticated without active member returns **403 FORBIDDEN** on staff workflow routes (not 401).

### OTP parameters

| Parameter | Value |
|-----------|-------|
| Format | 6-digit numeric |
| Expiry | 10 minutes |
| Storage | Hash only in `otp_challenges`; never plaintext |
| Request rate limit | Max 3 per email per 15 min; max 10 per IP per 15 min |
| Verify attempts | Max 5 wrong codes per OTP, then invalidate |
| Delivery (dev) | `EmailSender` dummy implementation logs code to terminal |

### Post-verify routing (Staff UI)

| Membership count | Action |
|------------------|--------|
| 0 | Redirect to `/onboarding/create-organization` |
| 1 | Auto-set `active_member_id` → redirect to `/` |
| 2+ | Redirect to `/select-organization` |

### Create-org onboarding

- Fields: **Organization** name (required), slug (required, pre-filled from name, editable)
- Slug: globally unique, URL-safe
- On success (single transaction): create **Organization**, create **Member** with role `org_admin`, set `session.active_member_id`

### Member roles (schema)

Enum values at launch: `org_admin`, `event_owner`, `event_staff`.
Only `org_admin` is assigned by this feature (on org creation).

### Go API endpoints

| Method | Route | Auth | Purpose |
|--------|-------|------|---------|
| POST | `/api/v1/auth/otp/request` | None | Send OTP to email |
| POST | `/api/v1/auth/otp/verify` | None | Verify code; create session |
| GET | `/api/v1/auth/session` | Session token | Return session + memberships summary |
| POST | `/api/v1/auth/logout` | Session token | Destroy session |
| POST | `/api/v1/staff/organizations` | Session (email OK, member optional) | Create org + member |
| GET | `/api/v1/staff/memberships` | Session | List memberships for picker |
| POST | `/api/v1/staff/session/organization` | Session | Set `active_member_id` |

Session token forwarded from BFF via `Authorization: Bearer <session_id>` (or equivalent agreed header).

### Staff BFF endpoints (browser-facing)

| Method | Route | Purpose |
|--------|-------|---------|
| POST | `/api/auth/request-otp` | Proxy; no cookie |
| POST | `/api/auth/verify-otp` | Proxy; set httpOnly cookie |
| GET | `/api/auth/session` | Proxy; read cookie |
| POST | `/api/auth/logout` | Proxy; clear cookie |
| POST | `/api/auth/create-organization` | Proxy |
| GET | `/api/auth/memberships` | Proxy |
| POST | `/api/auth/select-organization` | Proxy |

Cookie attributes: `httpOnly`, `Secure` in production, `SameSite=Lax`, path `/`.

### Staff UI pages

| Route | Guard |
|-------|-------|
| `/login` | Public; two-step (email → code) on one page |
| `/onboarding/create-organization` | Authenticated, 0 memberships |
| `/select-organization` | Authenticated, 2+ memberships, no active member |
| `/` | Authenticated + `active_member_id` set |

### Schema (new tables)

```
organizations
  id, name, slug (unique), created_at

members
  id, organization_id (FK), email, role, created_at
  unique (organization_id, email)

sessions
  id, email, active_member_id (nullable FK), expires_at, created_at

otp_challenges
  id, email, code_hash, expires_at, attempts, created_at
```

### Modules to build/modify

- **identity** domain module (handler, service, repository): OTP, sessions, organizations, memberships
- **platform**: DB pool wiring in server startup, migration runner, `httputil` standard envelope + error mapping, session auth middleware, `EmailSender` interface + dummy impl, request ID middleware
- **Staff app**: BFF route handlers, login/onboarding/picker pages, auth middleware for protected routes
- **OpenAPI**: regenerate spec after handlers are added
- **CONTEXT.md** and **docs/technical-design.md**: update to reflect open signup and session/member model (glossary only in CONTEXT.md)

### Platform plumbing (prerequisite in same slice)

- Migration runner on API startup (dev) and `make migrate` target
- `DATABASE_URL` connection in `main.go`
- Standard response envelope on all new endpoints per api-errors skill

### Login UI state machine

```
step: "email" | "code"

email step → POST request-otp → code step

code step → POST verify-otp →
  memberships === 0 → /onboarding/create-organization
  memberships === 1 → / (auto-select)
  memberships > 1  → /select-organization
```

## Testing Decisions

### What makes a good test

Test **external behavior** at the highest stable seam: HTTP requests in, HTTP responses out (status, envelope shape, `error.code`, session side effects in DB).
Do not test internal function call order, private helpers, or SQL string contents.

### Primary test seam (one suite)

**Go API HTTP integration tests against real PostgreSQL** (testcontainers in CI, matching technical design).
This is the single primary seam because it exercises the full auth state machine, tenancy rules, OTP limits, and envelope contract without coupling to Next.js.

Cover these scenarios in one integration test package:

1. Request OTP → verify → create organization → session has `active_member_id` → `GET session` returns org
2. Pre-seeded member → verify OTP → auto-select single org → staff-scoped call succeeds
3. Pre-seeded two memberships → verify → select org → `active_member_id` updates
4. Authenticated, no active member → staff workflow route returns 403
5. Invalid/expired OTP, rate limits, verify attempt cap
6. Duplicate org slug → 409 with domain error
7. Logout → session destroyed → subsequent request 401

### BFF and UI

- **Staff BFF cookie wiring**: covered by a single Playwright E2E smoke test (happy path: login → create org → dashboard) when the e2e scaffold is wired to the stack.
  Deferred if Playwright + Compose is not ready in the same PR; manual verification acceptable with documented steps.
- **No unit tests** for thin BFF proxy handlers unless they contain non-trivial logic.

### Prior art

No existing tests in the repo.
Follow technical design agreement: repository/integration tests against real Postgres in CI.
Playwright scaffold exists at `e2e/tests/smoke.spec.ts` (placeholder only).

## Out of Scope

- Member provisioning UI/API (**Org Admin** adding **Members** by email)
- Invite links or invite codes to join existing **Organizations**
- Production email provider (Resend, SES, etc.)
- Role management UI (assigning **Event Owner** or **Event Staff**)
- **Organization** settings (rename, change slug after creation)
- POS-specific session hardening (short-lived POS mode, PIN lock on shared tablets)
- Passwords, OAuth, or SSO for staff
- **Integration Partner** authentication (API keys / OAuth2)
- Customer / Storefront authentication (guest checkout only)
- Catalog, sales, or capacity features

## Further Notes

### Codebase context

This is the first feature for Ticket POS.
The codebase is scaffolded: health endpoint only, stub domain packages, placeholder Staff/Storefront pages, no migrations, no tests.

### Domain glossary updates needed

- Signing in proves email ownership; acting in an **Organization** requires an active **Member** on the session.
- A person may hold multiple **Member** records across **Organizations** (same email).

### ADR candidate

Open signup (anyone can create an **Organization**) vs invite-only is a hard-to-reverse product decision.
Consider a short ADR when implementing.

### Dev ergonomics

Include a seed migration or script that creates a sample **Organization** with a pre-provisioned **Member** email for testing the join-existing-org path.

### Technical design drift

[technical-design.md](./technical-design.md) currently describes staff auth at a high level but does not document open signup or create-org onboarding.
Update that document as part of implementation.

## Related documents

- [CONTEXT.md](../CONTEXT.md) - canonical domain glossary
- [business-intent.md](./business-intent.md) - product goals and scope
- [technical-design.md](./technical-design.md) - architecture and engineering agreements
