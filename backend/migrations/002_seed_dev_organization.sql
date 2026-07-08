INSERT INTO organizations (id, name, slug, created_at)
VALUES (
    'a0000000-0000-4000-8000-000000000001',
    'Demo Venue',
    'demo-venue',
    NOW()
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO members (id, organization_id, email, role, created_at)
VALUES (
    'b0000000-0000-4000-8000-000000000001',
    'a0000000-0000-4000-8000-000000000001',
    'preseeded@example.com',
    'org_admin',
    NOW()
)
ON CONFLICT (id) DO NOTHING;
