CREATE TYPE member_role AS ENUM ('org_admin', 'event_owner', 'event_staff');

CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id UUID NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email TEXT NOT NULL,
    role member_role NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (organization_id, email)
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    active_member_id UUID REFERENCES members (id) ON DELETE SET NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX sessions_email_idx ON sessions (email);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE otp_challenges (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    request_ip TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    invalidated BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX otp_challenges_email_created_at_idx ON otp_challenges (email, created_at);
CREATE INDEX otp_challenges_request_ip_created_at_idx ON otp_challenges (request_ip, created_at);
