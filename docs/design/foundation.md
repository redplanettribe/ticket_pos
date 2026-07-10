# Design Foundation

Shared visual language, components, accessibility, and feedback patterns for Staff and Storefront.

## Visual direction

**Neutral professional with a single accent.**

| Token area | Guidance |
|------------|----------|
| Neutrals | Cool slate/gray backgrounds, borders, and body text |
| Primary accent | Confident blue or indigo for actions, links, and focus rings |
| Semantic | Green success, red error, amber warning — always paired with text or icons |
| Radius | 6–8px on interactive elements and cards |
| Elevation | Subtle shadows on modals and elevated cards only; no glassmorphism or heavy gradients |
| Mode | Light only at launch |

Staff uses tighter line-height and slightly smaller body text.
Storefront uses more generous spacing and larger headings for event titles.

## Typography

- **Family:** One sans-serif via `next/font` — Inter or Geist.
- **Staff body:** 14px base, line-height ~1.4.
- **Storefront body:** 16px base, line-height ~1.5.
- **Headings:** Semantic `<h1>`–`<h4>` only; do not skip levels.

## Spacing

Use Tailwind's spacing scale consistently.
Prefer `4`, `6`, `8`, and `12` for component padding and section gaps.
Staff layouts may use tighter vertical rhythm than Storefront.

## Core components (`packages/ui`)

Build on shadcn/ui.
At minimum, provide:

| Component | Use |
|-----------|-----|
| Button | Primary, secondary, ghost, and destructive variants |
| Input, Label, Textarea | Forms |
| Alert | Banners and inline notices |
| Card | Grouped content |
| Dialog | Blocking confirmations |
| Toast | Transient success (via a toast provider in each app) |
| Skeleton | Page and list loading states |
| Badge | Status labels (e.g. sold out, draft) |

Apps compose layouts; `packages/ui` owns primitives and variants.

## Accessibility

Target **WCAG 2.1 AA**.

### Non-negotiables (every screen)

- Visible focus indicators on all interactive elements.
- Form labels associated with inputs (`htmlFor` / `id`, or `aria-label` when no visible label).
- Errors announced to assistive tech (`role="alert"` or `aria-live="polite"`).
- Color is never the only signal for state.
- Touch targets at least **44×44px** on POS and checkout flows.
- Semantic HTML: `<button>` for actions, `<a>` for navigation, proper heading hierarchy.

### AA targets

- Contrast 4.5:1 for body text; 3:1 for large text and UI components.
- Keyboard-operable Staff flows (login, catalog, POS).
- Skip link on the Staff shell once main navigation exists.

### Deferred

- WCAG AAA
- High-contrast theme toggle
- Extensive screen-reader-only copy beyond state changes

## Feedback patterns

Four tiers.
One rule: **display `error.message` as returned by the API.**

| Tier | When | Pattern |
|------|------|---------|
| **Inline** | Field validation (client or API `details.fields`) | Red text under the field; `aria-describedby` |
| **Banner** | Form-level or page-level API error | Alert at top of form; `role="alert"` |
| **Toast** | Transient success or background save | Auto-dismiss ~4s; `role="status"` |
| **Blocking** | Irrecoverable failure or required decision | Dialog with focus trap |

Show `request_id` only on persistent failures or in a support context — not on every toast.

### Loading

| Context | Pattern |
|---------|---------|
| Buttons | Label becomes progressive text ("Saving…"); `disabled` + `aria-busy="true"` |
| Pages and lists | Skeleton placeholders |
| POS payment | Full-screen blocking overlay; "Processing payment"; prevent double submission |

### Success

- **Staff:** Toast on save; stay on the current page for edit forms.
- **Storefront:** Inline confirmation during checkout; redirect only when the flow is complete.

### Empty states

Explain what happened and what to do next.
Example: "No events yet. Create your first event."
Never show a bare "No results."

## Locale and formatting

**English (US) only at launch.**

| Concern | Rule |
|---------|------|
| Language | Full words in UI copy ("Passcode", not "OTP") |
| Currency | USD; `$25` in catalog, `$25.00` on summed totals |
| Dates (Staff) | Relative when recent ("Today", "Tomorrow"); otherwise `Mon, Jul 12, 2026`; include time when relevant |
| Dates (Storefront) | `Saturday, July 12, 2026 · 7:00 PM` on event pages |
| Timezone | Event timezone when available; staff and customers see the same label |
| Capacity counts | Thousands separators (`1,250 remaining`); never round in a way that hides sold-out |

## Copy tone (shared rules)

- Use domain terms from [CONTEXT.md](../../CONTEXT.md): Event, Ticket Type, Organization.
- Prefer verbs over jargon: "Create event", "Get tickets".
- Error copy comes from the API; do not paraphrase unless mapping to a field.

Surface-specific tone lives in [staff.md](./staff.md) and [storefront.md](./storefront.md).
