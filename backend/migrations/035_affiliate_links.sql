-- Affiliate Links: named, trackable links to an Event's page, created by an Org
-- Admin or Event Owner to attribute Online Sales to whoever is promoting the
-- Event. The link itself is the whole concept — there is no affiliate person,
-- no commission, and no payout; the stats it will grow are informational only.
CREATE TABLE affiliate_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    -- Denormalized from the Event so an Organization's links can be read without
    -- a join, exactly as payments and ticket_sales carry it.
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    -- The editable display name the organizer recognizes ("María's Instagram").
    name TEXT NOT NULL,
    -- The immutable, system-generated code that travels in the Event page URL as
    -- ?ref=CODE. Unique per Event, which is the scope attribution resolves in.
    code TEXT NOT NULL,
    -- A deactivated link stops attributing but keeps its history.
    active BOOLEAN NOT NULL DEFAULT TRUE,
    -- Clicks on this link. Created now so the column exists from the first
    -- migration; nothing counts into it until Affiliate Attribution lands.
    click_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Codes never collide within an Event: the uniqueness attribution depends on.
CREATE UNIQUE INDEX affiliate_links_event_id_code_key ON affiliate_links (event_id, code);
-- The section's list: one Event's links, newest first.
CREATE INDEX affiliate_links_event_id_created_at_idx ON affiliate_links (event_id, created_at DESC);
