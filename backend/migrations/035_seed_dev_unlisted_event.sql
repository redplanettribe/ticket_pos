-- Dev seed: one published, NOT Discoverable Event for the demo Organization,
-- with a Ticket Type so it is sellable. Fixed UUIDs and ON CONFLICT DO NOTHING,
-- exactly like 008_seed_dev_events, which seeds the two Discoverable ones.
--
-- It exists for the pair of properties nothing else can assert together
-- (e2e/tests/seo.spec.ts): the page still renders and still sells — ADR 0002's
-- rule that Discoverable is not reachability — while telling a crawler not to
-- index it. Both halves need a published Event whose `discoverable` is FALSE,
-- and until this seed there was none: a draft Event 404s on the public read, so
-- it cannot stand in, and an E2E suite that created its own would be one
-- interrupted run away from leaving an unlisted Event in a shared dev database.
--
-- A third Event and not a flip of an existing one: the other two are read by the
-- checkout journey and the hero specs, and a test about crawlers must not be
-- able to break a test about selling. It is also why this is a new migration
-- rather than an edit to 008 — that one has already been applied everywhere and
-- would not re-run.
--
-- The name says out loud what the flag means, because this row is what a person
-- poking at the dev Storefront's listings will fail to find.

INSERT INTO events (
    id, organization_id, name, slug, status,
    starts_at, timezone, venue_name, venue_address, description, discoverable, created_at
)
VALUES (
    'c0000000-0000-4000-8000-000000000003',
    'a0000000-0000-4000-8000-000000000001',
    'Unlisted Loft Session',
    'unlisted-loft-session',
    'published',
    NOW() + INTERVAL '30 days',
    'America/New_York',
    'The Loft',
    '77 Greenpoint Ave, Brooklyn, NY',
    'A quiet room, forty chairs, and whoever was sent the link.',
    FALSE,
    NOW()
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO ticket_types (
    id, event_id, organization_id, name, description,
    price_cents, capacity, sold_count, sort_order, created_at, updated_at
)
VALUES (
    'd0000000-0000-4000-8000-000000000004',
    'c0000000-0000-4000-8000-000000000003',
    'a0000000-0000-4000-8000-000000000001',
    'Loft Entry', 'One seat, one drink, no guest list.',
    5000, 40, 0, 0, NOW(), NOW()
)
ON CONFLICT (id) DO NOTHING;
