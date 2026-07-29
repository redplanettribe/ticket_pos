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
One rule: **the API decides which failure occurred, and the UI renders that verdict without
re-reaching it.**

How that reads on each surface:

- **Staff** displays `error.message` as returned by the API, verbatim.
- **Storefront** displays its own copy for the failure, chosen by the API's `error.code` and
  falling back to `error.message` whenever the code is one its catalog does not know
  ([ADR 0023](../adr/0023-storefront-error-copy-keyed-on-api-error-code.md)). It serves two
  languages and the API answers in English only, so re-rendering the API's verdict in the
  visitor's language is the only way to keep the two agreeing about *which* failure it was.

Neither surface may infer a failure from a status code, a heuristic, or the absence of data.
Substituting local copy for a *specific* code is a deliberate, documented choice — the Payment
Provider return leg, the Confirmation Link, Google Sign-In — not something a surface does by
default.

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

**Two Locales on the Storefront: English and Ecuadorian Spanish.** Staff is English only.

A page's **Locale** is named in its own address (see [CONTEXT.md](../../CONTEXT.md) and
[storefront.md](./storefront.md)). The URL carries a short token because people read and retype it;
formatting needs a region, so each token maps to one Intl locale: `en` is `en-US`, `es` is `es-EC`.
Everything a reader sees formatted — month names, the marks inside numbers, the position of a
currency symbol — follows the Locale of the page they are on.

### A Locale is not a time zone

This is the distinction most easily lost. A Locale chooses **words and marks, and nothing else**.
Which wall clock a moment is drawn against is a domain rule and travels separately:

- **Event times** are drawn in the **Event's own timezone**, so a poster and a ticket agree.
- The **Reversal Window** deadline is drawn in **Ecuador time**
  ([ADR 0018](../adr/0018-customer-initiated-sale-reversal-within-the-reversal-window.md)), because
  a deadline set by an Ecuadorian wall-clock rule has to be read on an Ecuadorian clock — the hour
  binds a buyer reading Spanish and a buyer reading English identically.

Both hold in **both Locales**. The same instant shows the same hour on `/en` and on `/es`; only the
language it is written in changes. A Locale may never reach the time zone argument.

| Concern | Rule |
|---------|------|
| Language | Full words in UI copy ("Passcode", not "OTP"); copy is keyed by meaning, so the Spanish is free to word it differently |
| Currency | The Organization's, never the reader's; `$25` in catalog, `$25.00` on summed totals — under `es` that is `$25,50` |
| Dates (Staff) | Relative when recent ("Today", "Tomorrow"); otherwise `Mon, Jul 12, 2026`; include time when relevant |
| Dates (Storefront) | Event pages, same instant in both Locales: `Saturday, July 11, 2026 · 7:00 PM` under `en`, `sábado, 11 de julio de 2026 · 7:00 p. m.` under `es`. Cards use the compact form: `Sat Jul 11, 7:00 PM` / `sáb 11 jul, 7:00 p. m.` |
| Time zone | The Event's timezone for Event times, Ecuador for a Reversal Window deadline; identical in both Locales, and never derived from the reader |
| Capacity counts | Thousands separators (`1,250 remaining`); never round in a way that hides sold-out |

## Copy tone (shared rules)

- Use domain terms from [CONTEXT.md](../../CONTEXT.md): Event, Ticket Type, Organization.
- Prefer verbs over jargon: "Create event", "Get tickets".
- Which failure occurred comes from the API and is never re-decided in the UI. On Staff the
  words come from the API too; on the Storefront the words come from its own catalog, keyed on
  the API's code (see **Feedback patterns** above and
  [ADR 0023](../adr/0023-storefront-error-copy-keyed-on-api-error-code.md)).

Surface-specific tone lives in [staff.md](./staff.md) and [storefront.md](./storefront.md).
