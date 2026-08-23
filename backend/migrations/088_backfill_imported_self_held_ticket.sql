-- The imported Sale's buyer holds one Ticket too — on the imports recorded
-- BEFORE that was true (#394, parent #391, ADR 0055).
--
-- ADR 0048 gave the buyer of an Online Sale one Ticket of their own and
-- migration 084 backfilled every Sale that predated it, but 084 excluded the
-- `import` and `in_person` channels in a single clause: "A door sale's or Sale
-- Import's buyer is a name somebody else typed, and often is not attending."
-- ADR 0055 finds that clause right about a door sale and wrong about an import
-- — a Sale Import transcribes a transaction that already happened, and the
-- file's email column is the person who bought — and supersedes it for
-- `import` alone. This migration is the other half of that: without it the
-- rule would be true only of imports recorded from today, and the Holder List
-- would keep rendering every earlier one as nobody named.
--
-- THIS IS A DATA BACKFILL WITH NO SCHEMA CHANGE, in the family of 071 (Tickets
-- minted onto existing Sale Lines) and 084 (the Online Sale's Self-held
-- Ticket). It writes the four holder columns from 080 and nothing else: no
-- mail of any kind — no Sale Confirmation, no Assignment Link, no Assignment
-- Reminder, no Answer Reminder, no No Longer Holding notice — no
-- `ticket_assignment_mails` row, and NOT `customers.verified_at`. The grounds
-- for `accepted` here are the transcription and not a proof (ADR 0055), which
-- is a weaker warrant than 084's payment, and it is stated plainly rather than
-- dressed up: the platform is presuming the buyer attends. The sign-in module
-- keeps the authority to say who has proved an address.
--
-- MIGRATION 084 IS NOT RE-RUN, and this file is not a second copy of it with
-- one word changed for that reason. 084's residue on the affected Event is 2
-- Tickets and those are its correct behaviour — their buyers accepted a
-- different Ticket of the same Sale by Assignment Link, so the "buyer holds
-- nothing" skip below leaves them exactly where they are.
--
-- WHICH TICKET. The same one the checkout picks (sales/repository CommitSales)
-- and the same one 084 picked: the lowest-ordinal Ticket of the line whose
-- Ticket Type sorts first by sort_order, then name. Name ties are broken in the
-- "C" collation because the Go rule compares bytes, and the ORDER BY below is
-- byte-for-byte 084's — a migration that disagreed with the checkout or with
-- 084 about Ticket 1 would leave new, online and imported Sales naming
-- different Tickets as the buyer's. ADR 0055 makes the forward rule and 0048's
-- the same sentence; this keeps the backfills the same sentence too.
--
-- WHICH SALES — and the five that are left alone, each for a reason (one more
-- than 084, which did not split a live reversal from a refused one):
--
--   channel = 'import'      Online Sales were 084's and are untouched here.
--                           `in_person` stays out pending a buyer surface, not
--                           on principle (ADR 0055): a door sale can be
--                           assigned by nobody, and a presumption with no undo
--                           is not one this platform should make.
--   not reversed            The Ticket admits nobody; a Holder on it would put
--                           a no-show on the roster and a reminder in an inbox.
--                           An imported Sale is reversed by staff or by a batch
--                           undo, and both are recorded in the status column.
--   no LIVE reversal        THIS NARROWS 084, deliberately. 084 disqualified
--                           any Sale carrying a `sale_reversals` row "whatever
--                           became of it", so a buyer who asked for their money
--                           back, was REFUSED, and is still coming would hold
--                           nothing. The predicate here is the schema's own:
--                           `sale_reversals_live_per_sale_key` (migration 039)
--                           is partial ON `status <> 'refused'`, which is the
--                           database's existing definition of a live Reversal
--                           Request — a refusal means nothing happened. This
--                           matches zero rows differently from 084 today, which
--                           is exactly why it is safe to state correctly now.
--   Ticket 1 is unassigned  The buyer already handed it to somebody; that
--                           choice is never overwritten, accepted or not.
--   buyer holds nothing     A buyer who accepted another Ticket of the same
--                           Sale by its Assignment Link holds one Ticket, not
--                           two. Compared on the lower-cased, trimmed form of
--                           both sides, which is what platform.NormalizeEmail
--                           writes and what 016 used to fold Customers.
--
-- IDEMPOTENT BY CONSTRUCTION: a Sale whose Ticket 1 is held — by this
-- migration, by the recording flow, or by anybody — matches nothing on a
-- re-run, so a replayed or restored database is changed by it exactly once.
--
-- THE TIMESTAMPS ARE THE SALE'S OWN. assigned_at = accepted_at =
-- ticket_sales.created_at, which is the `now` the recording flow stamps on its
-- own self-held row, so the backfilled row is indistinguishable from one the
-- flow would have written and Holder List ordering, reminder timing and the
-- address purge all treat the Ticket as held since the Sale was recorded. It is
-- created_at and NOT sold_at: sold_at is when the transcribed transaction
-- happened, which may be months earlier, and stamping a holder column with it
-- would date an assignment before the row it sits on existed. The CHECKs from
-- 080 hold: address and assigned_at arrive together, and acceptance arrives
-- with both and with the Sale's Customer, which 016 made NOT NULL.
--
-- INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED. The flag gates surfaces; a backfill
-- is a one-shot act, and a gated one running while the flag was closed would
-- mean the data never existed at all.
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
    WHERE ts.channel = 'import'
      AND ts.status <> 'reversed'
      AND NOT EXISTS (
          SELECT 1 FROM sale_reversals sr
          WHERE sr.ticket_sale_id = ts.id AND sr.status <> 'refused'
      )
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
