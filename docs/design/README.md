# UI/UX Design

Ticket POS has two customer-facing surfaces — **Staff** and **Storefront** — plus a shared design foundation.
This folder is the canonical source for how those surfaces should look, behave, and feel.

Domain vocabulary lives in [CONTEXT.md](../../CONTEXT.md).
Technology choices for the frontend stack live in [technical-design.md](../technical-design.md).
API error shapes live in the [api-errors skill](../../.cursor/skills/api-errors/SKILL.md).

## Principles

1. **One foundation, two personalities.**
   Shared tokens, components, accessibility rules, and feedback patterns.
   Staff feels operational and dense; Storefront feels warm, event-led, and trustworthy.

2. **Event is the product.**
   Ticket Types belong to Events, not Organizations.
   Group tickets by event everywhere — especially on the Storefront.

3. **Show API messages faithfully.**
   Display `error.message` from the standard response envelope.
   Do not rewrite domain errors in the UI unless mapping `details` to a specific field.

4. **Accessible by default.**
   Target WCAG 2.1 AA.
   See [foundation.md](./foundation.md) for non-negotiables.

5. **Light mode only at launch.**
   Do not half-ship dark mode.

## Documents

| Document | Scope |
|----------|-------|
| [foundation.md](./foundation.md) | Tokens, typography, shared components, accessibility, feedback patterns, locale formatting |
| [staff.md](./staff.md) | Staff app shell, navigation, POS mode, operational tone |
| [storefront.md](./storefront.md) | Event-led layout, checkout, org-as-context branding, customer tone |

## Implementation stack

| Piece | Location | Notes |
|-------|----------|-------|
| Shared UI primitives | `packages/ui` | Button, Input, Alert, Card, Dialog, and similar |
| Component base | shadcn/ui | Accessible primitives; code is owned in-repo |
| Styling | Tailwind CSS | Tokens defined once; imported by both apps |
| Fonts | `next/font` | Inter or Geist in each app layout |

Both `apps/staff` and `apps/storefront` import from `packages/ui`.
Do not duplicate primitives per app.

## When to read which doc

| You are working on… | Read |
|---------------------|------|
| A shared component or token | `foundation.md` |
| Staff pages, POS, imports, team management | `foundation.md` then `staff.md` |
| Storefront event pages, cart, checkout | `foundation.md` then `storefront.md` |
| API error display in either app | `foundation.md` (feedback) + api-errors skill (envelope) |

## Deferred

The following are explicitly out of scope for launch UI:

- Dark mode and high-contrast themes
- Per-organization Storefront theming (custom logo, accent color)
- Custom domains and subdomains per Organization
- Multi-language and multi-currency
- Cross-event shopping carts
- White-label Storefront (removing platform attribution)

Document new UI decisions here before implementing.
If a decision is hard to reverse and surprising without context, add an ADR under `docs/adr/`.
