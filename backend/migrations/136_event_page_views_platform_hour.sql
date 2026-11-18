-- Platform Trends reads Page Views for the whole platform over a run of hours
-- (#813, spec #808): the whole-page bucket of every Event, summed per platform
-- day. The one index the table has is the bucket key, (event_id,
-- affiliate_link_id, hour), which leads on the Event and so serves one Event's
-- graph and nothing platform-wide.
--
-- PARTIAL ON THE WHOLE-PAGE ROWS, because that read counts nothing else: a
-- load that arrived through an Affiliate Link is already in its Event's null
-- bucket, and the link's own row is that same load stored again as a Click.
-- Leaving the link rows out of the index is what keeps a Click from being
-- counted twice, said once more in the shape of the index itself.
--
-- view_count is INCLUDEd so the range is answered from the index alone.
CREATE INDEX event_page_views_whole_page_hour
    ON event_page_views (hour) INCLUDE (view_count)
    WHERE affiliate_link_id IS NULL;
