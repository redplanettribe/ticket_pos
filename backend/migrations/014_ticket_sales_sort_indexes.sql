-- Sales list non-default sort indexes (#38, ADR-0006).
-- #36 added ticket_sales_event_sold_at_idx for the default sort (sold_at DESC,
-- id DESC). The Sales list also sorts by recorded-at (created_at) and by customer
-- name; each carries a trailing id tiebreaker so equal primary values keep a
-- stable order across pages. These composite indexes, scoped by event_id like the
-- default, back those sorts without a filesort.
--
-- recorded-at (created_at): sorted with an id tiebreaker.
CREATE INDEX ticket_sales_event_created_at_idx ON ticket_sales (event_id, created_at DESC, id DESC);

-- customer name: ordered by last name then first name, with an id tiebreaker.
CREATE INDEX ticket_sales_event_customer_idx ON ticket_sales (event_id, customer_last_name, customer_first_name, id);

-- Amount is deliberately NOT indexed: it is a computed aggregate
-- (SUM(quantity * unit_price_cents)) over a sale's Ticket Sale Lines, produced by
-- the lateral subquery the list query already builds for the rollup, so there is
-- no stored column to index. A single Event's sales are bounded (imports cap
-- ~10,000 rows, ADR-0006), so sorting that aggregate in memory is acceptable;
-- a materialized per-sale total would be the upgrade path if volume warrants it.
