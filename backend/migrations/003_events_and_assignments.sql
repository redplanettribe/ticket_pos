CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, slug)
);

CREATE INDEX events_organization_id_idx ON events (organization_id);

CREATE TABLE event_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id UUID NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    event_id UUID NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    role member_role NOT NULL CHECK (role IN ('event_owner', 'event_staff')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (member_id, event_id)
);

CREATE INDEX event_assignments_event_id_idx ON event_assignments (event_id);
