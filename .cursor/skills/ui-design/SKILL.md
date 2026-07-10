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

## Quick reference

### Architecture

- **One foundation, two personalities** — shared `packages/ui`; Staff is dense/operational; Storefront is warm/event-led.
- **Stack** — Tailwind CSS, shadcn/ui primitives in `packages/ui`, `next/font` in each app.
- **Light mode only** at launch.

### Domain-driven layout

- **Ticket Types belong to Events** — group tickets by event, not by organization.
- **Storefront checkout** — one event per cart at launch.
- **Storefront branding** — event-led hero; org as "Presented by …"; "Powered by Ticket POS" in footer.

### Staff shell

- Sidebar navigation on authenticated pages.
- Auth pages (login, onboarding, org picker): centered card, no sidebar.
- POS: full-screen takeover, tablet-first, clear "Exit POS".

### Feedback (four tiers)

1. **Inline** — field errors
2. **Banner** — form/page API errors (`role="alert"`)
3. **Toast** — transient success
4. **Blocking** — dialogs for sold-out, payment, import failure

Display `error.message` from the API envelope as-is.
For envelope shape and error codes, see the [api-errors](../api-errors/SKILL.md) skill.

### Accessibility

WCAG 2.1 AA target.
44×44px touch targets on POS and checkout.
Visible focus, labeled inputs, no color-only state.

### Locale

English (US) only at launch.
USD currency; formatting rules in foundation.md.

## Checklist for UI changes

- [ ] Read foundation.md; read staff.md or storefront.md for the app you touch
- [ ] Use or extend `packages/ui` — do not duplicate primitives per app
- [ ] Event-first ticket grouping (no flat org-wide ticket catalog)
- [ ] API errors use the four-tier feedback model; show `error.message`
- [ ] Loading states: skeletons for pages, `aria-busy` on buttons, blocking overlay for POS/checkout payment
- [ ] Empty states include a next action
- [ ] Touch targets ≥ 44px on POS and checkout
- [ ] Copy uses CONTEXT.md terms (Event, Ticket Type, Organization)

## When updating the design

If a grilling session changes UI conventions, update `docs/design/` first, then align this skill.
Backend-only or OpenAPI-only work does not require this skill — use api-errors or update-swagger instead.
