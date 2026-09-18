-- The buyer is moved onto the Ticket they paid for, on the Sales the old
-- seating rule stranded (#647, parent #645, ADR 0074).
--
-- ADR 0048 seated the buyer on the first Ticket of the Sale's first line in
-- CATALOG ORDER. Organizations list their catalogs cheap-to-dear, so on every
-- Sale that mixed Ticket Types that meant "the cheapest thing in the basket",
-- and where the cheapest thing was free it meant the giveaway. ADR 0074 moved
-- the rule to the Sale's DEAREST line and #646 moved the commit spine with it,
-- which fixes every Sale made from now on and no Sale already made. This
-- migration is the other half: the Sales still sitting in the state the old
-- rule left them in, where the buyer holds a Ticket that cost nothing and the
-- Ticket they plainly paid for went out `unassigned`, owing its Answers and
-- chased by nobody who can give them.
--
-- THIS IS A DATA BACKFILL WITH NO SCHEMA CHANGE, in the family of 084 (the
-- Online Sale's Self-held Ticket) and 088 (the imported one's), and it follows
-- 084 closely enough that the differences are worth naming rather than leaving
-- to be noticed. It writes the same four holder columns from 080 and nothing
-- else: no mail of any kind, no `ticket_assignment_mails` row, no Answer
-- Reminder and no Answer, and NOT `customers.verified_at` — the warrant is
-- still the payment and paying is not Proof of Email Ownership (ADR 0048). No
-- Ticket is destroyed, no Ticket Sale Line is touched, and no money, Tax
-- Invoice or capacity moves: the seat moves WITHIN one Sale, between two
-- Tickets that both already exist and both stay.
--
-- IT IS NOT AN UPGRADE, and the word is avoided deliberately. An Upgrade is the
-- BUYER'S election, it reverses a free Ticket Sale and it destroys a Ticket. No
-- Sale is reversed here, nothing is destroyed, and no buyer has elected
-- anything. This is the platform correcting a guess it made on the buyer's
-- behalf and got systematically wrong.
--
-- 084 AND 088 ARE NOT RE-CUT AND NOT RE-RUN. Both kept the rule they ran under
-- (see their own headers): re-cutting them to the dearest rule would change no
-- production row, because both are idempotent by construction, and on a restored
-- database it would re-seat every historical mis-seat rather than the three this
-- migration is allowed to touch. The selection below is therefore stated from
-- scratch rather than inherited.
--
-- SELECTED BY PREDICATE AND NOT BY REFERENCE. No confirmation reference appears
-- in this file. A migration that named its three rows would be a list somebody
-- has to take on trust, would silently do nothing on a database where those rows
-- are numbered differently, and would hide the one thing worth reviewing: what
-- shape of Sale is being changed and why every other shape is not. The predicate
-- is the whole argument, and on production it matches exactly the three Sales
-- ADR 0074 names — `TP-4Z3MNO5A`, `TP-U2HKDYJD` and `TP-W7CXRAEE` — out of the
-- six Sales in the mixed free-and-paid shape. That was measured and not assumed:
-- run inside a rolled-back transaction against a copy of the production database
-- on 2026-09-18, this file changed six rows on exactly those three Sales — the
-- paid Ticket seated and the free one released on each — and a second execution
-- in the same transaction changed none.
--
-- TWO NARROWINGS OF THE WORDING, both deliberate and both stricter:
--
--   THE DEAREST LINE, NOT MERELY A DEARER ONE. The seat may only move to the
--   Ticket today's commit spine would pick. If that Ticket is held, the Sale is
--   left alone even though some cheaper paid line might have an unheld Ticket —
--   moving the buyer somewhere the spine would not have put them would leave a
--   corrected Sale disagreeing with every Sale made after #646, which is the one
--   outcome this whole exercise exists to end.
--
--   THE FREE TICKET MUST BE ACCEPTED, which a Self-held Ticket always is: it is
--   accepted in the transaction that mints it. A free Ticket merely ASSIGNED to
--   the buyer's own address and not yet accepted is somebody's pending
--   invitation, not the seat the old rule wrote, and is left alone.
--
-- WHICH SALES, and the ones deliberately left alone:
--
--   channel = 'online'      An In-Person Sale has no Self-held Ticket at all
--                           and an imported one's seat was 088's; neither is in
--                           the shape this is correcting.
--   not reversed            A reversed Sale's Tickets admit nobody, so moving a
--                           seat between them would put a no-show on a roster.
--                           WRITTEN AS `status <> 'reversed'` AND NOT AS
--                           `status = 'active'`, which is the looser of the two
--                           spellings and worth being honest about: the Upgrade's
--                           eligibility predicate (sales/repository self_held.go)
--                           says `= 'active'`. They select the same rows here and
--                           cannot come apart, because migration 010's CHECK
--                           admits exactly `active` and `reversed` and nothing
--                           else — but a third status added later would be
--                           included by this clause and excluded by that one, so
--                           a reader comparing the two should know which is which.
--                           `status` covers an operator's reversal and a
--                           `sale_reversals` row that is not `refused` covers a
--                           live Reversal Request — the schema's own definition
--                           of live, from `sale_reversals_live_per_sale_key`
--                           (migration 039), which is 088's narrowing of 084
--                           rather than 084's blunter "whatever became of it".
--   the buyer holds exactly An ambiguous seat is not corrected. A buyer holding
--   one Ticket of it       two Tickets of one Sale accepted one of them by
--                           Assignment Link, and which of the two the platform
--                           may move is not a question this migration gets to
--                           answer on their behalf.
--   that Ticket is free     THE ZERO BOUNDARY, which is ADR 0074's and is not
--                           re-argued here: the seat moves off a Ticket that
--                           cost nothing. A buyer mis-seated on the cheaper of
--                           two PAID Tickets is left where they are, because
--                           "which of the two did you pay for" has a real answer
--                           on both sides and the platform does not know it.
--   the dearest line's      THE WHOLE OF THE SAFETY ARGUMENT. The Ticket the
--   Ticket has NO holder    seat moves onto must be held by nobody at all —
--                           not assigned, not accepted, not pending. Two of the
--                           six buyers in this shape hold BOTH Tickets, and one
--                           assigned the paid Ticket to a third party, which is
--                           proof that "the dearer one is for a friend" is a
--                           real shape and not a hypothesis. Moving a seat onto
--                           a Ticket somebody else holds would take it from them
--                           silently, and no correctness argument is worth that.
--
-- THE 27 CROSS-SALE DOUBLE-HOLDERS ARE NOT TOUCHED, and the predicate excludes
-- them by construction rather than by a clause: a buyer who took a free Ticket
-- on one Sale and a paid Ticket on another has one seat on each, and neither
-- Sale has a dearer unheld line. Re-seating across Sales would mean reversing
-- one of them — an Upgrade, which is the buyer's election and never the
-- platform's (ADR 0074).
--
-- WHICH TICKET THE SEAT MOVES TO. The one the commit spine picks today
-- (sales/repository selfHeldSeat, #646): the first Ticket of the DEAREST line,
-- priced on `unit_price_cents` AS SOLD, ties broken by the catalog's order —
-- sort_order, then name. The name tie-break is made in the "C" collation for
-- 084's reason exactly: the Go rule compares bytes, and a migration that
-- disagreed with the spine about which Ticket is the buyer's would leave the
-- corrected Sales naming a different Ticket from the ones made after #646.
--
-- THE TIMESTAMPS ARE THE SALE'S OWN. assigned_at = accepted_at =
-- ticket_sales.created_at, exactly as 084 and 088 stamp theirs and exactly what
-- the spine would have written had it run under ADR 0074 at the time. The point
-- is that the corrected row is indistinguishable from an ordinary one: Holder
-- List ordering, reminder timing and the address purge all treat the Ticket as
-- held since the purchase, because that is when the buyer bought it.
--
-- THE FREE TICKET IS RELEASED, not destroyed and not re-assigned. Its four
-- holder columns go back to NULL and it becomes an ordinary `unassigned` Ticket
-- of the Sale, which is what the spine would have left it as. The Sale therefore
-- ends with exactly as many unassigned Tickets as it started with — the buyer is
-- chased to name somebody for the free Ticket instead of for the paid one, which
-- is the correct question, since the paid one is the one they are attending on.
-- The release is written only where the seating actually happened (it reads the
-- seat UPDATE's own RETURNING), so there is no ordering of these two statements
-- under which a buyer ends up holding neither Ticket.
--
-- IDEMPOTENT BY CONSTRUCTION, like 084: once the seat has moved, the buyer's one
-- Ticket sits on a PAID line, so the Sale no longer matches and a replay changes
-- nothing. The `holder_email IS NULL` guard on the seat UPDATE says the same
-- thing a second time, at the row.
--
-- INDEPENDENT OF TICKET_ASSIGNMENT_ENABLED. The flag gates surfaces; the data is
-- correct the moment it flips, and a backfill that waited for a flag would mean
-- the correction never happened at all.
WITH buyer_seat AS (
    -- Every Ticket of an active Online Sale whose Holder is the Sale's own
    -- buyer, carrying how many such Tickets that Sale has: exactly one is what
    -- an unambiguous seat looks like. Addresses are compared lower-cased and
    -- trimmed, which is the form platform.NormalizeEmail writes and the one 016
    -- folded Customers on.
    SELECT
        ts.id AS sale_id,
        tk.id AS ticket_id,
        l.unit_price_cents,
        tk.accepted_at,
        count(*) OVER (PARTITION BY ts.id) AS held_by_buyer
    FROM ticket_sales ts
    JOIN ticket_sale_lines l ON l.ticket_sale_id = ts.id
    JOIN tickets tk ON tk.ticket_sale_line_id = l.id
    WHERE ts.channel = 'online'
      AND ts.status <> 'reversed'
      AND NOT EXISTS (
          SELECT 1 FROM sale_reversals sr
          WHERE sr.ticket_sale_id = ts.id AND sr.status <> 'refused'
      )
      AND tk.holder_email IS NOT NULL
      AND lower(btrim(tk.holder_email)) = lower(btrim(ts.customer_email))
),
free_seat AS (
    -- The buyer's one Ticket, where it cost nothing and they have accepted it —
    -- a Self-held Ticket is accepted the moment the Sale is made.
    SELECT sale_id, ticket_id
    FROM buyer_seat
    WHERE held_by_buyer = 1
      AND unit_price_cents = 0
      AND accepted_at IS NOT NULL
),
paid_seat AS (
    -- The Ticket today's commit spine would seat that buyer on.
    SELECT DISTINCT ON (ts.id)
        ts.id AS sale_id,
        tk.id AS ticket_id,
        tk.holder_email AS current_holder,
        l.unit_price_cents,
        ts.customer_id,
        lower(btrim(ts.customer_email)) AS holder_email,
        ts.created_at
    FROM ticket_sales ts
    JOIN ticket_sale_lines l ON l.ticket_sale_id = ts.id
    JOIN ticket_types tt ON tt.id = l.ticket_type_id
    JOIN tickets tk ON tk.ticket_sale_line_id = l.id
    WHERE ts.id IN (SELECT sale_id FROM free_seat)
    ORDER BY ts.id, l.unit_price_cents DESC, tt.sort_order, tt.name COLLATE "C",
             l.ticket_type_id, tk.ordinal
),
stranded AS (
    SELECT
        f.ticket_id AS free_ticket_id,
        p.ticket_id AS paid_ticket_id,
        p.customer_id,
        p.holder_email,
        p.created_at
    FROM free_seat f
    JOIN paid_seat p ON p.sale_id = f.sale_id
    WHERE p.unit_price_cents > 0
      AND p.current_holder IS NULL
),
reseated AS (
    UPDATE tickets tk
    SET holder_email = s.holder_email,
        holder_customer_id = s.customer_id,
        assigned_at = s.created_at,
        accepted_at = s.created_at
    FROM stranded s
    WHERE tk.id = s.paid_ticket_id
      AND tk.holder_email IS NULL
    RETURNING s.free_ticket_id
)
UPDATE tickets tk
SET holder_email = NULL,
    holder_customer_id = NULL,
    assigned_at = NULL,
    accepted_at = NULL
FROM reseated r
WHERE tk.id = r.free_ticket_id;
