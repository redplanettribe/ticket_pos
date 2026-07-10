---
name: ui-design
description: >-
  Ticket POS UI/UX and design system. Use when building or changing Staff UI,
  Storefront UI, shared components in packages/ui, Tailwind or shadcn setup,
  forms, layout, navigation, POS mode, checkout, accessibility, loading states,
  or visual feedback. Trigger on phrases like "UI", "UX", "design system",
  "component", "layout", "storefront", "POS", "checkout", or when editing
  files under apps/staff, apps/storefront, or packages/ui.
---

# UI/UX Design

Ticket POS has one shared design foundation and two surface personalities (Staff and Storefront).
Read the canonical docs before implementing or reviewing UI changes.

## Canonical docs

Start at [docs/design/README.md](../../../docs/design/README.md), then read the surface-specific doc:

| Area | Document |
|------|----------|
| Shared tokens, a11y, feedback, locale | [docs/design/foundation.md](../../../docs/design/foundation.md) |
| Staff shell, POS, catalog admin | [docs/design/staff.md](../../../docs/design/staff.md) |
| Storefront event pages, checkout | [docs/design/storefront.md](../../../docs/design/storefront.md) |

Related:

- Domain terms: [CONTEXT.md](../../../CONTEXT.md) — use Event, Ticket Type, Organization (not synonyms)
- Stack agreements: [docs/technical-design.md](../../../docs/technical-design.md) — Client applications
- API error envelope: [api-errors](../api-errors/SKILL.md) skill — UI shows `error.message`; skill owns envelope shape

## Implementation (`packages/ui`)

Shared package: `@ticket-pos/ui`.
Both apps depend on it via `workspace:*`.

### App wiring (already in place — preserve when adding apps)

**`package.json`:** `"@ticket-pos/ui": "workspace:*"` plus `tailwindcss` and `@tailwindcss/postcss`.

**`postcss.config.mjs`:** `@tailwindcss/postcss` plugin.

**`app/globals.css`:**

```css
@import "@ticket-pos/ui/globals.css";
@source "../../../packages/ui/src";
@source "../";
```

**`next.config.ts`:** `transpilePackages: ["@ticket-pos/ui"]`.

**Root layout:** import `./globals.css`, load Inter via `next/font`, apply surface class on `<body>`:

- Staff: `surface-staff` (14px base, tighter rhythm)
- Storefront: `surface-storefront` (16px base, more generous spacing)

**Toasts:** render `<Toaster />` from `@ticket-pos/ui` once in each app root layout.

### Imports

```tsx
import { Button, Card, Input, AuthCard, StaffShell, cn } from "@ticket-pos/ui";
```

New primitives go in `packages/ui/src/components/ui/`, exported from `packages/ui/src/index.ts`.
`components.json` in `packages/ui/` is configured for future `shadcn` CLI use.

Do **not** duplicate primitives inside `apps/staff` or `apps/storefront`.

### Available exports

| Category | Components |
|----------|------------|
| **Primitives** | `Button`, `Input`, `Label`, `Textarea`, `Alert`, `AlertTitle`, `AlertDescription`, `Card` (+ header/footer/title/description/content), `Dialog` (+ trigger/content/header/footer/title/description), `Badge`, `Skeleton`, `Toaster` |
| **Layouts** | `AuthCard` (centered auth card, max-width ~400px), `StaffShell` (sidebar app chrome), `StorefrontShell` (header + footer) |
| **Forms** | `FormField` (label + control + inline error; passes `aria-describedby` to the control) |
| **Utils** | `cn` |

### Layout usage

| Surface | Shell | When |
|---------|-------|------|
| Staff auth | `AuthCard` | Login, onboarding, org picker — no sidebar |
| Staff app | `StaffShell` | Authenticated pages — sidebar, skip link, org name |
| Storefront | `StorefrontShell` | All public pages — org name in header, "Powered by Ticket POS" footer |

## Design principles

1. **One foundation, two personalities** — shared tokens/components; Staff is dense/operational; Storefront is warm/event-led.
2. **Event is the product** — Ticket Types belong to Events; group tickets by event, never as a flat org catalog.
3. **Show API messages faithfully** — display `error.message` as returned; map `details` to fields only when structured.
4. **Accessible by default** — WCAG 2.1 AA (see below).
5. **Light mode only** at launch — do not half-ship dark mode.

## Visual direction

- **Neutrals:** cool slate/gray backgrounds, borders, body text
- **Primary accent:** indigo for actions, links, focus rings
- **Semantic:** green success, red error, amber warning — always pair with text or icons
- **Shape:** 6–8px radius; subtle shadows on elevated surfaces only
- **Typography:** Inter via `next/font`; semantic `<h1>`–`<h4>` without skipping levels

## Staff UI

- **Auth pages:** `AuthCard`, single-column forms, one primary action per step.
- **Shell:** left sidebar (~240px) with org name, primary nav, user menu (switch org, logout).
- **Primary nav:** Dashboard → Events → POS → Imports → Team.
- **Catalog IA:** Events list → Event detail → Ticket Types (breadcrumbs on drill-down).
- **POS mode:** full-screen takeover, sidebar hidden, tablet-first, large touch targets, sticky total bar, clear "Exit POS".
- **Tone:** direct and operational ("Create event", "Exit POS", "Passcode" not "OTP").

## Storefront UI

- **Event-first:** event page is the product page; org is trust context ("Presented by …"), not the shopping unit.
- **Hero-led layout:** event title as `<h1>`, optional hero image, ticket type cards below.
- **Checkout:** guest only at launch; **one event per cart** — no cross-event carts.
- **Branding:** minimal platform chrome; small "Powered by Ticket POS" footer; no per-org theming at launch.
- **Sold out:** show sold-out ticket types as unavailable, not hidden.
- **Tone:** warm and clear ("Get tickets", "Sold out").

## Feedback (four tiers)

| Tier | When | Pattern |
|------|------|---------|
| **Inline** | Field validation | Red text under field; `FormField` or `aria-describedby` on control |
| **Banner** | Form/page API error | `Alert variant="destructive"` at top of form |
| **Toast** | Transient success | `Toaster` / sonner; `role="status"` |
| **Blocking** | Sold out, payment, import failure | `Dialog` with focus trap |

Rules:

- Display `error.message` from the API envelope as-is.
- Do **not** use `role="alert"` for success messages.
- Show `request_id` only on persistent failures or support context — not every toast.
- Buttons: `disabled` + `aria-busy={loading}` + progressive label ("Saving…").
- Pages/lists: `Skeleton` placeholders.
- POS/checkout payment: full-screen blocking overlay.

## Accessibility (non-negotiables)

- Visible focus indicators on all interactive elements.
- Labels associated with inputs — use `FormField` or `Label` + `htmlFor`.
- Errors announced (`role="alert"` or `aria-live="polite"`).
- Color is never the only state signal.
- Touch targets ≥ 44×44px on POS and checkout flows.
- Semantic HTML: `<button>` for actions, `<a>` for navigation.
- Staff shell includes a skip link to `#main-content`.

## Locale (launch)

English (US) only.
USD: `$25` in catalog, `$25.00` on summed totals.
Staff dates: relative when recent, otherwise `Mon, Jul 12, 2026`.
Storefront dates: `Saturday, July 12, 2026 · 7:00 PM`.
Capacity counts use thousands separators.

## Deferred (do not implement without an explicit decision)

- Dark mode / high-contrast themes
- Per-org Storefront theming (logo, accent color)
- Custom domains per Organization
- Multi-language / multi-currency
- Cross-event shopping carts
- White-label Storefront (removing "Powered by" footer)

## Checklist for UI changes

- [ ] Read `foundation.md`; read `staff.md` or `storefront.md` for the app you touch
- [ ] Use or extend `@ticket-pos/ui` — do not duplicate primitives per app
- [ ] Correct shell: `AuthCard` vs `StaffShell` vs `StorefrontShell`
- [ ] Event-first ticket grouping (no flat org-wide ticket catalog)
- [ ] API errors use four-tier feedback; show `error.message` (see api-errors skill)
- [ ] Success uses toast/status — not `role="alert"`
- [ ] Forms use `FormField` for labeled inputs with accessible error wiring
- [ ] Loading: skeletons for pages, `aria-busy` on buttons, blocking overlay for payment
- [ ] Empty states explain what to do next
- [ ] Touch targets ≥ 44px on POS and checkout
- [ ] Copy uses CONTEXT.md terms

## When updating the design

If a grilling session changes UI conventions, update `docs/design/` first, then align this skill.
Backend-only or OpenAPI-only work does not require this skill — use api-errors or update-swagger instead.
