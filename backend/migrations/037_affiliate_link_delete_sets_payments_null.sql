-- Deleting an Affiliate Link that never drove a sale must not be blocked by the
-- checkouts nobody finished.
--
-- A link is deletable only while it has no clicks, no Ticket Sale of any status,
-- and no PENDING Payment — a pending checkout may still be approved, and the
-- delete must not race the sale it would produce. What is left pointing at the
-- link afterwards is only Payments that ended without one: abandoned (expired)
-- and declined (failed) checkouts, which are not history an organizer would keep
-- a marketing link for.
--
-- Those Payments outlive the link and stop naming it. SET NULL rather than
-- CASCADE because the Payment is the record of a real attempt to buy and is not
-- the link's to delete; and rather than RESTRICT because that is exactly the
-- refusal being lifted. The one case it covers concretely: an expired Payment
-- can still confirm into a sale when the provider approved it late (ADR 0013),
-- and that sale is then recorded unattributed rather than carrying an id that no
-- longer exists.
--
-- ticket_sales.affiliate_link_id keeps its default (RESTRICT) FK: an actual
-- sale's attribution is never rewritten, and the delete's own WHERE clause
-- guarantees no such row exists.
ALTER TABLE payments DROP CONSTRAINT payments_affiliate_link_id_fkey;

ALTER TABLE payments
    ADD CONSTRAINT payments_affiliate_link_id_fkey
    FOREIGN KEY (affiliate_link_id) REFERENCES affiliate_links (id) ON DELETE SET NULL;
