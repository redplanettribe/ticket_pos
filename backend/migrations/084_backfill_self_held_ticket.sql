-- The buyer holds one Ticket by paying — on the Sales made BEFORE that was true
-- (#339, parent #338, ADR 0048).
--
-- ADR 0048 made one Ticket of every Online Sale the buyer's own at checkout:
-- assigned to the buyer's address and accepted in the transaction that minted
-- it, so the Sale page can say "your ticket" and the Holder List names the
-- buyer. Every Sale in production predates that decision, so without this
-- migration each existing buyer opens their Sale page to "0 of N tickets have
-- an address" and a form asking them to email their own address to themself.
--
-- THIS IS A DATA BACKFILL WITH NO SCHEMA CHANGE, in the family of 071 (Tickets
-- minted onto existing Sale Lines). It writes the four holder columns from 080
-- and nothing else: no mail, no `ticket_assignment_mails` row, no Answer
-- Reminder, and NOT `customers.verified_at` — accepted by purchase is not Proof
-- of Email Ownership (ADR 0048), and the sign-in module keeps that authority.
--
-- WHICH TICKET. The same one the checkout picks (sales/repository CommitSales):
-- the lowest-ordinal Ticket of the line whose Ticket Type sorts first by
-- sort_order, then name. Name ties are broken in the "C" collation because the
-- Go rule compares bytes, and a migration that disagreed with the checkout
-- about Ticket 1 would leave new and old Sales naming different Tickets as the
-- buyer's. Two lines of one Sale never share a Ticket Type (the checkout
-- merges them), but ticket_type_id and ordinal close the ordering anyway.
--
-- WHICH SALES — and the four that are left alone, each for a reason:
--
--   channel = 'online'      A door sale's or Sale Import's buyer is a name
--                           somebody else typed, and often is not attending.
--   not reversed            The Ticket admits nobody; a Holder on it would put
--                           a no-show on the roster and a reminder in an inbox.
--                           Both the status column (how an operator or free-sale
--                           reversal is recorded) and a `sale_reversals` row
--                           (a buyer's own Reversal Request, whatever became of
--                           it) disqualify the Sale.
--   Ticket 1 is unassigned  The buyer already handed it to somebody; that
--                           choice is never overwritten.
--   buyer holds nothing     A buyer who accepted another Ticket of the same
--                           Sale by its Assignment Link holds one Ticket, not
--                           two. Compared on the lower-cased, trimmed form of
--                           both sides, which is what platform.NormalizeEmail
--                           writes and what 016 used to fold Customers.
--
-- IDEMPOTENT BY CONSTRUCTION: a Sale whose Ticket 1 is held — by this
-- migration, by the checkout, or by anybody — matches nothing on a re-run, so
-- a replayed or restored database is changed by it exactly once.
--
-- THE TIMESTAMPS ARE THE SALE'S OWN. assigned_at = accepted_at =
-- ticket_sales.created_at, which is the `now` the checkout stamps on its own
-- self-held row, so Holder List ordering, reminder timing and the address
-- purge treat the Ticket as held since purchase. The CHECKs from 080 hold:
-- address and assigned_at arrive together, and acceptance arrives with both
-- and with the Sale's Customer, which 016 made NOT NULL.
--
-- INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED. The flag gates surfaces; the data
-- is ready the moment it flips.
WITH first_ticket AS (
    SELECT DISTINCT ON (ts.id)
        tk.id AS ticket_id,
        ts.customer_id,
        lower(btrim(ts.customer_email)) AS holder_email,
        ts.created_at
    FROM ticket_sales ts
    JOIN ticket_sale_lines l ON l.ticket_sale_id = ts.id
    JOIN ticket_types tt ON tt.id = l.ticket_type_id
    JOIN tickets tk ON tk.ticket_sale_line_id = l.id
    WHERE ts.channel = 'online'
      AND ts.status <> 'reversed'
      AND NOT EXISTS (SELECT 1 FROM sale_reversals sr WHERE sr.ticket_sale_id = ts.id)
      AND NOT EXISTS (
          SELECT 1
          FROM tickets held
          JOIN ticket_sale_lines hl ON hl.id = held.ticket_sale_line_id
          WHERE hl.ticket_sale_id = ts.id
            AND held.holder_email IS NOT NULL
            AND lower(btrim(held.holder_email)) = lower(btrim(ts.customer_email))
      )
    ORDER BY ts.id, tt.sort_order, tt.name COLLATE "C", l.ticket_type_id, tk.ordinal
)
UPDATE tickets tk
SET holder_email = ft.holder_email,
    holder_customer_id = ft.customer_id,
    assigned_at = ft.created_at,
    accepted_at = ft.created_at
FROM first_ticket ft
WHERE tk.id = ft.ticket_id
  AND tk.holder_email IS NULL;
