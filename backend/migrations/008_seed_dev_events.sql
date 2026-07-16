-- Dev seed: published, discoverable Events with Ticket Types for the demo
-- Organization so the Storefront has data to render locally. Uses fixed UUIDs
-- and ON CONFLICT DO NOTHING to stay idempotent, mirroring 002_seed_dev_organization.

INSERT INTO events (
    id, organization_id, name, slug, status,
    starts_at, timezone, venue_name, venue_address, description, discoverable, created_at
)
VALUES
    (
        'c0000000-0000-4000-8000-000000000001',
        'a0000000-0000-4000-8000-000000000001',
        'Midnight Synth Live',
        'midnight-synth-live',
        'published',
        NOW() + INTERVAL '21 days',
        'America/New_York',
        'The Broadway Hall',
        '123 Broadway, New York, NY',
        'An electric night of analog synths and neon lights.',
        TRUE,
        NOW()
    ),
    (
        'c0000000-0000-4000-8000-000000000002',
        'a0000000-0000-4000-8000-000000000001',
        'Sunrise Jazz Brunch',
        'sunrise-jazz-brunch',
        'published',
        NOW() + INTERVAL '40 days',
        'America/New_York',
        'Riverside Terrace',
        '9 Riverside Dr, New York, NY',
        'Live jazz, bottomless coffee, and a river view.',
        TRUE,
        NOW()
    )
ON CONFLICT (id) DO NOTHING;

INSERT INTO ticket_types (
    id, event_id, organization_id, name, description,
    price_cents, capacity, sold_count, sort_order, created_at, updated_at
)
VALUES
    (
        'd0000000-0000-4000-8000-000000000001',
        'c0000000-0000-4000-8000-000000000001',
        'a0000000-0000-4000-8000-000000000001',
        'General Admission', 'Standing room on the main floor.',
        3500, 200, 0, 0, NOW(), NOW()
    ),
    (
        'd0000000-0000-4000-8000-000000000002',
        'c0000000-0000-4000-8000-000000000001',
        'a0000000-0000-4000-8000-000000000001',
        'VIP Balcony', 'Reserved balcony seating with a dedicated bar.',
        8500, 40, 0, 1, NOW(), NOW()
    ),
    (
        'd0000000-0000-4000-8000-000000000003',
        'c0000000-0000-4000-8000-000000000002',
        'a0000000-0000-4000-8000-000000000001',
        'Brunch Seat', 'A table for the full brunch service.',
        4500, 80, 0, 0, NOW(), NOW()
    )
ON CONFLICT (id) DO NOTHING;
