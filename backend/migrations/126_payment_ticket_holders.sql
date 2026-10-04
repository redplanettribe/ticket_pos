-- The Holder addresses a buyer names at a Named Tickets checkout, held on the
-- PAYMENT until there is a Ticket to assign (#669, parent #665, ADR 0076).
--
-- THE SEVENTH THING THE CHECKOUT FORM HOLDS FOR CONFIRM, and it rides the
-- Payment for the reason the Answers beside it do (migration 074): when the
-- buyer types an address there is no Ticket, and between the form and the
-- Ticket Sale sits the Payment Provider's redirect.
--
-- NOBODY IS WRITTEN TO FROM THIS TABLE. ADR 0076 replaces ADR 0046's "an
-- abandoned checkout holds no third party's address" with "it may hold one for
-- 30 days, and never writes to it". The commit copies these onto the minted
-- Tickets as Ticket Assignments (#670), and only then is anybody mailed.
--
-- KEYED BY (PAYMENT LINE, INDEX), exactly as `payment_ticket_answers` is, so
-- the address and the Answers for one Ticket meet on the same pair and both
-- land on `tickets.ordinal` = index. One address per Ticket: a second row for
-- the same pair would be two Holders with nothing to say which is meant.
--
-- THE BUYER'S OWN TICKET HAS NO ROW. Begin-checkout seats it with the shared
-- Self-held predicate and holds no address for it, so a row here is always a
-- Ticket somebody was named for - possibly the buyer, by their own address,
-- which the commit accepts at once.
--
-- RETENTION IS THE HELD ANSWERS' RULE, IN THE SAME STATEMENT: purged 30 days
-- after a Payment that never reached 'approved', never on 'expired' alone (see
-- PurgeAbandonedCheckoutAnswers). The cascade below is not the retention rule;
-- the Payment and its lines are kept forever.
CREATE TABLE payment_ticket_holders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    payment_line_id UUID NOT NULL REFERENCES payment_lines (id) ON DELETE CASCADE,

    -- ONE-BASED, TO MATCH `tickets.ordinal` and `payment_ticket_answers`.
    ticket_index INTEGER NOT NULL CHECK (ticket_index > 0),

    -- Normalised by catalog.ParseHolderEmail before it is written, the rule
    -- every Ticket Assignment's address goes through, so the commit copies it
    -- and has no opinion of its own.
    holder_email TEXT NOT NULL CHECK (holder_email <> ''),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (payment_line_id, ticket_index)
);
