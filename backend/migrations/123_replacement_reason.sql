-- Why one Ticket Sale stands in the place of another (#650, parent #645,
-- ADR 0074).
--
-- Migration 085 gave the platform ONE reason to link two Ticket Sales: a Sale
-- Correction (ADR 0050), where somebody recorded a sale wrongly and a
-- replacement was typed to stand in its place. The link alone said so, and that
-- was true while it was the only writer.
--
-- An UPGRADE is the second writer, and it is not a correction. The buyer's free
-- Ticket Sale is reversed inside the transaction that commits the paid one they
-- elected instead; the two point at each other through the very same pair. But
-- nothing was recorded wrongly and no member of staff erred — a buyer changed
-- their mind, and ADR 0074 refuses to let one word mean both, because reading
-- "corrected" off an Upgrade would tell an Organization its people made a
-- mistake on a Sale no human touched.
--
-- SO THE REASON IS STORED, AND THE LINK STOPS CARRYING IT ALONE. This is a
-- POSITIVE marker, deliberately, and the same argument origin.go makes about
-- DeriveSaleOrigin's three-way negative applies here in reverse: a third way to
-- replace a Ticket Sale must not be able to inherit "correction" by saying
-- nothing. The pairing constraint below is what stops it — a link without a
-- reason is refused by the database, so a future writer cannot forget.
ALTER TABLE ticket_sales ADD COLUMN replacement_reason TEXT;

-- Every existing linked pair is a Sale Correction: until this migration there
-- was no other way to write either column, and #351 has been the sole writer
-- since migration 085.
UPDATE ticket_sales
SET replacement_reason = 'correction'
WHERE replaced_by_sale_id IS NOT NULL OR replaces_sale_id IS NOT NULL;

-- `correction` is ADR 0050's word and keeps its meaning exactly; `upgrade` is
-- ADR 0074's and is written on BOTH halves of the pair — on the reversed free
-- sale beside replaced_by_sale_id, and on the paid sale beside
-- replaces_sale_id — so either row answers the question on its own, without a
-- join to the other.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_replacement_reason_known
    CHECK (replacement_reason IS NULL OR replacement_reason IN ('correction', 'upgrade'));

-- A reason exactly when there is a replacement to give one for. Both directions
-- are refused: a link with no reason (the forgetting this column exists to make
-- impossible) and a reason with no link (a claim about a replacement that is
-- not there).
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_replacement_reason_pair_ck
    CHECK (
        (replacement_reason IS NULL)
        = (replaced_by_sale_id IS NULL AND replaces_sale_id IS NULL)
    );
