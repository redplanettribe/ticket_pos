-- "Due" must mean "needs work" (#162, ADR 0024).
--
-- Migration 039 gave a Reversal Request a retry schedule and nothing that says
-- the reversal finished LOCALLY. The provider's answer lands in `status`, and
-- `succeeded` is written before the Ticket Sale is voided precisely so the
-- answer survives a local write that fails — which leaves the one fact the
-- Reversal Reconciler's queue turns on unrecorded: whether the sale ever
-- followed the money. The queue had to ask `ticket_sales` instead, over every
-- succeeded row ever written, forever.
--
-- 039's own comment named this column as the thing to add. It is added rather
-- than 039 amended, because 039 is applied.
ALTER TABLE sale_reversals ADD COLUMN sale_voided_at TIMESTAMPTZ;

-- What it records is an OBSERVATION and not the reversal's own timestamp: the
-- instant a pursuit of this request last saw the Ticket Sale voided, whoever
-- voided it. `ticket_sales.reversed_at` stays the moment the sale was reversed
-- and stays the only answer to "when" (migration 031).
--
-- Whoever voided it, because what the queue asks is whether any local work is
-- left, and there is none once the sale is reversed — by this platform
-- finishing its own commit, or by a Platform Operator recording an Operator
-- Reversal out of band (ADR 0019). Both leave nothing to repair.
COMMENT ON COLUMN sale_reversals.sale_voided_at IS
    'When a pursuit of this Reversal Request last observed the Ticket Sale voided, whoever voided it. NULL on a succeeded request is the SALE_REVERSAL_NOT_COMMITTED incident: the money went back and the sale never followed it.';

-- The backfill is not optional. Every reversal that has ever completed carries
-- a succeeded row, and without this they are all born due — which is the bug
-- this migration is here to end rather than to introduce on the next tick.
--
-- The instant used is the sale's own reversed_at, which is the truest value
-- available for a reversal that landed before this column existed. A succeeded
-- row over a sale that is still active is left NULL on purpose: that is the
-- broken one, and it is exactly what the Reconciler must still be handed.
UPDATE sale_reversals sr
SET sale_voided_at = ts.reversed_at
FROM ticket_sales ts
WHERE ts.id = sr.ticket_sale_id
  AND sr.status = 'succeeded'
  AND ts.status = 'reversed';

-- The index the claim query needs, and the reason this column exists at all.
-- Partial on the incident itself, so the succeeded arm of the queue is the size
-- of what is broken rather than of the history: the cost argument is written
-- down once, at repository.ClaimDueReversalRequest.
CREATE INDEX sale_reversals_uncommitted_idx
    ON sale_reversals (next_attempt_at)
    WHERE status = 'succeeded' AND sale_voided_at IS NULL;
