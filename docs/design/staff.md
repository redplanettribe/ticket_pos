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
| Top | Active Organization name (opens switcher dialog) |
| Middle | Primary navigation |
| Bottom | User menu (logout) |

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

Organization switching is in the Organization header dialog, not as a top-level nav item or user menu control.

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
- **Registration**: an Event chooses between selling Ticket Types and carrying a Registration Link.
  The choice is offered only while the Event is a `draft` and is disabled afterwards — it settles at publish.
  The Registration Link field appears only for the external choice, and Fee Handling disappears with it:
  there is no fee to apportion on an Event that sells nothing.
- The Registration Link's **click count** sits beside the link, on every Event page, for every Member —
  the same audience as the Sales list, since it stands in for the sales figures such an Event will never have.
  It is worded as clicks, never registrations or people: what happens on the other site is not visible here.

## Lists and tables

- Events list: name, date, status, quick actions.
- Ticket Types on an event: name, price, capacity, remaining.
- Prefer responsive tables that stack on small screens, or card lists where tables add little value.
- The Sales list and the Affiliate Links list are dense tables that **scroll sideways** on small screens
  (`overflow-x-auto`) rather than stack: their rows are comparisons, and a stacked row loses the column.
- **Affiliate Links** (#425): one row per link — name, code (monospace, never the full URL), Clicks, Sales,
  Net proceeds, status, created date (in the Event's zone), actions. Sorted by Clicks descending by default; column headers sort, a second click
  flips, each column opening in its natural direction (Name A→Z, everything else largest/newest first).
  A search box matches name and code. Sort and search run in the browser over the full list — the endpoint is
  unpaginated. Sales and Net proceeds are absent, not zero, on an Event that registers externally.
  Copy link stays a visible button; Rename / Deactivate / Delete sit behind a ⋯ menu. Create opens a dialog so
  the toolbar holds one text input.

## Trend readings

- A trend reading is drawn with the shared charts and the shared `TrendTile` (#820), never with a chart forked for one page.
- **Comparison span**: a thin, dashed, half-transparent line over the chart's own series, aligned bucket to bucket.
  Where the comparison has no data the line has a gap, never a zero.
- **Partial bucket** (today): on a line, a dashed stretch ending in a ring; on bars, a faint bar in a dashed outline.
  The hover card says it is partial before any figure.
- **Recording-began marker**: a dashed vertical mark with its label written toward the middle of the chart.
- **Selecting a bucket**: by a click anywhere in its column, or by keyboard through one tab stop on the plot (arrows, Home and End move; Enter and Space choose).
  The selected bucket is a solid mark, the keyboard's a dashed one, and the bucket the keyboard is on is shown as a chip at the plot's corner.
- **Not recorded** is not zero: a figure with no recorded value draws no axis and no grid, only the room the chart would take and the words for why.
- **Tile**: label, figure, change, small trend line, then lesser readings under a rule; a money tile stacks one reading per currency.
  The four states of a change are an arrow up with "+", an arrow down with "-", a dash, and the sentence "nothing to compare against" with no icon.
  The direction is that of the percentage printed, to one decimal: a change that prints as 0% is the dash, never an arrow.
  Colour repeats the icon and never carries the state alone.
- **Platform Trends** (#821) opens the Operator Dashboard's Overview, above the queues and totals: its controls, the tiles three to a row, then one chart per figure two to a row.
  The span and the comparison live in the page's address, and choosing either draws the new reading in place, with the one before dimmed until it arrives.
  A figure read as a trajectory is a line; Tickets Sold is bars stacked by Sales Channel.
  Money is one chart per currency, never two currencies on one axis.
  Each chart is drawn at its card's width, and scrolls sideways only in a card narrower than the plot's floor.
  The Overview reads as one page (#838): its section title (`text-lg`) is never smaller than the headings of the cards under it, which are `text-base`, and every grid of cards on it shares one gap (`gap-4`), so two-column grids line up edge to edge.
  Both live in `apps/staff/app/operator/overview-layout.ts`; `CardTitle`'s own default is left for pages with no section title above their cards.
- **Breakdown of a bucket** (#822): choosing a day or a week on a Platform Trends chart opens what it was made of in a bordered panel beneath that chart, inside its card, and the page is not left.
  One panel is open at a time: a bucket chosen on another chart moves it there, and its Close button lets the selection go and returns focus to the chart.
  The panel's title is one heading level beneath its chart card's title (an `h4` under the card's `h3`), and the headings of its lists one beneath that (an `h5`, the one exception to the foundation's `h1` to `h4`), so a reader moving by heading meets the breakdown as part of its chart.
  Ranked contributors are a numbered list, a plain list is not numbered, and money is one group per currency, each under its own total.
  Every row links to an Organization's operator page; an Event names its Organization beside it.
  A breakdown of today says it is partial, one of sales alone says so, and one not recorded says that in place of a zero.

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
