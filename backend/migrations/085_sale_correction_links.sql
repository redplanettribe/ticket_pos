-- The two halves of a Sale Correction point at each other (#350, parent #349,
-- ADR 0050).
--
-- A Sale Correction is a single-sale staff Sale Reversal on the `import`
-- channel plus a replacement Ticket Sale recorded in the same transaction. The
-- Ticket Sale stays immutable: nothing is edited, two rows exist, and each says
-- which other row it belongs with. These two columns are that linkage.
--
--   replaced_by_sale_id  on the REVERSED sale: the replacement that corrected it.
--   replaces_sale_id     on the REPLACEMENT: the mistaken sale it stands in for.
--
-- THIS MIGRATION IS THE PREFACTOR FOR THE SERIES. #350 ships only the plain
-- reverse-one-sale half, which writes NEITHER column — a sale reversed on its
-- own has no replacement, and reads "Reversed by staff" rather than
-- "Corrected". The correction endpoint (#351) is what writes them, in one
-- transaction with the reversal. They land here so the Sales list and the
-- generated clients can carry the fields from the first ticket and #351 adds
-- writes rather than a schema change.
--
-- Nullable self-references with no ON DELETE clause: a Ticket Sale is never
-- deleted (a reversed one is kept, CONTEXT.md "Sale Reversal"), so the default
-- RESTRICT is the honest statement. Neither is a `reversed_by` value: the actor
-- is `staff` in both the plain and the correcting case, and the link is what
-- distinguishes them.
ALTER TABLE ticket_sales
    ADD COLUMN replaced_by_sale_id UUID REFERENCES ticket_sales (id),
    ADD COLUMN replaces_sale_id UUID REFERENCES ticket_sales (id);

-- One replacement per reversed sale, and one reversed sale per replacement. A
-- sale corrected twice is a replacement corrected once: the chain goes through
-- the replacement, never fans out from the original.
CREATE UNIQUE INDEX ticket_sales_replaced_by_sale_id_key
    ON ticket_sales (replaced_by_sale_id) WHERE replaced_by_sale_id IS NOT NULL;
CREATE UNIQUE INDEX ticket_sales_replaces_sale_id_key
    ON ticket_sales (replaces_sale_id) WHERE replaces_sale_id IS NOT NULL;

-- A sale cannot replace itself, and a sale that has been replaced is by
-- definition reversed: an active sale with a replacement would be two live
-- records of one transaction, the double-count the whole design exists to
-- avoid.
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_replacement_not_self
    CHECK (replaced_by_sale_id IS DISTINCT FROM id AND replaces_sale_id IS DISTINCT FROM id);
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_replaced_only_when_reversed
    CHECK (replaced_by_sale_id IS NULL OR status = 'reversed');
