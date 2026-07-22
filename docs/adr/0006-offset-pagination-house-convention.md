# Offset pagination with a nested envelope is the house convention for staff list APIs

## Context

The Event Sales list is the first paginated list endpoint in the system — until now every list (`ListEventsByOrganizationID`, `ListTicketTypesByEventID`, `ListImportBatches`, the Storefront listings) returns an unbounded slice. The Sales list is a filterable, user-sortable back-office table that must show a result count ("247 sales"), let a staffer jump to an arbitrary page, and sort by any of several columns. Because it is the first, whatever shape it takes becomes the pattern the next staff list (events, members) copies.

## Decision

Staff list endpoints use **offset/limit pagination** (`page`, `page_size`) and return a **nested envelope**:

```json
{ "data": [ … ], "pagination": { "page": 1, "page_size": 50, "total": 247, "total_pages": 5 } }
```

`page_size` defaults to 50 and is clamped to a maximum (100); `sort`/`dir` are validated against per-endpoint allowlists. The total is computed in the same query via `COUNT(*) OVER()`. Scalability is carried by indexes and input clamping, not by the pagination style: the Sales list is served by a composite index on `ticket_sales (event_id, sold_at DESC, id DESC)` with supporting indexes for the other sort columns, and ordering always carries an `id` tiebreaker so equal timestamps do not reorder between pages.

## Considered options

- **Keyset / cursor pagination** — stable under inserts and fast at any depth, and the reflexive choice when "scalable" is the goal. Rejected here: it cannot return a total or support jump-to-page (both of which *are* the back-office UI), and it fights user-switchable multi-field sorting because every sort order needs its own tiebreaker-encoded cursor. Its one real advantage — deep-offset performance — only pays off at millions of rows in a single result set, whereas a single Event's sales are bounded in the low tens of thousands (imports cap ~10,000 rows).
- **Flat envelope** (`{ "sales": [...], "page", "page_size", "total" }`) — less ceremony, but a nested `pagination` object lets a future firehose endpoint add `next_cursor` inside it without a breaking change, and reads as one reusable shape across endpoints.

## Consequences

The envelope is forward-compatible: an endpoint that genuinely needs a cursor later adds `next_cursor` under `pagination` and existing clients that read `data` keep working. Customer search (`q`) uses a substring `ILIKE` scan within the already-narrow `event_id` scope; a `pg_trgm` trigram index is the documented upgrade path when a single Event's volume warrants it. Because offset counts and rows can shift as data changes between page loads, this convention is for staff back-office views, not for append-heavy public feeds.
