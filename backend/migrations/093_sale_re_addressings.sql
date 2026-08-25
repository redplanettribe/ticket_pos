-- Sale Re-addressing: the Platform Operator records, against one active Online
-- Sale, the address its buyer meant; the corrected address accepts by mail
-- (#420, parent #419, ADR 0058).
--
-- THE REMEDY FOR A SALE THE SIGN-IN WALL COULD NOT HELP. Before ADR 0054 an
-- online buyer typed their own address and nothing proved it. A typo landed the
-- Sale Confirmation, the Confirmation Link and every later mail on an address
-- nobody can open, and the `TP-` reference on the success page is not a
-- credential. The wall stopped the set growing; this table is how the ones
-- already in it are fixed without a refund and a second purchase.
--
-- A RECORD AGAINST THE SALE AND NOT COLUMNS ON IT, which is the opposite of the
-- choice migration 080 made for Ticket Assignment, and for a reason 080 itself
-- states: an assignment "has exactly one current value and no life of its
-- own". A re-addressing has a life of its own. It is requested, it is accepted
-- or withdrawn, it may be replaced by another, and the accepted ones are kept
-- FOREVER as the evidence trail — "old address, new address, who, when" is the
-- record an operator reads a year later when the buyer disputes something. A
-- Sale may in principle be re-addressed twice, and its history is the list of
-- accepted rows here. NO COLUMN IS ADDED TO ticket_sales OR tickets: the Sale
-- learns of an acceptance only when #421 rewrites its customer_id and
-- snapshot email, and until that click the Sale is exactly as it was.
--
-- FOUR STATES, DERIVED AND NEVER STORED. There is no `status` column, on 080's
-- rule: a state read off the timestamps cannot disagree with itself.
--
--   pending    accepted_at IS NULL AND withdrawn_at IS NULL,
--              AND the Sale is active AND its Event has not started
--   accepted   accepted_at IS NOT NULL
--   withdrawn  withdrawn_at IS NOT NULL
--   expired    accepted_at IS NULL AND withdrawn_at IS NULL,
--              AND the Sale is reversed OR its Event has started
--
-- `expired` is read from OTHER tables (ticket_sales.status, events.starts_at)
-- and is why the state cannot be a column here even if one wanted it: a Sale
-- Reversal or an Organization moving its Event changes the state of a row it
-- never touches. Every read joins for it.
--
-- ONLY `pending` IS REACHABLE IN THIS TICKET. Nothing in #420 writes
-- accepted_at or withdrawn_at: the accept flow is #421 and the withdraw/replace
-- controls are #423. The columns land now, as 080's accepted_at did, so that
-- every read added here describes the final state machine and the partial
-- index below can state the one-pending rule before there is data to break it.
CREATE TABLE sale_re_addressings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The Sale being re-addressed. ON DELETE CASCADE follows ticket_sales,
    -- which in practice deletes nothing — a Sale Reversal voids a Sale and
    -- never removes it — and is here for the one path that does delete, a
    -- draft Event torn down, where a record about a Sale that no longer exists
    -- is a record about nothing.
    ticket_sale_id UUID NOT NULL REFERENCES ticket_sales (id) ON DELETE CASCADE,

    -- Who recorded it: the acting operator's email, as their Staff Session
    -- knows it and never as a request body claims it — the same rule the
    -- Operator Reversal's reversed_by_operator (migration 040-series) and every
    -- Payout's recorded_by follow. An email and not a member id, because an
    -- operator is a Member of nothing (ADR 0015) and the record must outlive
    -- any allowlist entry.
    operator_email TEXT NOT NULL,

    -- THE SALE'S ADDRESS AT REQUEST TIME: the one that was wrong. Copied here
    -- rather than read from the Sale later, because the Sale's own snapshot
    -- email is exactly what #421's acceptance overwrites — after which nothing
    -- else remembers where the mail used to go. It is the "old address" of the
    -- evidence trail the ADR promises the operator.
    previous_email TEXT NOT NULL,

    -- THE ADDRESS THE BUYER MEANT, normalised (trimmed and lowercased) by
    -- platform.NormalizeEmail before it arrives, exactly as every Customer
    -- address is — so that the Verified Customer #421 mints or matches from
    -- the click is the same person the Operator named, not a second record
    -- one capital letter away.
    --
    -- NULLABLE, AND NULL MEANS PURGED. Until accepted, this is an address
    -- typed by somebody who is not its owner, held at the Operator's word —
    -- the same kind of fact as an unaccepted Holder address (migration 080),
    -- and it ends the same way: when the Event starts, #424 widens the Holder
    -- Address Purge to null this column on every still-pending row for that
    -- Event, keeping the row and the operator/time facts. Nothing in #420
    -- writes NULL here; the column is nullable now so the purge is a widening
    -- and not a schema change. The CHECK below keeps an accepted row's
    -- address: once proven, it is the Customer's own and is never purged.
    corrected_email TEXT,

    -- Why. Free text from the Operator — "buyer wrote in from the support
    -- address", "organizer forwarded the reference" — bounded as the Operator
    -- Reversal's note is, and for the same reason: it is one sentence for a
    -- human, not a document.
    note TEXT,

    -- When the Operator recorded it, which is the moment the Re-addressing
    -- Link was minted for. THE TOKEN IS SIGNED OVER THIS ROW'S ID AND THIS
    -- INSTANT (sales.ReAddressingLinkSigner), so a withdrawal or a replacement
    -- — each of which ends this row and, for a replacement, opens a new one
    -- with its own id and instant — invalidates every earlier link by
    -- construction, without a revocation list. Microsecond precision is what
    -- a TIMESTAMPTZ keeps and what the signer compares at.
    requested_at TIMESTAMPTZ NOT NULL,

    -- When the corrected address clicked, which is the moment the Sale moved
    -- (#421). UNREACHABLE IN THIS TICKET.
    accepted_at TIMESTAMPTZ,

    -- When the Operator withdrew it, or when a later recording replaced it —
    -- one column for both, because from the record's side they are the same
    -- fact: this correction was never completed, by the Operator's own act
    -- (#423). UNREACHABLE IN THIS TICKET.
    withdrawn_at TIMESTAMPTZ,

    -- A record ends exactly once. Accepted and withdrawn are the two terminal
    -- states and they exclude each other: a row carrying both would be one
    -- whose derived state depends on which column a reader looked at first.
    CONSTRAINT sale_re_addressings_ended_once_ck
        CHECK (accepted_at IS NULL OR withdrawn_at IS NULL),

    -- An acceptance is an acceptance OF AN ADDRESS: the purge takes the
    -- corrected address only off rows nobody accepted, and a row that says
    -- "accepted" with no address would be the platform asserting somebody
    -- proved an address it no longer knows.
    CONSTRAINT sale_re_addressings_accepted_keeps_address_ck
        CHECK (accepted_at IS NULL OR corrected_email IS NOT NULL),

    CONSTRAINT sale_re_addressings_note_length_ck
        CHECK (note IS NULL OR char_length(note) <= 500)
);

-- ONE PENDING PER SALE, enforced by the database and not by a Go check that a
-- racing second Operator could slip past. Partial on the two terminal columns
-- rather than on the full derived state, deliberately: a row whose Sale was
-- reversed or whose Event started still occupies the slot, and that is right —
-- a new recording is refused on those Sales anyway, so the slot is never
-- wanted, and a purged row keeps saying "somebody tried" until history is
-- read. #423's replace withdraws the old row and inserts the new one in ONE
-- transaction, which this index is what makes safe under concurrency.
CREATE UNIQUE INDEX sale_re_addressings_one_pending_idx
    ON sale_re_addressings (ticket_sale_id)
    WHERE accepted_at IS NULL AND withdrawn_at IS NULL;

-- "Every re-addressing of this Sale, in the order they were recorded": the
-- Operator lookup's block, read on every lookup. requested_at is in the key so
-- the history is a range scan in order rather than a sort.
CREATE INDEX sale_re_addressings_sale_idx
    ON sale_re_addressings (ticket_sale_id, requested_at);
