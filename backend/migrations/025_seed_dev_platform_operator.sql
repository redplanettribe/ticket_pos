-- Dev seed: puts the demo Organization's Org Admin (002_seed_dev_organization)
-- on the platform operator allowlist, so signing in locally with that email
-- reaches the Operator Dashboard as well as the staff app. Idempotent, and
-- skipped entirely outside development like every other _seed_dev_ migration —
-- a production allowlist starts empty and is filled by direct database write
-- (ADR 0015).

INSERT INTO platform_operators (id, email, created_at)
VALUES (
    'd0000000-0000-4000-8000-000000000001',
    'preseeded@example.com',
    NOW()
)
ON CONFLICT (email) DO NOTHING;
