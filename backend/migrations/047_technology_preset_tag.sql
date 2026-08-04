-- Technology joins the Preset Tags seeded by migration 009.
--
-- Seeded here rather than by flipping `curated` on an existing Custom Tag,
-- which ADR 0004 also allows: this way the Tag arrives in a commit, and that
-- commit is what carries its Spanish (apps/storefront/messages/es.json) and
-- updates the fixture in apps/storefront/lib/messages.test.ts. A production
-- UPDATE would put the chip in front of a Spanish-reading visitor in English,
-- which the fallback in lib/tag-name.ts survives but nobody chose.
--
-- ON CONFLICT DO NOTHING, exactly as 009 does, because 'technology' may already
-- exist in the pool as a Custom Tag an Organization coined — the canonical_key
-- is unique across both tiers. Where it does, this migration deliberately
-- leaves it alone rather than promoting it: silently reclassifying a Tag an
-- Organization owns would change how it renders in every Locale (a Custom Tag
-- is shown as typed, a Preset Tag is translated). Promoting it is the separate,
-- deliberate UPDATE that ADR 0004 describes.
INSERT INTO tags (canonical_key, display_name, curated) VALUES
    ('technology', 'Technology', TRUE)
ON CONFLICT (canonical_key) DO NOTHING;
