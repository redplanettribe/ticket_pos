-- Dev seed: a Thumbnail on one seeded Event, so a fresh dev stack's Timeline
-- shows both sides of the card image rule (spec #853): Midnight Synth Live in
-- its Thumbnail, and Sunrise Jazz Brunch, which has a Cover Image and no
-- Thumbnail (migration 034), in its Cover Image cropped to the centre.
--
-- Midnight Synth Live and not Sunrise Jazz Brunch: putting the Thumbnail on
-- the Event that already has a Cover Image would leave no seeded Event showing
-- the fallback. Nothing the checkout E2E journey reads changes: the Thumbnail
-- is shown only in a square card frame, never on the Event page.
--
-- Like migration 034, the key is a placeholder and no object is created
-- behind it, so its URL 404s until an Org Admin uploads a real Thumbnail
-- through Staff. The key shape matches what the Thumbnail upload-URL endpoint
-- mints, thumbnails/{organizationID}/{eventID}/{uuid}.jpg, because the attach
-- path validates the prefix and the ownership within it (ADR 0020).
--
-- Fixed UUIDs and a WHERE on the seeded id keep it idempotent and a no-op on
-- any database without the demo Organization.
UPDATE events
SET thumbnail_key = 'thumbnails/a0000000-0000-4000-8000-000000000001/c0000000-0000-4000-8000-000000000001/e0000000-0000-4000-8000-000000000003.jpg'
WHERE id = 'c0000000-0000-4000-8000-000000000001';
