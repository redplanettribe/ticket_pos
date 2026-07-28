-- Dev seed: hero media keys on one seeded Event, so the Storefront's Cover
-- Video hero has something to render locally and in the E2E suite.
--
-- The keys are placeholders and the objects behind them are deliberately not
-- created: nothing here uploads bytes to the bucket, so both URLs 404 until an
-- Org Admin uploads real media through Staff. That is enough for what depends
-- on this seed — the hero's *decisions* (is a <video> element created at all,
-- with which src and poster, and never on a listing card) are attribute-level
-- and provable without a byte of MP4. Real playback, buffering and the fade are
-- verified by hand against a real upload; see
-- docs/manual-verification-cover-video.md.
--
-- Sunrise Jazz Brunch and not Midnight Synth Live: the checkout E2E journey
-- buys from the latter, and a spec about the hero must not be able to break a
-- spec about selling.
--
-- Key shapes match what the presigned-upload endpoints mint —
-- covers/{organizationID}/{eventID}/{uuid}.jpg and
-- videos/{organizationID}/{eventID}/{uuid}.mp4 — because the attach path
-- validates the prefix and the ownership within it (ADR 0020).
UPDATE events
SET cover_image_key = 'covers/a0000000-0000-4000-8000-000000000001/c0000000-0000-4000-8000-000000000002/e0000000-0000-4000-8000-000000000001.jpg',
    cover_video_key = 'videos/a0000000-0000-4000-8000-000000000001/c0000000-0000-4000-8000-000000000002/e0000000-0000-4000-8000-000000000002.mp4'
WHERE id = 'c0000000-0000-4000-8000-000000000002';
