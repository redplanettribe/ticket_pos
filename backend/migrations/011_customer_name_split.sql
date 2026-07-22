-- Split the single Customer name on Ticket Sales into first and last name,
-- across every Sales Channel (see docs/adr/0005-customer-name-split-first-last.md).
-- The database is greenfield (no rows yet), so the NOT NULL columns need no
-- backfill and customer_name is dropped outright.

ALTER TABLE ticket_sales DROP COLUMN customer_name;
ALTER TABLE ticket_sales ADD COLUMN customer_first_name TEXT NOT NULL;
ALTER TABLE ticket_sales ADD COLUMN customer_last_name TEXT NOT NULL;
