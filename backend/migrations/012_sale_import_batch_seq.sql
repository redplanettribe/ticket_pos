-- Give Sale Import batches a monotonic insertion order so "the latest batch on
-- an Event" is deterministic. Previously the latest-batch guard and the history
-- list ordered by (created_at DESC, id DESC); when two batches share a created_at
-- (identical clock tick, or a fixed test clock) the tiebreak fell to a random
-- gen_random_uuid() id, making "latest" nondeterministic.
--
-- seq is a strictly increasing identity assigned at insert; ordering by it is
-- total and stable regardless of created_at ties.

ALTER TABLE sale_import_batches
    ADD COLUMN seq BIGINT GENERATED ALWAYS AS IDENTITY;

CREATE INDEX sale_import_batches_event_seq_idx
    ON sale_import_batches (event_id, seq DESC);
