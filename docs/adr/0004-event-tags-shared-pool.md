# Event Tags live in one shared global pool, coined by Org Admins

## Status

accepted

## Context and decision

To improve event discoverability we introduced **Tags** (see CONTEXT.md): discovery
facets an Event can wear. An Event carries **many** Tags, drawn from a curated set of
**Preset Tags** plus **Custom Tags** that organizers coin when nothing fits. The key
structural decisions:

- **One shared, system-wide Tag pool** — not per-Organization. When Org A coins
  "Techno", that same pool row is reusable by Org B and can appear in the global
  explorer. This makes Tags the *second* deliberately cross-tenant concept in the
  system, after the global explorer itself (ADR 0002).
- **One pool, a `curated` flag** — Preset Tags are seeded rows with `curated = true`;
  Custom Tags are `curated = false`. Promoting a hot Custom Tag into the explorer's
  filter chips is a one-flag change, not a data migration, and event links already
  point at the right row.
- **Coining a Tag is Org-Admin-only** — assigning Tags and coining Custom Tags rides
  with all other event editing. This is deliberately *not* an any-Member action.
- **A dedicated set-replace endpoint** — `PUT /api/v1/staff/events/{id}/tags` is the
  sole writer of an Event's Tag set, separate from the scalar `PATCH /events/{id}`.

Names are normalized to a **canonical key** (trim, collapse internal whitespace,
lowercase) used for pool uniqueness, so "Techno" / "techno " / "  Techno" collapse to
one row; the first coiner's display casing is preserved. Custom Tag names are limited
to letters, digits, spaces, and hyphens, 1–30 characters. A staff typeahead
(`GET /staff/tags`) steers reuse before a row is coined.

## Why this way

- **Shared pool over org-private.** A global explorer that filters by Tag is only
  useful if a Tag means the same thing across tenants. Org-private tags could never
  power cross-tenant discovery. We accept the moderation surface this creates and
  contain it: only Preset Tags appear as explorer filter chips (ADR for the explorer
  filtering is separate); Custom Tags aid discovery via search and on org/event pages.
- **Org-Admin, not any-Member — the opposite call from `Discoverable`.** ADR 0003 made
  `Discoverable` an any-Member action because it is a single, low-stakes, reversible
  bit scoped to one event. Coining a Custom Tag is neither single-event-scoped nor
  cleanly reversible: it writes a permanent row into a namespace other Organizations
  draw from. That is a *higher* trust bar than ordinary editing, so it must not be
  *lighter* than it. Keeping tagging Org-Admin-only leaves `Discoverable` as the one
  deliberate exception rather than eroding the rule.
- **Dedicated endpoint, not a field on PATCH.** Unlike `Discoverable` (whose own
  endpoint exists because of a *different auth level*), the tag endpoint shares the
  Org-Admin gate — but it carries a side effect the scalar PATCH fields do not: it can
  coin new rows in the shared pool. Isolating that in an idempotent set-replace
  endpoint keeps event-field edits clean and maps directly to the UI's tag editor.

## Consequences

- A new cross-tenant table (`tags`) exists that, unlike almost everything else, is not
  scoped by `organization_id`; queries and future features must treat it as shared.
- Custom Tags accumulate globally with no deletion/moderation path yet — deferred.
- The staff tag editor surfaces existing Custom Tags for reuse in two ways: the
  typeahead searches the whole pool on input, and — added later — a focus-triggered
  "browse" list shows the most-used Custom Tags (by cross-tenant Event-association
  count, all statuses, no attribution), capped and excluding zero-usage rows. This is
  a deliberately *bounded, usage-ranked* surface: it steers reuse over near-duplicate
  coining without turning the unmoderated pool into permanent chrome. Preset Tags
  remain the only always-visible chip row and the only explorer filter chips.
- A future reader will see tagging is Org-Admin-gated while `Discoverable` (ADR 0003)
  is any-Member, on the same Event; this record explains why they diverge.
- Preset Tags are seeded by migration and must survive data resets that clear tenant
  data (the integration harness deletes only `curated = false` rows).
