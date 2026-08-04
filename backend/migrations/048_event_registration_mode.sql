-- External Registration: an Event either sells Ticket Types on this platform or
-- carries a Registration Link and sends its audience elsewhere to sign up, never
-- both (ADR 0028).
--
-- The mode is an explicit column rather than being derived from
-- registration_url IS NOT NULL. The two are genuinely different facts — the mode
-- is what the organizer chose, the URL is what they typed — and the difference is
-- what makes the draft state expressible: an Event that has decided on External
-- Registration but has no URL yet is a legitimate incomplete draft, exactly as a
-- draft with no starts_at is today.
--
-- 'tickets' is the default, so every existing Event keeps selling tickets and
-- adopting this feature is a choice rather than a migration.
--
-- The exclusivity invariant itself is NOT expressible here: it spans events and
-- ticket_types, so no CHECK constraint can hold it. It is enforced in the catalog
-- service on both sides — Ticket Type creation is refused on an external Event,
-- and switching an Event to 'external' is refused while any Ticket Type exists.
ALTER TABLE events
    ADD COLUMN registration_mode TEXT NOT NULL DEFAULT 'tickets'
        CHECK (registration_mode IN ('tickets', 'external')),
    -- The Registration Link. Nullable because the mode may be chosen before the
    -- registration page exists. Constrained to https in the service layer, which
    -- is what keeps javascript: and data: out of a value destined for both an
    -- href and a Location header.
    ADD COLUMN registration_url TEXT,
    -- A raw counter of hand-offs to the registration site, mirroring
    -- affiliate_links.click_count: no dedup, no visitor identification. It counts
    -- clicks, never registrations — the platform loses sight of the buyer at the
    -- link and never learns whether they signed up.
    ADD COLUMN registration_click_count BIGINT NOT NULL DEFAULT 0;
