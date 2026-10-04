-- Named Tickets (#667, parent #665, ADR 0076): an Event may require that every
-- Ticket bought on its Storefront names its Holder and answers its required
-- Ticket Questions at checkout.
--
-- ON FOR EVERY NEW EVENT, OFF FOR EVERY EVENT THAT ALREADY EXISTS. The existing
-- Events' organizers authored their questions under "nothing ever blocks a
-- checkout", and some of their buyers are mid-checkout the day this deploys, so
-- their live checkouts must not change under them. A new Event is born with the
-- setting on, and the column's default says so, so that the database default is
-- the product default and no insert path has to remember to state it.
--
-- Two statements rather than ADD ... DEFAULT TRUE followed by an UPDATE: adding
-- the column with a constant default stamps every existing row with FALSE
-- without rewriting the table or locking its rows, and the second statement only
-- changes the default the next insert reads. Same outcome, no table rewrite.
ALTER TABLE events ADD COLUMN requires_named_tickets BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE events ALTER COLUMN requires_named_tickets SET DEFAULT TRUE;
