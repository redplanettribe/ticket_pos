# Storefront discovery surfaces and per-event Discoverable flag

## Status

accepted

## Context and decision

The original Storefront design (`docs/design/storefront.md`) was deliberately event-only: "group and sell tickets per event, never as a flat org-wide catalog", with an optional org index and the assumption that most traffic arrives on a specific event via a marketing link. We have decided to expand discovery to **three surfaces** — the event page (`/{orgSlug}/events/{eventSlug}`), the organization page (`/{orgSlug}`), and a **global explorer** (`/`) that lists published events across *all* organizations, soonest first, with search and date filtering.

Because a global explorer is a flat cross-tenant catalog — exactly what the earlier doc warned against — we split the meaning of "published" into two independent axes:

- **Reachability** (`Event status`): a `published` event is loadable at its URL by direct link.
- **Discoverability** (new per-event `Discoverable` flag): whether a `published` event *advertises itself* in the org page and global explorer, or stays direct-link-only.

Selling is unchanged: still one event per cart, ticket selection on the event page.

## Why this way

- **Discoverability is per event, not per org.** An organizer routinely wants some events openly listed and others private (a link-only pre-sale, a company party). A single org-wide "listed" switch could not express that, so the flag lives on the Event.
- **One flag, two levels (not two flags).** `discoverable = false` hides an event from *both* the org page and the global explorer; `true` lists it in both. We rejected independent "list on org page" vs "list globally" switches as more UI and model surface than the need justifies at launch.
- **Auto-list rejected.** Making every `published` event globally visible would remove the organizer's ability to sell privately, and cross-tenant exposure should be opt-in.

## Consequences

- New public read endpoints under `/api/v1/public/...` for the global explorer, org events, and event detail; the global query is cross-tenant (no `organization_id` scoping) by design, unlike every other query in the system.
- Past events (end, or start if no end, in the past) drop off the global explorer, remain on the org page under "Past events", and keep a reachable event page in an "ended" state.
- Structured location/city filtering is deferred — the Event has only free-text venue fields, so the explorer filters on date and name only for now.

This is hard to reverse (public URLs, a schema column, and a cross-tenant query others will build on) and surprising without this record (it contradicts the prior design doc), which is why it is written down.
