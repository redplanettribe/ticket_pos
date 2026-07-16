ALTER TABLE events
    ADD COLUMN discoverable BOOLEAN NOT NULL DEFAULT FALSE;

-- Discoverable, published, upcoming Events drive the public Storefront listings
-- (the Organization page and the global explorer). Index the common filter.
CREATE INDEX events_discoverable_idx
    ON events (starts_at)
    WHERE status = 'published' AND discoverable = TRUE;
