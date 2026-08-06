-- A Spanish display name on the shared Tag pool, for Preset Tags only (#216,
-- parent #215).
--
-- This is ADR 0030's narrow amendment to ADR 0027 and NOT a reversal of it.
-- Preset Tag copy still lives in apps/storefront/messages/{en,es}.json, keyed on
-- canonical_key, and every Storefront page still words its own chips and badges
-- from there. Nothing on this side reaches a page. What the amendment buys is
-- the one reader a page cannot serve: the Follow Digest is mail, mail has no
-- address to carry a Locale, and the backend composing it could not name
-- "Musica". Thirteen strings are duplicated in one direction; the two copies of
-- Spanish now have to be edited together, and ADR 0027's consequence about a
-- seeded English name being inert on the page is unchanged.
--
-- CURATED ROWS ONLY, and the CHECK constraint is what makes that a rule rather
-- than a habit. A Custom Tag is an Organization's own word, coined mid-edit in
-- one language with no second field to fill and no reviewer between them and
-- the reader — it is rendered as typed in every Locale, which ADR 0027 already
-- holds correct and which SetEventTags relies on when it coins a row without
-- ever naming this column. The constraint also states the direction of ADR
-- 0004's promotion path: flipping curated to TRUE is permitted with no Spanish
-- at all (the resolution in Go falls back to English, exactly as the Storefront
-- catalogue does), while demoting a Preset Tag has to drop the Spanish in the
-- same statement, because the moment the row is a Custom Tag its name is the
-- Organization's and not ours to have translated.
--
-- NULLABLE rather than NOT NULL DEFAULT display_name. A row with no Spanish is
-- a fact worth being able to see: it is the promoted-by-UPDATE case, and
-- copying the English into it would hide a missing translation behind a value
-- that looks deliberate. The fallback belongs in one place, in Go, where it can
-- be stated once and tested.
--
-- No index. Nothing is looked up by a Spanish name and nothing is planned to —
-- canonical_key remains the only way a Tag is addressed.
ALTER TABLE tags ADD COLUMN display_name_es TEXT;

ALTER TABLE tags ADD CONSTRAINT tags_spanish_name_curated_only
    CHECK (display_name_es IS NULL OR (curated AND display_name_es <> ''));

-- The Spanish for every Preset Tag seeded so far: the twelve from migration 009
-- and the one from 047. The words are exactly the Storefront catalogue's
-- (apps/storefront/messages/es.json, `tags` namespace), because a Customer who
-- reads "Arte y teatro" on the explorer must not be sent mail calling the same
-- Tag something else.
--
-- Guarded on curated so a canonical key that reached the pool as a Custom Tag
-- first — 009 and 047 both insert ON CONFLICT DO NOTHING for precisely that
-- reason — is left untranslated rather than quietly given our words for an
-- Organization's Tag.
UPDATE tags t
SET display_name_es = v.display_name_es
FROM (VALUES
    ('music', 'Música'),
    ('nightlife', 'Vida nocturna'),
    ('arts & theatre', 'Arte y teatro'),
    ('comedy', 'Comedia'),
    ('food & drink', 'Comida y bebida'),
    ('festival', 'Festival'),
    ('sports', 'Deportes'),
    ('workshop', 'Talleres'),
    ('conference', 'Conferencias'),
    ('community', 'Comunidad'),
    ('family', 'Familia'),
    ('film', 'Cine'),
    ('technology', 'Tecnología')
) AS v (canonical_key, display_name_es)
WHERE t.canonical_key = v.canonical_key
  AND t.curated = TRUE;
