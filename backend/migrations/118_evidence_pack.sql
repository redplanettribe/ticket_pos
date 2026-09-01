-- WHAT WAS HANDED OVER, AND NOTHING OF WHAT WAS IN IT (#568, parent #556,
-- ADR 0067).
--
-- The Consent Evidence Pack is generated on demand from the per-subject record
-- and NEVER STORED. That is the whole design: a pack is assembled precisely
-- because somebody asked what the platform holds about them, so storing it
-- would give the platform a second, unindexed copy of its most sensitive data,
-- created by the act of answering a privacy request — and an erasure would then
-- have to reach inside a ZIP.
--
-- But a handover still has to be provable years later, so TWO FACTS SURVIVE THE
-- FILE and only two: the pack's SHA-256, and the ids of the acts it covered.
-- Together they answer both questions anybody can ask about a handover — "did
-- one happen, and what did it say" — because the pack is DETERMINISTIC: the same
-- acts regenerate the same bytes, so a file produced today either hashes to a
-- stored value or it is not the file that was sent. Nothing else about the pack
-- is here. Not the subject, not the address, not the operator, not the editions,
-- not a copy of a single row of the contents.
--
-- THE SHA-256 IS ALSO THE FILENAME'S KEY, which is the #546-vs-#548 resolution
-- ADR 0067 records: `consent-evidence-<first 16 hex of this value>-<date>.zip`.
-- #546 wanted the name never to be the email; #548 forbade ever writing the
-- HMAC digest to a file, because a key rotation would leave it unresolvable in
-- a document meant to stay meaningful for years. A hash of the pack's own bytes
-- is neither: it names nobody, it depends on no key, and it resolves a file in
-- somebody's mailbox to the row below it with no second index.
--
-- WHO GENERATED IT IS NOT HERE, and its absence is deliberate: #569's
-- `consent_access_log` records the `evidence_export` act with the actor, the
-- subject and this same `pack_sha256`, because the house pattern hangs
-- attribution off the log rather than off the domain row. This table is the
-- ARTIFACT's row; that one is the ACT's. Duplicating the actor here would be two
-- facts that can disagree.

CREATE TABLE IF NOT EXISTS consent_evidence_packs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The SHA-256 of the whole ZIP, lowercase hex. NOT UNIQUE, and that is not
    -- an oversight: the pack is deterministic, so generating one twice over an
    -- unchanged record produces the same 64 characters, and both generations
    -- are real handovers that happened on different days. A UNIQUE here would
    -- make the second one fail — refusing to answer a data subject because
    -- somebody else already had — or, worse, invite an upsert that rewrote the
    -- first one's date.
    pack_sha256 TEXT NOT NULL CHECK (pack_sha256 ~ '^[0-9a-f]{64}$'),

    -- How many bytes the file was. The one fact about the contents worth
    -- keeping, because it is the only one that costs nothing to disclose and
    -- lets a truncated download be told from a different pack before anybody
    -- computes a hash.
    size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),

    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Reading the handovers is a "what did we send, and when" question, so the
-- index is on the day rather than on the hash: the hash is looked up once in a
-- lifetime, from a file somebody is holding, and a sequential scan over a table
-- that grows by one row per privacy request is not a cost.
CREATE INDEX IF NOT EXISTS idx_consent_evidence_packs_generated_at
    ON consent_evidence_packs (generated_at DESC);

-- WHAT THE PACK COVERED: one row per act, and the act id alone.
--
-- TWO POPULATIONS IN ONE TABLE, because one pack spans both — a person who is
-- both a Customer and somebody who signs into the Staff platform gets ONE file,
-- and a coverage record split across two tables would have to be reassembled to
-- answer the one question it exists to answer. `act_kind` says which log the id
-- belongs to; it is not a discriminator standing in for a foreign key, because
-- there is no foreign key here at all.
--
-- AND DELIBERATELY NO FOREIGN KEY. Every other reference to `consent_records`
-- on this platform is ON DELETE RESTRICT, so that a Customer deletion fails
-- loudly rather than silently orphaning evidence. This one must NOT be: the
-- record of a handover has to outlive the data it disclosed, or the platform
-- would be unable to prove it answered a request precisely in the case where
-- counsel later ordered the underlying rows erased. A dangling id here is the
-- correct state, and it reads as "these acts were sent, and have since gone".
CREATE TABLE IF NOT EXISTS consent_evidence_pack_acts (
    pack_id UUID NOT NULL REFERENCES consent_evidence_packs(id) ON DELETE CASCADE,

    -- `consent_record` for a row of `consent_records`, `staff_terms_acceptance`
    -- for a row of `staff_terms_acceptances`. An open CHECK in the house style:
    -- a third population would be a code change landing beside its own
    -- migration, not a string somebody can invent at runtime.
    act_kind TEXT NOT NULL CHECK (act_kind IN ('consent_record', 'staff_terms_acceptance')),
    act_id UUID NOT NULL,

    PRIMARY KEY (pack_id, act_kind, act_id)
);

-- Which packs covered a given act — the question asked from the other end:
-- "this consent was later disputed; what did we disclose about it, and when".
CREATE INDEX IF NOT EXISTS idx_consent_evidence_pack_acts_act
    ON consent_evidence_pack_acts (act_kind, act_id);
