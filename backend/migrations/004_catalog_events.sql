ALTER TABLE events
    ADD COLUMN status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'cancelled')),
    ADD COLUMN starts_at TIMESTAMPTZ,
    ADD COLUMN ends_at TIMESTAMPTZ,
    ADD COLUMN timezone TEXT,
    ADD COLUMN venue_name TEXT,
    ADD COLUMN venue_address TEXT,
    ADD COLUMN description TEXT,
    ADD COLUMN cover_image_key TEXT;
