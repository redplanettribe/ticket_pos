# Customer name stored as separate first and last name across all Sales Channels

## Context

A Customer was identified on every Ticket Sale by a single `customer_name` (`TEXT NOT NULL` on `ticket_sales`), shared by the online, in-person, and import channels and read back by the Sale Confirmation email. While making the Sale Import template easier to fill in, we chose to capture the buyer's given name and family name as distinct inputs, which raised the question of whether to split only in the import template or in the canonical model.

## Decision

We store the Customer name as two required columns — `customer_first_name` and `customer_last_name` (`TEXT NOT NULL`) — everywhere a name is recorded, not only in imports. Where a single display name is needed (Sale Confirmation email, notices, preview display) the two are joined as `"First Last"` at the call site; the email/notice structs keep a single name field rather than propagating the split. The Sale Import preview API exposes the two fields separately so the preview can pinpoint which half is wrong.

## Considered options

- **Import-only, concatenated on commit** — the template collects first/last but the parser joins them into the existing single `customer_name`. Smaller change, no migration, but it leaves the two halves unrecoverable and makes the import channel model a name differently from the rest of the system.
- **Full split across all channels (chosen)** — one consistent Customer model. Larger surface (schema, repository, service, handler DTOs, import parse/validate/preview), but the database is greenfield with no rows, so no backfill is required and the `NOT NULL` columns are safe to add.

## Consequences

Both names are mandatory on every channel, so a mononym Customer must still supply a last name; relaxing this later to `first required / last optional` is a forward-only change and easier than the reverse. A new forward-only migration (`011`) drops `customer_name` and adds the two columns.
