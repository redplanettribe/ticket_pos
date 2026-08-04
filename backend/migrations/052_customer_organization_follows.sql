-- A Customer's Follows of Organizations (#217, parent #215).
--
-- A Follow is a standing subscription, not a shortlist: its whole payload is the
-- Follow Digest, and pressing it is a request to be written to (CONTEXT.md,
-- ADR 0030). Nothing sends email yet — this table is the record the Digest will
-- later read.
--
-- ONE TABLE PER FOLLOWED THING, not one polymorphic `follows` table with a
-- `subject_type` discriminator and a nullable id per kind. Tag Follows (#218)
-- land beside this as their own table. The reason is the foreign keys: the
-- cascade below is what makes "deleting an Organization removes its Follows"
-- true in the schema rather than in whichever service happens to remember, and a
-- polymorphic subject column cannot carry a foreign key at all. The listing
-- endpoint unions the two and puts the discriminator on the wire, where it costs
-- nothing, instead of in the storage, where it would cost referential integrity.
--
-- COMPOSITE PRIMARY KEY, and it is doing two jobs. It is the uniqueness that
-- makes a Follow idempotent — following twice leaves exactly one row, and the
-- repeat is an ON CONFLICT that returns the existing `followed_at` rather than
-- moving it, so a retried request cannot rewrite when the person subscribed. And
-- it is the index the Customer's own list is read by, since that read is always
-- `WHERE customer_id = $1` and the Customer is the leading column. No surrogate
-- id: nothing addresses a Follow by an identifier of its own. It is unfollowed
-- by naming what it is a Follow of, which is what the Customer sees.
--
-- ON DELETE CASCADE on both sides. A Follow is meaningless without either end of
-- it: a deleted Organization has nothing left to hear about, and a Customer's
-- record going takes their subscriptions with it. Orphan rows here would be
-- worse than useless — they are an input to an email job, and the one thing a
-- mailing must never do is send about something that no longer exists.
--
-- The second index is for the direction this table will be read from when the
-- Digest is composed: given an Organization that just published an Event, who
-- Follows it. The primary key cannot serve that read — its leading column is the
-- Customer — and while ADR 0030 gives Organizations no follower counts and no
-- analytics, the Digest itself is the fan-out this index exists for.
CREATE TABLE customer_organization_follows (
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    followed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (customer_id, organization_id)
);

CREATE INDEX customer_organization_follows_by_organization_idx
    ON customer_organization_follows (organization_id);
