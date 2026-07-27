# Tax ID required on native Sales Channels, enforced in the service layer over nullable columns

## Context

Ecuadorian tax declarations (SRI) require the platform's Organizations to identify buyers by a *tipo de identificación* and number — cédula, RUC, or passport. Nothing in the system collected this: Customers and Ticket Sale snapshots held only email and name. Sales already exist without it, imports describe sales transacted elsewhere where the ID may never have been collected, and a Customer's stored assertion can drift from what a given sale was declared under.

## Decision

A **Tax ID** (type + number) is required to record a Ticket Sale on the native Sales Channels (`online`, `in_person`) and optional on `import`. The requirement lives in the sale-recording service layer only; the database columns — `tax_id_type`/`tax_id_number` on `customers` and snapshot columns `customer_tax_id_type`/`customer_tax_id_number` on `ticket_sales` — are nullable forever, because legacy rows are never backfilled and imported sales may legitimately lack an ID. Each sale's snapshot is immutable; the Customer's stored Tax ID follows asymmetric write-back rules mirroring the existing name rule (PRD #55, decision 8): a sale may *fill* a never-set Tax ID on any channel, may *refresh* it while the Customer is unverified, and never overwrites a verified Customer's Tax ID except when the sale runs under that Customer's own session, where the checkout override is the person's own assertion. The Tax ID carries no uniqueness constraint — email remains the sole Customer identity — and it is a fact of the sale's buyer, never of an individual attendee.

## Considered options

- **`NOT NULL`/CHECK constraints in the schema** — strongest enforcement, but legacy rows and imports both violate it; a channel-conditional CHECK would still be falsified by every pre-existing online sale, forcing a fabricated backfill of history.
- **Consumidor-final escape hatch** (SRI's `9999999999999` placeholder for small anonymous sales) — a fast path at the busiest channel (the door) that would get used constantly, defeating the declared purpose of collecting real IDs. Rejected for now; adding it later is forward-only.
- **Optional everywhere, prompted but skippable** — no friction, but declarations would carry holes exactly where the Organization is liable: its own native sales.

## Consequences

The requirement is invisible in the schema — a reader must know enforcement is in the service; this ADR is that record. Historical sales and some imported sales display no Tax ID ("—") indefinitely. The future in-person POS endpoint (none exists yet) inherits the requirement by going through the shared sale-recording path, not by re-implementing it. Retroactive collection is impossible by design: a sale recorded without an ID stays that way.
