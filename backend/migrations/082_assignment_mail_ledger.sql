-- The ledger of Assignment mails: which Ticket the platform wrote to a stranger
-- about, on whose say-so, and when (#332, parent #322, ADR 0046).
--
-- THIS TABLE EXISTS ONLY TO SAY NO, exactly as `answer_reminders` (migration
-- 079) does, and it is worth being blunt about why it is needed at all. A buyer
-- may reassign a Ticket at any time, and every reassignment to a NEW address
-- mails that address. Uncapped, that makes one Ticket an unlimited mailer: point
-- it at a fresh address, a mail goes; repeat. Purchase Limit does not close this
-- — it is "a deterrent against taking too many rather than a defence against
-- someone minting identities" (CONTEXT.md), it is keyed on the Customer rather
-- than on sends, and it is unset on most Ticket Types, so a Free Ticket Type
-- would otherwise be a bulk sender on the platform's own transactional domain.
-- The bounces and the spam complaints land on ONE sending reputation, shared
-- with passcodes and Sale Confirmations, which is the asset ADR 0009 and the
-- OTP global ceiling already exist to protect.
--
-- A LEDGER OF SENDS, NOT COUNTERS ON THE TICKET, for the reason 079 gives at
-- length: the rationing asks two questions at once — "how many has this Ticket
-- ever sent" and "how many has this buyer sent lately" — and a pair of
-- hand-maintained columns eventually answers them inconsistently. A COUNT and a
-- COUNT-since cannot contradict each other. It also keeps a mail's writes off
-- `tickets`, which the assignment write already locks a row of.
--
-- IT IS NOT THE ASSIGNMENT HISTORY TABLE ADR 0046 REJECTED, and the difference
-- is the whole of what makes it admissible. That rejection was about keeping
-- "a permanent store of addresses the purge is meant to end". THERE IS NO
-- ADDRESS IN THIS TABLE, and no column from which one could be recovered: it
-- says a mail went out for this Ticket at this moment, and not where it went.
-- The Holder Address Purge (migration 081) therefore has nothing to take from
-- here, and a purged Ticket keeps its spent allowance rather than being handed a
-- fresh one — which is the point, though in practice no route reassigns a Ticket
-- after its Event has started anyway (AssignmentRefusedEventStarted).
--
-- THIS SCHEMA SHIPS DARK, like 080 and 081 before it: every route that writes it
-- is behind TICKET_ASSIGNMENT_ENABLED, which ships closed. An empty table is the
-- correct state of this migration in production.
CREATE TABLE ticket_assignment_mails (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The Ticket the mail was about, which is the unit of the HARD CAP: a Ticket
    -- gets a fixed number of Assignment mails for its whole life, and no amount
    -- of reassigning buys more.
    --
    -- ON DELETE CASCADE follows `tickets`, which in practice deletes nothing —
    -- a Sale Reversal voids a sale and never removes it. The cascade is here for
    -- the one path that does delete, an Event torn down while still a draft,
    -- where a row about a Ticket that no longer exists would be a row about
    -- nothing.
    ticket_id UUID NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,

    -- The BUYER who caused the send — never the Holder it was sent to. This is
    -- the unit of the RATE LIMIT, and keying it on the buyer is what stops the
    -- cap being sidestepped by spreading one script across fifty Tickets of one
    -- free order: fifty Tickets is fifty separate per-Ticket allowances, and this
    -- column is the only thing that sees them as one person.
    --
    -- IT IS AN ID AND NOT AN ADDRESS. The buyer's address is on their Ticket
    -- Sale, where an erasure request already reaches it; a copy here would be a
    -- second place personal data lives, kept forever, for a job that already
    -- knows where the first is.
    buyer_customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,

    -- When the mail was accepted by the provider. WRITTEN AFTER THE SEND, never
    -- before, on 079's rule: a row written first would ration a buyer out of a
    -- mail nobody received. The reverse failure — a send whose row was lost
    -- because the process died between the two — costs at most one extra mail
    -- later, which is the direction worth failing in.
    sent_at TIMESTAMPTZ NOT NULL
);

-- "How many has this Ticket ever sent", read on every assignment that would
-- change an address. Small and hot.
CREATE INDEX ticket_assignment_mails_ticket_idx
    ON ticket_assignment_mails (ticket_id);

-- "How many has this buyer sent since T", read on the same call. The timestamp
-- is in the key so the rolling window is a range scan rather than a filter over
-- one buyer's whole history.
CREATE INDEX ticket_assignment_mails_buyer_idx
    ON ticket_assignment_mails (buyer_customer_id, sent_at);
