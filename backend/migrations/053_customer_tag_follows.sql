-- A Customer's Follows of Tags (#218, parent #215).
--
-- The second kind of Follow, and the one that made the Follow Digest necessary.
-- A Follow of a Tag is a standing subscription to an interest rather than to a
-- publisher: whoever puts on an Event carrying it, the Customer hears about it
-- (CONTEXT.md, ADR 0030). Nothing sends email yet.
--
-- A SECOND TABLE beside customer_organization_follows rather than a `subject_id`
-- added to it or a polymorphic `follows` table, for the reason 052 gives at
-- length and this table is the test of: the foreign key below is what makes
-- "deleting a Tag removes its Follows" true in the schema instead of in whichever
-- service happens to remember, and a polymorphic subject column can carry no
-- foreign key at all. The listing endpoint unions the two in Go and puts the
-- discriminator on the wire, where it costs nothing.
--
-- EVERY TAG IS FOLLOWABLE, Preset and Custom alike, and there is deliberately no
-- CHECK constraining this to `curated = TRUE`. ADR 0030 records the reasoning:
-- restricting the pool was the obvious way to bound how much mail a Follow can
-- produce, and it was rejected because the weekly Digest bounds that
-- structurally, at a number the reader can verify — while the restriction would
-- have cost the most valuable case, a narrow interest like "Techno", which is
-- exactly the sort of Tag that is Custom. A future reader who adds the
-- constraint here will be undoing the decision that made every Tag followable.
--
-- COMPOSITE PRIMARY KEY, doing the same two jobs as in 052. It is the uniqueness
-- that makes the Follow idempotent — following twice leaves one row, and the
-- repeat is an ON CONFLICT that returns the stored `followed_at` rather than
-- moving it, so a retried request cannot rewrite when the person subscribed. And
-- it is the index the Customer's own Following list is read by, since that read
-- is always `WHERE customer_id = $1`. No surrogate id: a Follow is unfollowed by
-- naming what it is a Follow of, never by an identifier of its own.
--
-- KEYED BY tag_id, not by canonical_key, even though the canonical key is unique
-- and is what the API names a Tag by. The id is what the cascade can hang off,
-- and a Follow of a Tag that no longer exists is worse than an orphan — it is an
-- input to a mailing that can match nothing, and nothing else would ever look at
-- it again. The canonical key is the address; the id is the reference.
--
-- The second index is the direction the Digest will read this from: given an
-- Event that carries a Tag, who Follows that Tag. The primary key cannot serve
-- it — its leading column is the Customer — and that fan-out is the whole of
-- what this table exists to make possible.
CREATE TABLE customer_tag_follows (
    customer_id UUID NOT NULL REFERENCES customers (id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    followed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (customer_id, tag_id)
);

CREATE INDEX customer_tag_follows_by_tag_idx
    ON customer_tag_follows (tag_id);
