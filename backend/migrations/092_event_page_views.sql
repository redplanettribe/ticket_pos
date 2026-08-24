-- Page Views of an Event's storefront page, counted into anonymous hourly
-- buckets (ADR 0057, #412). One row per (Event, Affiliate Link or none, UTC
-- hour) holding nothing but a count: growth is bounded by time × links, not by
-- traffic, and spamming the public endpoint that writes here can only make a
-- number wrong, never fill a disk.
--
-- WHAT A ROW DELIBERATELY DOES NOT RECORD is anybody. No visitor identity, no
-- IP, no user agent, no dedup — ADR 0022 rejected a table that grows with page
-- views partly because it meant inventing a visitor to store, and this table
-- reopens the storage without reopening the visitor. There is no personal data
-- here, nothing to purge, and no ADR 0045 dark-ship obligation. Counts are
-- loads, not people: refreshes, bots and prefetches all count, and every figure
-- read off this table is a floor, not a measurement.
--
-- THE NULL ROW IS THE PAGE ITSELF. affiliate_link_id IS NULL is the Event's
-- whole-page bucket, incremented on EVERY load; a load that arrived through a
-- live Affiliate Link increments that link's bucket AS WELL, because a Click is
-- a Page View that carried a live link (ADR 0057). The gap between the null
-- bucket and a link's bucket is organic traffic, derived, never stored.
--
-- HOURLY IS THE FLOOR FOREVER: the hour column is the truncated UTC hour of the
-- load, and minute-level trends, uniques and sessions are permanently out of
-- reach of this shape — reopening any of them means reopening ADR 0057.
--
-- affiliate_links.click_count REMAINS AUTHORITATIVE for the headline number and
-- the delete-only-without-history rule; a link's bucket is written alongside it
-- (dual-write), begins at this migration, and will never sum to a pre-existing
-- counter. Buckets are a graph, not a ledger of the counter.
CREATE TABLE event_page_views (
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,

    -- NULL is the Event's own whole-page bucket. ON DELETE CASCADE because a
    -- deletable link is by definition one with zero clicks (see
    -- DeleteIfNoHistory), so the buckets that go with it were never a link's
    -- real history — and buckets must never be what stops a delete.
    affiliate_link_id UUID REFERENCES affiliate_links (id) ON DELETE CASCADE,

    -- The truncated UTC hour the loads landed in. UTC in storage; the staff
    -- graph renders it in the Event's own timezone at read time.
    hour TIMESTAMPTZ NOT NULL,

    view_count BIGINT NOT NULL DEFAULT 0,

    -- One bucket per (Event, link-or-none, hour): the upsert target. NULLS NOT
    -- DISTINCT so the Event's null bucket is one row per hour too, not a new
    -- row per view.
    CONSTRAINT event_page_views_bucket_key
        UNIQUE NULLS NOT DISTINCT (event_id, affiliate_link_id, hour)
);
