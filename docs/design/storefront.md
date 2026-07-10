# Storefront UI

Customer-facing UI for Online Sales.
Path-based tenancy: `/{orgSlug}/events/{eventSlug}`.

Read [foundation.md](./foundation.md) first for tokens, components, accessibility, and feedback patterns.

## Personality

Warm, clear, and trustworthy.
Event imagery and title lead; UI chrome stays minimal.
Checkout should feel safe and simple — guest checkout, no account friction.

## Event-first commerce

**The event is the center of the Storefront.**

Ticket Types belong to Events.
Group and sell tickets per event, never as a flat org-wide catalog.

| Concept | Role |
|---------|------|
| **Event** | Product page; ticket selection happens here |
| **Organization** | Trust and attribution context, not the shopping unit |

### Primary flow

Marketing link → **event page** → select Ticket Types → checkout → confirmation.

### Secondary flow

Org events index (optional) → pick an event → same event page.

Most traffic arrives on a specific event.
The org index is for discovery when an Organization runs many events.

### Checkout scope

**One event per cart at launch.**
No cross-event carts.
Document multi-event checkout as deferred if requested later.

## Page layout

### Event page (hero-led)

```
┌─────────────────────────────────────┐
│  [optional event image / hero]      │
│  Event title (h1)                     │
│  Date · time · venue (if available) │
│  Presented by {Organization}        │
├─────────────────────────────────────┤
│  Ticket Type cards                  │
│  - name, description, price         │
│  - remaining / sold out             │
│  - quantity selector                │
├─────────────────────────────────────┤
│  Sticky "Get tickets" / cart bar    │
└─────────────────────────────────────┘
│  Powered by Ticket POS (footer)     │
└─────────────────────────────────────┘
```

- **H1:** Event name.
- **Org name:** Secondary line ("Presented by Acme Promotions") — trust, not the headline.
- **Hero:** Event image when available; clean typographic fallback when not.
- **Ticket list:** All Ticket Types for this event on one page; no tabs per type.

### Header

- Organization name as light chrome.
- No large Ticket POS logo in the header.

### Footer

- Small "Powered by Ticket POS" text link.
- Legible but unobtrusive.

## Branding (launch)

| In scope | Out of scope |
|----------|--------------|
| Platform design tokens | Per-org accent colors |
| Org name as text attribution | Custom logo upload |
| Event-led imagery | Custom domains |
| "Powered by" footer | White-label (no attribution) |

## Ticket Type presentation

Each Ticket Type is a **card** with:

- Name and description
- Price (formatted per [foundation.md](./foundation.md))
- Remaining capacity or **Sold out** badge
- Quantity stepper (hidden or disabled when sold out)

Sold-out types remain visible but clearly unavailable — do not hide them without reason.

## Checkout

Guest checkout only at launch.
No customer account or login.

| Step | UI |
|------|-----|
| Cart review | Line items, quantities, per-line and total price |
| Payment | Provider UI or stub; event name stays visible |
| Processing | Blocking overlay; no duplicate submission |
| Success | Confirmation with event name, ticket summary, and order reference |
| Failure | Banner or blocking Dialog with `error.message`; sold-out mid-checkout uses blocking tier |

On `CAPACITY_EXCEEDED` during checkout: blocking Dialog; offer to return to ticket selection.

## SEO and metadata

Event pages are SSR/ISR.
Page `<title>`: `{Event name} · {Organization name}`.
Open Graph and canonical URLs per [technical-design.md](../technical-design.md).

Visual design must not compromise semantic HTML and metadata.

## Copy tone

Warm and clear.

| Prefer | Avoid |
|--------|-------|
| Get tickets | Purchase tickets |
| Continue to payment | Proceed to checkout module |
| Sold out | Unavailable |
| Your tickets | Your order items |

## Responsive behavior

- Mobile-first for event page and checkout.
- Quantity steppers and primary CTA remain thumb-reachable.
- Sticky cart/total bar on scroll for ticket selection.
