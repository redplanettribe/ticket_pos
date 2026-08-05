-- The sent-ledger: the (Customer, Event) pairs a Follow Digest has actually
-- carried to somebody (#220, parent #215, ADR 0030).
--
-- This is the backbone of the whole feature and it is worth saying why so
-- plainly, because the table looks trivial and its four jobs are not obvious
-- from its two columns. ADR 0030 gives them as one list:
--
--   NOVELTY. An Event is New to You when it has never appeared in a Digest sent
--   to YOU (CONTEXT.md). That is a fact about the reader rather than about the
--   Event, and this table is where it is written down. Nothing timestamps an
--   Event's publication or the flip of its `discoverable` flag, so novelty could
--   not have been derived from the Event at all — an Event published in January
--   and listed in March would have read as three months stale on the day the
--   public first saw it, and an Event tagged after publication would never have
--   been new to that Tag's followers.
--
--   CROSS-FOLLOW DEDUPE. A Customer who Follows both an Organization and a Tag
--   the same Event carries is matched twice and told once, because the second
--   match finds the row the first one wrote.
--
--   WEEK-TO-WEEK ANTI-REPETITION. A Digest never re-advertises what it already
--   advertised. Without this the same standing Event would be mailed out every
--   week for as long as it stayed upcoming, which is the shape of a system
--   people unsubscribe from.
--
--   SEND IDEMPOTENCY. A retried Digest composes against the ledger the earlier
--   attempt wrote, so what was already carried is not carried again.
--
-- A row is written ONLY for an Event actually INCLUDED in a Digest that was
-- actually SENT, and that is the invariant every reader below depends on. It is
-- why the write happens after the provider has accepted the message rather than
-- while the Digest is being composed (see digest/service.deliverDigest): a row
-- written for an Event nobody was shown makes that Event permanently invisible
-- to that person, and nothing would ever notice. Over-writing this table is
-- silent data loss dressed as a successful send.
--
-- INDEPENDENT OF FOLLOWS, deliberately. There is no follow_id here and no
-- foreign key to either Follow table. The ledger records what a person was
-- SHOWN, so Unfollowing and Following again does not make previously shown
-- Events new — ADR 0030 records that as an intended consequence, and a future
-- reader who "fixes" it by keying this on the Follow will have reopened the
-- weekly repetition the table exists to prevent.
--
-- COMPOSITE PRIMARY KEY, no surrogate id, exactly as the two Follow tables have
-- it. The pair IS the fact; nothing addresses a ledger row by an identifier of
-- its own. It is also the index the composition query reads by, which is always
-- an anti-join on `customer_id = $1`, the leading column.
--
-- ON DELETE CASCADE on both sides, for the reason the Follow tables cascade: a
-- ledger row for a deleted Customer or a deleted Event is a record of a showing
-- to nobody, of nothing. An orphan here would be worse than useless, since the
-- only thing that ever reads it is a query deciding what to mail.
CREATE TABLE follow_digest_sent_events (
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    -- When the Digest carrying this Event was sent. It is not read by the
    -- composition query — presence is the whole test — and it is here for the
    -- two readers that come later: the prune that drops rows once an Event has
    -- ended (ADR 0030), and the human answering "when were they told about
    -- this?" during a support thread.
    sent_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (customer_id, event_id)
);

-- The Event-first direction. The primary key cannot serve it, since its leading
-- column is the Customer, and two readers need it: the CASCADE above when an
-- Event is deleted, and the prune that clears a whole Event's rows once it has
-- ended. Both are "given an Event, find its ledger rows", which is the one
-- question the primary key answers worst.
--
-- It answers only the second half of the prune. Finding WHICH Events have ended
-- needs an index on `events` itself, which migration 057 adds.
CREATE INDEX follow_digest_sent_events_by_event_idx
    ON follow_digest_sent_events (event_id);
