-- Affiliate Attribution: the tie between an Online Sale and the Affiliate Link
-- that drove it. The code the buyer's last click left behind is resolved once,
-- at begin-checkout, and snapshotted on the Payment; the sale-commit chokepoint
-- copies it onto the Ticket Sale, so a free zero-total checkout (ADR 0017)
-- carries attribution by the same path a paid one does.
--
-- Nullable on both, and unattributed is the ordinary case: a buyer who arrived
-- directly, a code that no longer lives, and every in-person or imported sale.
ALTER TABLE payments ADD COLUMN affiliate_link_id UUID REFERENCES affiliate_links (id);
ALTER TABLE ticket_sales ADD COLUMN affiliate_link_id UUID REFERENCES affiliate_links (id);

-- The staff table's figures: one Affiliate Link's attributed sales. Partial,
-- because an unattributed sale is the overwhelming majority and none of these
-- reads ever asks for one.
CREATE INDEX ticket_sales_affiliate_link_id_idx ON ticket_sales (affiliate_link_id)
WHERE affiliate_link_id IS NOT NULL;
