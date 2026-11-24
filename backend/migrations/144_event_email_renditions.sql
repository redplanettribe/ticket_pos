-- Email renditions (spec #870, #876): each Event's imagery as mail can carry
-- it, produced on the server from the originals, so no mail ever ships a WebP
-- or a multi-megabyte original.
--
--   email_wide_key   the Cover Image as a JPEG 1200px wide
--   email_square_key a 128px square JPEG from the Thumbnail, or from the Cover
--                    Image cropped to its centre when there is no Thumbnail
--
-- Two nullable object keys beside cover_image_key and thumbnail_key, shaped
-- like them: the platform stores where the rendition lives and computes the
-- public URL at read time. Keys live under the `email-wide/` and
-- `email-square/` bucket prefixes, disjoint from `covers/`, `thumbnails/` and
-- `videos/`, so a key minted for one slot never validates for another
-- (ADR 0020).
--
-- NULL means "not rendered yet", and mail omits the image. Every existing row
-- starts NULL: the rendition fill (POST /api/v1/internal/email-renditions/fill)
-- renders the existing catalogue once after deploy, and any original it
-- cannot render stays NULL for the next run.
--
-- Forward-only, like every migration here: there are no down migrations.
ALTER TABLE events
    ADD COLUMN email_wide_key TEXT,
    ADD COLUMN email_square_key TEXT;
