# Storefront UI

Customer-facing UI for discovering events, completing Online Sales, and — once signed in — reviewing what was bought.
Path-based tenancy, under a Locale: `/{locale}/{orgSlug}/events/{eventSlug}`.

Read [foundation.md](./foundation.md) first for tokens, components, accessibility, and feedback patterns.

## Addresses

Every page lives under a **Locale**, named in the address itself: `/{locale}`,
`/{locale}/{orgSlug}`, `/{locale}/{orgSlug}/events/{eventSlug}`, `/{locale}/signin`,
`/{locale}/tickets`, `/{locale}/checkout/success`. The token is `en` or `es`, and it is always
there — there is no unmarked language, so no page address is ambiguous about which one it serves.

**A prefixed URL's content never depends on the cookie or on Accept-Language.** `/en/…` is English
for every visitor and every crawler, `/es/…` is Spanish for every visitor and every crawler, and the
link one person sends another renders the same page at both ends. The reader's cookie and browser
languages decide exactly one thing: which Locale an address naming *none* is sent to. That redirect
is **temporary and never permanent** — the destination is computed per request, and a permanent one
would let one visitor's browser cache a language for the next person on the shared machine.

Four kinds of address stay deliberately **unprefixed**, because each would break if it gained a
Locale:

| Unprefixed | Why |
|------------|-----|
| `/api/…` | The Storefront's own BFF, called by this app's scripts at fixed paths |
| `/checkout/return` | The **Payment Provider** builds this URL from a constant handed to it at Payment time; some providers POST to it, and the return leg is the last place to spend a redirect |
| `/tickets/confirm` | A **Confirmation Link** sits in an email for the life of an Event — it cannot acquire a language it did not have on the day it was sent |
| `/sitemap.xml`, `/robots.txt`, `/manifest.webmanifest` | A crawler asks for these at the origin root; each describes the whole site, in both Locales, rather than one language of it |

Each of those still hands the visitor on to a page that *does* have a Locale, so each has to choose
one, by the same chain. The **Payment Provider** return leg does better than choosing: the language
the buyer set off in is recorded when checkout begins, so a Spanish buyer comes back to a Spanish
confirmation rather than to whatever their browser happens to say.

Internal links are written without a prefix and pick up the Locale the reader is already in, so a
Customer who arrived in Spanish stays in Spanish across every route below.

## Personality

Warm, clear, and trustworthy.
Event imagery and title lead; UI chrome stays minimal.
Checkout should feel safe and simple — guest checkout, no account friction.

## Event-first commerce

**The event is the center of the Storefront.**

Ticket Types belong to Events.
**Selling** always happens per event — one event per cart, ticket selection on the event page. Tickets are never sold as a flat org-wide or cross-org cart.

Discovery, however, is layered. Customers reach an event three ways, and all three funnel into the same event page where buying happens.

| Concept | Role |
|---------|------|
| **Event** | Product page; ticket selection happens here |
| **Organization** | Trust and attribution context, not the shopping unit |

### Discovery surfaces

| Surface | Route | Answers | Lists |
|---------|-------|---------|-------|
| **Event page** | `/{locale}/{orgSlug}/events/{eventSlug}` | "What tickets does *this event* have?" | one event |
| **Organization page** | `/{locale}/{orgSlug}` | "What is *this org* putting on?" | one org's Discoverable events (upcoming + past) |
| **Global explorer** | `/{locale}` | "What's coming up *anywhere*?" | Discoverable events across all orgs, soonest first |

A published event is **reachable by direct link** regardless of discovery. Whether it also *advertises itself* in the org page and global explorer is controlled by the per-event **Discoverable** flag (see [CONTEXT.md](../../CONTEXT.md)). `draft` and `cancelled` events are never reachable or listed.

Past events (end, or start if no end, in the past) drop off the global explorer and appear under a "Past events" section on the org page; their event page stays reachable with an "ended" state.

### Primary flow

Marketing link → **event page** → select Ticket Types → checkout → confirmation.

### Secondary flows

- Global explorer (`/{locale}`) → search / date filter → pick an event → event page.
- Org page (`/{locale}/{orgSlug}`) → pick an event → event page.

Most traffic arrives on a specific event; the explorer and org page are for discovery.

### Global explorer

- Sorted soonest-first; only Discoverable, published, not-yet-ended events.
- One search box over event and organization name.
- Date presets over event start: All upcoming / This weekend / This week / This month.
- Cursor-based "Load more" pagination.
- Location/city filtering is **deferred** (no structured location data yet).

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
- Surfaces with no Organization context — the global explorer, sign-in, the **Customer Area** — show the platform mark alone, linked home.
- Sign-in state sits at the trailing edge; see [Customer sign-in](#customer-sign-in).

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
Buying never requires signing in — see [Customer sign-in](#customer-sign-in) for the surfaces that do.
A **Customer** record is created or reused by the **Ticket Sale** itself, and the **Sale Confirmation** carries a **Confirmation Link** back to it.

| Step | UI |
|------|-----|
| Cart review | Line items, quantities, per-line and total price |
| Payment | Provider UI or stub; event name stays visible |
| Processing | Blocking overlay; no duplicate submission |
| Success | Confirmation with event name, ticket summary, and order reference |
| Failure | Banner or blocking Dialog with the copy for the API's `error.code`; sold-out mid-checkout uses blocking tier |

On `CAPACITY_EXCEEDED` during checkout: blocking Dialog; offer to return to ticket selection.

### The undo offer on the success page

Where the **Reversal Window** is open, the success page states it: "You can undo this purchase until {deadline} Ecuador time", with **"Sign in to undo"** beside it. The word is *undo* — "cancel" is an Event being called off, and "refund" is wrong for a free Online Sale.

Checkout is guest-facing and reversal needs a **Customer Session**, so the buyer most likely to change their mind is the one least equipped to act. The offer turns that requirement into one click: sign-in arrives prefilled with the address the purchase was made under, bound for **this purchase** in the Customer Area, which is where the undo actually happens. A visitor who already holds a full session skips the prompt — "Undo it in your tickets" goes straight to that sale.

The destination is the sale and not the list. The Customer Area has no per-sale route, so each **Ticket Sale** card is addressable by a fragment anchor and the links aim at it; a buyer with a season's worth of tickets is not handed a page to scan at the moment they are anxious about their money. When the sale is not known — a lost checkout context — the destination is the Customer Area itself, exactly as before.

Nothing on this page reverses anything. It states a deadline and offers a door.

The API answers whether there is an offer at all, keyed on the checkout this browser began. When it says no — the window closed, the sale was never eligible, the **Payment Provider** cannot reverse it — the page says nothing about undoing: no greyed-out button, no expired countdown. Same when the checkout context has been lost (another browser, a cleared jar): the confirmation is still a confirmation.

## Customer sign-in

**Anonymous browsing is unchanged and requires no sign-in.**
The global explorer, Organization pages, and event pages render for an anonymous visitor exactly as they did before Customer identity existed, and stay as fast.
Do not put a sign-in wall in front of discovery or checkout.

A **Customer** never registers. The record is created by the **Ticket Sale** (see [CONTEXT.md](../../CONTEXT.md)), so signing in only ever means proving ownership of an email — ask for a passcode, never for a password and never for a signup.

### Sign-in page (`/{locale}/signin`)

Two steps on one page, matching the Staff sign-in: email, then the 6-digit passcode, the form swapping in place rather than navigating.

| Step | UI |
|------|-----|
| Email | Email field, "Send passcode" |
| Passcode | Numeric passcode field (`one-time-code` autofill), "Sign in", plus "Send a new passcode" and "Use a different email" |

- Passcode failures show this Storefront's copy for the API's `error.code`, per [foundation.md](./foundation.md) and [ADR 0023](../adr/0023-storefront-error-copy-keyed-on-api-error-code.md) — the page never decides for itself which failure it was, and a code with no copy falls back to the API's `error.message`.
- "Send a new passcode" stays on screen for every recoverable failure (mistyped, expired, attempts exhausted). It is withheld only when asking again is the thing being refused: rate limiting and the global send ceiling.
- Alerts above the form explain two arrivals: a **Customer Session** that ran out, and a **Confirmation Link** that was expired or invalid.
- No sign-in state in the header here.
- A visitor already holding a full **Customer Session** is redirected on. One holding a **Confirmation Link** session is not — widening is what they came for.

### Customer Area (`/{locale}/tickets`)

Everything the Customer has bought, from every **Organization**, in one place, plus the one thing
they can change about themselves.

- **Upcoming** first, then **Past**; each a list of **Ticket Sale** cards.
- Each card carries the event name (linked to its event page), date and venue, "Presented by {Organization}" (linked to the Organization page), the **Ticket Sale Lines** as quantity × Ticket Type with line totals, the **Sale Confirmation** reference in a monospaced face, and the ticket count with the sale total. A reversed sale carries a **Reversed** badge.
- A card the API reports as reversible ends in the **Sale Reversal** offer: the deadline in Ecuador time, and **"Undo this purchase"**. The word is *undo* — "cancel" is an Event being called off, and "refund" is not this system's word for a **Sale Reversal**. The button opens a confirmation dialog naming the Event and the Sale Confirmation reference, with a destructive confirm and "Keep my tickets"; the action cannot be taken back and the dialog says so. A purchase that cost something adds one sentence there — the amount will be returned — and no claim about how or when it arrives; a free claim says nothing about money at all. Nothing about undoing appears on a card the API does not offer — no disabled button and no expired deadline. Every refusal shows the copy for the API's `error.code`: the **Reversal Window** is enforced server-side, so a card rendered before the deadline and pressed after it is refused rather than honoured, and which refusal it is stays the API's to say.
- `noindex, nofollow`: the Customer Area is private and must stay out of search results.
- A missing or expired session redirects to the sign-in page rather than showing an error. When a session existed, the redirect says so, so sign-in can explain what happened instead of looking like a random demand.
- A read that fails for any other reason keeps the page and shows the copy for the API's `error.code` in a banner-tier Alert.

#### My info

Below the ticket lists, a panel showing what the platform holds about the Customer: their **email**,
their name, and their **Tax ID** rendered as its human label with the number ("Cédula: 1712345678"),
or "—" when they have none. An **Edit** button opens an inline form over the same panel.

- The email is displayed and never editable — it is the Customer's identity, and the form says so
  rather than offering a disabled input with no explanation.
- First and last name are required; the form refuses a blank one before the API does.
- The Tax ID type and number sit on one row, as at checkout. **Emptying the number clears the stored
  Tax ID**, which the field's description states outright; nothing prefills at the next checkout
  until one is supplied again.
- Field errors render under their inputs — the copy for each `FieldError`'s own `code`, falling
  back to its message — and the form-level failure in a destructive Alert, exactly as the
  checkout dialog does.
- Absent for a **Confirmation Link** arrival. That session shows one purchase and may edit nothing;
  the API refuses the write behind it.

#### Empty states

| Situation | Treatment |
|-----------|-----------|
| No Ticket Sales at all | "No tickets yet", one line of explanation, primary "Discover events" → `/{locale}` |
| Past sales but nothing upcoming | "Nothing coming up" in a dashed panel above the Past list, secondary "Discover events" |

Neither dead-ends. The first is reachable normally: completing a passcode creates the record if no Ticket Sale ever did, so an empty Area is a plausible first visit, not a fault.

### Arriving by a Confirmation Link

A **Confirmation Link** from a **Sale Confirmation** email has no UI of its own — it redeems and lands on the same Customer Area, narrowed to the one **Ticket Sale** it covers.
Its address is one of the unprefixed ones, so redemption has to choose the Locale it lands in: cookie, then browser languages, then English. A link opened days later from an inbox, often on another device, has nothing better to go on.

| | Full Customer Session | Confirmation Link arrival |
|---|---|---|
| Heading | "Your tickets" | "Your purchase" |
| Shows | Every Ticket Sale the Customer owns | Exactly the one linked sale |
| Notice | None | "You're viewing one purchase", with "Sign in with a passcode" as the way to widen |
| Reversed sale | **Reversed** badge on the card | Badge, plus a destructive Alert at the top of the page (banner tier) |
| Undo offer | The deadline and **"Undo this purchase"** | The same deadline and **"Sign in to undo"** — never the button |

A reversed sale gets that extra Alert because someone opening a confirmation email at the gate must not have to infer cancellation from a small label.

The link arrival is **read-only**, and the undo row is where that matters most. A **Confirmation Link** travels by email and gets forwarded, so it must never carry the one destructive, money-moving action a Customer has — which is exactly why reversal sits behind a **Customer Session**. The page therefore reveals the deadline and the route to sign in, prefilled with the address the purchase was made under and bound for this sale's own card, and offers no action a forwarded email could trigger. The deadline shown is the same instant the Customer Area shows for the same sale.

A link that is expired or invalid sends the visitor to the sign-in page with copy for whichever it was — an expired link means this person really did buy a ticket.

### Sign-in state in the header

| State | Header trailing edge |
|-------|----------------------|
| Anonymous | "Sign in" (ghost) |
| Signed in | The signed-in email (hidden below `sm`, full address as a `title`), "Your tickets", "Sign out" |

Present on the global explorer, Organization pages, event pages, and the Customer Area; absent on the sign-in page.
Showing the email is what keeps the signed-in identity unambiguous on a shared device.
An anonymous visitor pays nothing for it: with no session cookie there is no API call.

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
| Sign in | Log in, Create an account |

## Responsive behavior

- Mobile-first for event page and checkout.
- Quantity steppers and primary CTA remain thumb-reachable.
- Sticky cart/total bar on scroll for ticket selection.
