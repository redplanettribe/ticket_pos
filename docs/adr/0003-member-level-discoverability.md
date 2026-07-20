# Discoverability is a Member-level action, decoupled from event-editing authority

## Status

accepted

## Context and decision

Every staff event route is gated by `RequireOrgAdmin`: creating, editing, publishing,
cancelling, and deleting an event, plus ticket-type CRUD, are all Org-Admin-only. The
`Discoverable` flag (see ADR 0002) shipped as one field on the shared
`PATCH /api/v1/staff/events/{id}` body, so it inherited that Org-Admin gate.

We have decided that **any active Member of the Organization may set an Event's
`Discoverable` flag**, while every other event edit stays Org-Admin-only. To do this:

- `discoverable` is removed from the `PATCH /events/{id}` body.
- A dedicated `PUT /api/v1/staff/events/{id}/discoverable` endpoint (body `{"discoverable": bool}`)
  becomes the sole writer of the flag. It is gated by session auth + an active Member only —
  no role check.
- `GET /events` (list) and `GET /events/{id}` are opened to any Member so they can see events
  to toggle; all write routes stay Org-Admin-only.
- The endpoint enforces the invariant ADR 0002 asserts: discoverability may only change on a
  `published` event. `draft` and `cancelled` events reject the change.

## Why this way

- **Advertising is a lighter decision than editing.** Flipping whether a published event
  appears in listings is a low-stakes, reversible operation any trusted org member can own,
  unlike renaming, rescheduling, or cancelling an event.
- **We deliberately did not use the assignment model.** The glossary defines Event Owner and
  Event Staff via Event assignments, and an earlier cut of this change scoped discoverability
  to Event Owners. We chose "any Member" instead because assignment-based access is not yet
  enforced anywhere, and building it only to gate one flag was more machinery than the need
  justified. Org Admin remains the sole authority for all other event actions.
- **Its own endpoint, not field-level auth.** Keeping the flag on the admin `PATCH` and
  authorizing per-field inside one handler would entangle two trust levels in one code path.
  A separate idempotent endpoint keeps the member-writable and admin-writable surfaces cleanly
  separated and maps directly to the UI toggle.

## Consequences

- A future reader will see that *every* event route is Org-Admin-gated **except** this one
  member-open toggle, and that Event Owner/Staff are defined but unused — this record explains
  why.
- Reachability edits (publish/cancel) stay with Org Admins, so a Member can advertise or
  un-advertise an event but cannot change whether it is sellable at all.
- If assignment-based event access is built later, this endpoint should be revisited to decide
  whether discoverability stays org-wide-open or narrows to assigned Members.
