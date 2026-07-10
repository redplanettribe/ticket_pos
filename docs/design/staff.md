# Staff UI

Operational UI for Org Admins, Event Owners, and Event Staff.
Covers catalog management, POS mode, sale imports, and team management.

Read [foundation.md](./foundation.md) first for tokens, components, accessibility, and feedback patterns.

## Personality

Dense, utilitarian, and keyboard-friendly.
Think Stripe Dashboard or Linear — not playful.
Staff users work under time pressure; optimize for scanability and few clicks.

## Auth pages (no shell)

Login, onboarding, and organization picker sit **outside** the main app shell.

| Property | Value |
|----------|-------|
| Layout | Centered card, max-width ~400px |
| Chrome | No sidebar; minimal header (product name only) |
| Forms | Single-column; clear primary action per step |

The login flow is a single-page two-step pattern: email, then passcode.

## App shell

Persistent **left sidebar** on authenticated pages.

### Sidebar (~240px)

| Area | Content |
|------|---------|
| Top | Active Organization name |
| Middle | Primary navigation |
| Bottom | User menu (switch organization, logout) |

Below the `md` breakpoint, collapse to an icon rail or hamburger drawer.

### Main content

- Page title (`<h1>`)
- Optional breadcrumbs on drill-down pages
- Content area with standard page padding

### Primary navigation (launch)

1. **Dashboard** — events overview and quick links
2. **Events** — list, create, edit; Ticket Types live inside an event
3. **POS** — enters full-screen POS mode
4. **Imports** — CSV sale import upload
5. **Settings** — organization profile, members, event assignments

Organization switching stays in the user menu, not as a top-level nav item.

## Information architecture

**Event is the catalog unit.**

```
Events list → Event detail → Ticket Types
```

Breadcrumbs example: `Events / Summer Fest / Ticket Types`

Do not show a flat org-level ticket list mixing types from different events.

## POS mode

Full-screen takeover optimized for **landscape tablet**.

| Property | Guidance |
|----------|----------|
| Chrome | Sidebar hidden; minimal header with event context |
| Exit | Clear "Exit POS" control (top corner) returns to the normal shell |
| Flow | Pick event → pick Ticket Types and quantities → confirm sale |
| Targets | Large touch targets (≥ 44×44px); high-contrast quantity steppers |
| Total | Sticky total bar at the bottom |
| Capacity | Live remaining capacity per Ticket Type |
| Payment | Blocking overlay during processing; no double-tap |
| Receipt | Sale confirmation with line summary after success |

Usable on phone in a pinch; tablet landscape is the primary target.

## Forms (catalog and admin)

- Single-column on narrow viewports; two columns only when fields are logically paired.
- Primary action right-aligned or full-width on mobile.
- Destructive actions (delete event, delete ticket type) require a blocking Dialog confirmation.
- On save success: toast + stay on page (do not redirect away from edit forms).

## Lists and tables

- Events list: name, date, status, quick actions.
- Ticket Types on an event: name, price, capacity, remaining.
- Prefer responsive tables that stack on small screens, or card lists where tables add little value.

## Copy tone

Direct and operational.

| Prefer | Avoid |
|--------|-------|
| Create event | Add a new event occurrence |
| Import sales | Upload external platform data |
| Exit POS | Leave point of sale |
| Passcode | OTP, verification code |

## Capacity and errors

- Show `remaining` capacity on Ticket Type rows and in POS.
- On `CAPACITY_EXCEEDED`: blocking feedback in POS; banner on catalog forms.
- On import failure (`IMPORT_BATCH_FAILED`): blocking Dialog with row detail from `error.details`.
