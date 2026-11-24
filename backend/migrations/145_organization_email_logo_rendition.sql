-- The Logo email rendition (spec #870, #877): an Organization's Logo as mail
-- can carry it, produced on the server from the original, so no mail ever
-- ships a WebP or a multi-megabyte Logo.
--
--   email_logo_key  the Logo as a PNG fitted within 96px square, its
--                   transparency kept
--
-- A nullable object key beside logo_image_key, shaped like it: the platform
-- stores where the rendition lives and computes the public URL at read time.
-- Keys live under the `email-logo/` bucket prefix, disjoint from `logos/`, so a
-- key minted for one slot never validates for another (ADR 0020).
--
-- NULL means "not rendered yet", and mail omits the Logo. Every existing row
-- starts NULL: the rendition fill (POST /api/v1/internal/email-renditions/fill)
-- renders the existing Logos once after deploy, beside the Event renditions,
-- and any Logo it cannot render stays NULL for the next run.
--
-- Forward-only, like every migration here: there are no down migrations.
ALTER TABLE organizations
    ADD COLUMN email_logo_key TEXT;
