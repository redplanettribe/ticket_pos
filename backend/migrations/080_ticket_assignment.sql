-- Ticket Assignment: the buyer names an email address for one Ticket, and the
-- person at that address may later accept it (#324, parent #322).
--
-- THIS SCHEMA SHIPS DARK, like migrations 072, 073 and 074 before it. Every
-- route that writes these columns is behind TICKET_ASSIGNMENT_ENABLED, which is
-- a NEW flag, separate from TICKET_QUESTIONS_ENABLED, and which ships closed. An
-- entirely NULL set of columns is the correct state of this migration in
-- production, and anybody who finds it that way has found the feature working as
-- designed (ADR 0045).
--
-- ASSIGNMENT IS FIELDS ON THE TICKET AND NOT A NEW ENTITY, which is the decision
-- this migration most wants recorded. A Ticket Assignment has exactly one
-- subject (a Ticket), exactly one current value (an address), and no life of its
-- own: it is not created, listed or deleted independently, and nothing ever
-- holds two of them for one Ticket. A `ticket_assignments` table would therefore
-- be a one-to-at-most-one join carrying an id nobody needs, plus a way for a
-- Ticket to acquire two current Holders.
--
-- AND THERE IS NO HISTORY TABLE, deliberately. The platform keeps the CURRENT
-- Holder and when it last changed, exactly as `ticket_answers` keeps the current
-- Answer and its updated_at and no more: who changed it, what it was before and
-- how many times are not facts this product adjudicates. The cost is real — a
-- buyer who reassigns away from a friend leaves no trace that the friend was
-- ever named — and it is the same cost ADR 0044 already accepted for the Answer.
--
-- THREE STATES, DERIVED AND NEVER STORED. There is no `assignment_state` column:
-- the state is read off the timestamps, which cannot disagree with themselves.
--
--   unassigned  holder_email IS NULL
--   assigned    holder_email IS NOT NULL AND accepted_at IS NULL
--   accepted    accepted_at IS NOT NULL
--
-- `accepted` IS MODELLED HERE AND IS UNREACHABLE IN THIS TICKET. Nothing in
-- #324 writes accepted_at or holder_customer_id — no mail is sent, so there is
-- no Assignment Link to click and no click to mint a Customer from. #325 adds
-- the Assignment mail and the accept flow and is what makes the third state
-- reachable. The columns land now rather than then because every read added here
-- has to describe the state a Ticket is in, and a reader that learns about a
-- third state later is a reader that has to be found and changed everywhere.

-- The address the buyer named, normalised (trimmed and lowercased) by
-- platform.NormalizeEmail before it arrives, exactly as every other address on
-- this platform is — so that `Ana@Example.com ` typed by a buyer and
-- `ana@example.com` proven at an inbox are one person and not two.
--
-- NULL IS `unassigned` AND IS THE ONLY WAY TO SAY IT. There is no empty string
-- state: an address the buyer cleared and an address never given are the same
-- fact, and two representations of one fact is one representation too many.
--
-- IT IS A THIRD PARTY'S CONTACT DETAIL, held at the word of somebody with no
-- authority to supply it, and that is why the retention job exists (#322): an
-- address never accepted is purged when the Event starts, taking this column and
-- leaving the Ticket, its Answers and the fact of the sale. That job is NOT in
-- this ticket, which is worth stating plainly — until it lands, this column
-- accumulates addresses with no scheduled end. The flag is what keeps that
-- window shut.
ALTER TABLE tickets ADD COLUMN holder_email TEXT;

-- The Customer the Holder proved themselves to be, and the whole of what
-- `accepted` means. Written only by the accept flow, from a click at the
-- address above, which is Proof of Email Ownership (ADR 0035).
--
-- NULL UNTIL ACCEPTED, AND NULL IN THIS TICKET ALWAYS. See the CHECKs below: a
-- Customer reference without an accepted_at is refused, because a Ticket that
-- names a Customer nobody proved would be the platform asserting an identity it
-- never verified — which is precisely what the accept step exists to avoid.
--
-- ON DELETE RESTRICT, beside `ticket_sales.customer_id` (migration 016) and
-- `consent_records.customer_id` (061), and for the same reason: a Customer who
-- holds a Ticket is a party to a sale that happened, and the database refuses to
-- be the thing that quietly forgets them. Erasure is a protocol with a person in
-- the loop, not a cascade.
--
-- NOTE WHAT THIS IS NOT: it is not ownership. The Ticket Sale, the Sale
-- Confirmation, the money and the Reversal Window all stay with the buyer, who
-- may reassign this Ticket at any time. A Holder is the named person a Ticket
-- was handed to. This is assignment, never transfer.
ALTER TABLE tickets ADD COLUMN holder_customer_id UUID REFERENCES customers (id) ON DELETE RESTRICT;

-- When the CURRENT address was named. It moves on every reassignment to a
-- different address and stands still when the same address is submitted again,
-- so that correcting a typo back to what it already said is not an event.
--
-- IT IS "WHEN THIS ADDRESS WAS NAMED" AND NOT "HOW MANY TIMES": with no history
-- table this is the only thing #322's per-Ticket mail cap and the buyer's "when
-- did I do that" have to read, and both need the current fact rather than the
-- series.
ALTER TABLE tickets ADD COLUMN assigned_at TIMESTAMPTZ;

-- When the Holder clicked, which is the moment a row became a person.
--
-- UNREACHABLE IN THIS TICKET — nothing writes it until #325. It is declared now
-- so that the state machine every read here derives is the FINAL one, and so
-- that the CHECKs below can state the invariants of all three states while there
-- is no data to violate them.
--
-- Cleared on reassignment, together with holder_customer_id: a Ticket handed to
-- somebody new has not been accepted by them.
ALTER TABLE tickets ADD COLUMN accepted_at TIMESTAMPTZ;

-- An address and the moment it was named travel together, both present or both
-- absent. Half of a pair is not a state this feature has, and a row with an
-- assigned_at and no address would make every state derivation ambiguous.
ALTER TABLE tickets
    ADD CONSTRAINT tickets_assignment_pair_ck
    CHECK ((holder_email IS NULL) = (assigned_at IS NULL));

-- ACCEPTANCE IMPLIES ASSIGNMENT, AND IMPLIES A CUSTOMER. There is no accepting
-- a Ticket nobody was handed, and no accepting it anonymously — the click mints
-- or matches a Customer and that Customer is the point. Stated as one CHECK
-- because it is one rule with two halves, and a row failing either half is the
-- same bug in the accept transaction.
--
-- This is what makes `accepted` unreachable-but-safe while #325 is unbuilt: any
-- half-written accept is refused by the database rather than left readable.
ALTER TABLE tickets
    ADD CONSTRAINT tickets_accepted_requires_holder_ck
    CHECK (
        accepted_at IS NULL
        OR (assigned_at IS NOT NULL AND holder_customer_id IS NOT NULL)
    );

-- And the converse: a Customer reference without an acceptance is an identity
-- the platform asserted without proof. Refused here rather than in Go, because
-- the whole value of `accepted` is that it cannot be true by accident.
ALTER TABLE tickets
    ADD CONSTRAINT tickets_holder_customer_requires_acceptance_ck
    CHECK (holder_customer_id IS NULL OR accepted_at IS NOT NULL);

-- "Which Tickets does this Customer hold", which is the read #325's Customer
-- Area needs and the read the RESTRICT above is checked with on every Customer
-- deletion. PARTIAL, because the column is NULL on every Ticket that was never
-- accepted — which is all of them today and most of them forever — and a full
-- index would be almost entirely NULLs.
--
-- NO INDEX ON holder_email, deliberately. Nothing looks a Ticket up BY address:
-- the buyer's surface reaches Tickets through their Ticket Sale, the accept flow
-- reaches one through a signed token naming the Ticket, and the retention purge
-- (#322) will sweep by the Event's start rather than by address. An index here
-- would also be a fast way to ask "which Tickets is this person named on",
-- which is a question nothing on this platform is entitled to ask of an address
-- that has proved nothing.
CREATE INDEX tickets_holder_customer_id_idx
    ON tickets (holder_customer_id)
    WHERE holder_customer_id IS NOT NULL;
