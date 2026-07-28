-- A checkout whose cart totals zero is settled by the platform itself, with no
-- Payment Provider involved (ADR 0017): a cart of Free Ticket Types creates a
-- Payment that is approved on the spot and commits its Ticket Sale through the
-- same spine an approved PayPhone Payment uses. That sale needs a Payment Method
-- naming how it was settled, so the value set gains 'free' — nothing was
-- collected, by anyone.
--
-- Only the value-set constraint changes. ticket_sales_payment_method_ck, which
-- requires a Payment Method on channel = 'online' or source = 'direct' sales and
-- forbids it elsewhere, is left untouched: a free claim is an Online Sale, so it
-- already satisfies that rule.
ALTER TABLE ticket_sales DROP CONSTRAINT ticket_sales_payment_method_check;
ALTER TABLE ticket_sales ADD CONSTRAINT ticket_sales_payment_method_check
    CHECK (payment_method IN ('cash', 'transfer', 'payphone', 'free'));
