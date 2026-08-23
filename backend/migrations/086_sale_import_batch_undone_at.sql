-- When a Sale Import batch was undone (#352, ADR 0050).
--
-- The Sales Export names the ROUTE a Sale Reversal arrived by, and until now
-- every `reversed_by = 'staff'` sale had arrived by one route: the batch undo.
-- #350 and #351 added two more staff levers — reversing one imported sale on
-- its own, and correcting one by replacement — that write the same actor, so
-- the actor alone no longer says which lever was pulled. The correction is
-- told apart by its linkage (replaced_by_sale_id, migration 085). The plain
-- single-sale reversal and the batch undo are told apart here: a batch undo
-- reverses every sale it sweeps at one instant and records that instant on the
-- batch, so a reversed sale whose reversed_at is its batch's undone_at went by
-- the undo, and one reversed at any other moment went on its own — including a
-- sale reversed singly whose batch was undone afterwards.
--
-- Not a third value of ticket_sales.reversed_by: that column records the KIND
-- of actor (migration 031) and the actor is `staff` in all three cases. Not a
-- flag on the sale either: the batch is the thing that was undone, and when it
-- was is a fact about the batch that nothing had recorded.
ALTER TABLE sale_import_batches ADD COLUMN undone_at TIMESTAMPTZ;

-- A batch undone before this column existed was undone at the instant its
-- sales were reversed: the single-sale reversal did not exist before
-- migration 085 shipped, so every reversed sale in a reversed batch went by
-- the undo, and they all carry the same moment. A batch whose sales carry no
-- reversed_at at all (reversed before migration 031) stays null, as they do.
UPDATE sale_import_batches b
SET undone_at = (
    SELECT MAX(ts.reversed_at) FROM ticket_sales ts WHERE ts.import_batch_id = b.id
)
WHERE b.status = 'reversed';

ALTER TABLE sale_import_batches ADD CONSTRAINT sale_import_batches_undone_only_when_reversed
    CHECK (undone_at IS NULL OR status = 'reversed');
