-- Tags are discovery facets an Event can wear. They live in one system-wide
-- shared pool reused across all Organizations. curated = TRUE marks a Preset Tag
-- (seeded, offered as a filter chip in the global explorer); curated = FALSE is a
-- Custom Tag coined by an Organization when no existing Tag fits.
CREATE TABLE tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_key TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    curated BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- event_tags links Events to Tags (many-to-many).
CREATE TABLE event_tags (
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_id, tag_id)
);

-- Reverse lookup: Events carrying a given Tag drives explorer tag filtering.
CREATE INDEX event_tags_tag_id_idx ON event_tags (tag_id);

-- Seed the 12 curated Preset Tags. canonical_key is the lowercased display name.
INSERT INTO tags (canonical_key, display_name, curated) VALUES
    ('music', 'Music', TRUE),
    ('nightlife', 'Nightlife', TRUE),
    ('arts & theatre', 'Arts & Theatre', TRUE),
    ('comedy', 'Comedy', TRUE),
    ('food & drink', 'Food & Drink', TRUE),
    ('festival', 'Festival', TRUE),
    ('sports', 'Sports', TRUE),
    ('workshop', 'Workshop', TRUE),
    ('conference', 'Conference', TRUE),
    ('community', 'Community', TRUE),
    ('family', 'Family', TRUE),
    ('film', 'Film', TRUE)
ON CONFLICT (canonical_key) DO NOTHING;
