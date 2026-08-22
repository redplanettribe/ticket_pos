-- Backfill: every Ticket Sale recorded before migration 070 gets its Tickets
-- (#308, ADR 0043).
--
-- The invariant this feature ships is "there is one Ticket per ticket sold",
-- with no qualifier about when the sale happened. A table that holds only the
-- sales made since Tuesday would force every reader of it to know about
-- Tuesday, and "how many Tickets does this sale have" would answer 0 for the
-- whole existing book of business.
--
-- REVERSED TICKET SALES ARE BACKFILLED TOO — there is deliberately no
-- `WHERE status = 'active'` below. A Ticket carries no status of its own;
-- whether it stands is read from its Ticket Sale. Skipping reversed sales would
-- smuggle liveness into this table by omission, and would mean a reversed sale
-- had fewer Tickets than it sold, which is not a thing the model can say.
--
-- `created_at` is taken from the LINE and not from NOW(), so that a Ticket's
-- creation timestamp means the same thing on a backfilled row as on a minted
-- one: when its Ticket Sale was recorded. Dating a sale from last March with
-- today is the kind of small lie that later gets charted.
--
-- ON CONFLICT DO NOTHING against the unique (ticket_sale_line_id, ordinal)
-- makes this safe to run twice. The runner applies a file once, but this is the
-- statement someone will reach for by hand the day a line is found without its
-- Tickets, and a backfill that doubles the table on a second run is a trap laid
-- for exactly that moment.
--
-- One statement, and no batching. `generate_series` fans each line out to its
-- quantity, so this writes one row per ticket ever sold on the platform — a
-- number in the same order as the row count of `ticket_sale_lines` times its
-- average quantity, and small enough that a chunked migration would be
-- ceremony. A 500-row Sale Import mints 500 Tickets; that was accepted when the
-- table was.
INSERT INTO tickets (ticket_sale_line_id, ordinal, created_at)
SELECT l.id, ordinals.n, l.created_at
FROM ticket_sale_lines l
CROSS JOIN LATERAL generate_series(1, l.quantity) AS ordinals (n)
ON CONFLICT (ticket_sale_line_id, ordinal) DO NOTHING;
