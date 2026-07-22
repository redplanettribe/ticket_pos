-- Sales list default sort index (ADR-0006).
-- The Sales list is served by the Event's active Ticket Sales ordered by
-- sold_at descending with an id tiebreaker; this composite index backs that
-- default sort (and the per-Event scan) without a filesort.
CREATE INDEX ticket_sales_event_sold_at_idx ON ticket_sales (event_id, sold_at DESC, id DESC);
