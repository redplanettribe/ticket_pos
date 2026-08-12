-- Edition 1: the real Privacy Policy, replacing the placeholder (parent #249).
--
-- This is the content drop migration 060 was written in anticipation of, and it
-- follows the rule that migration states rather than breaking it: THE
-- PLACEHOLDER ROW IS NOT TOUCHED. Edition `0-placeholder` stays exactly as it
-- was inserted, with its own label, its own effective date and the fingerprint
-- of the text it was published under. An edition is a record of what was shown;
-- editing one to say something else is the single thing this table exists to
-- make impossible.
--
-- THIS ROW RE-GATES THE ENTIRE CUSTOMER BASE, which is the blast radius
-- migration 060 warns about, and here it is intended rather than tolerated.
-- Policy Acceptance is of a VERSION (migration 062): the moment this edition is
-- current, every Customer whose `policy_version_id` names the placeholder stops
-- matching, `Outstanding` reports the required box again, and the next sign-in
-- or checkout asks them to accept THIS text. Their standing Marketing and
-- Networking answers are not disturbed — the re-prompt shows the required box
-- alone — so this costs each Customer one tick and costs nobody their
-- preferences.
--
-- Nobody is grandfathered by it, and that is the point: an acceptance of
-- placeholder prose is not an acceptance of the legal text, and treating it as
-- one would be exactly the manufactured evidence the whole feature refuses to
-- produce.
--
-- The effective date is TODAY rather than a future date. A scheduled edition is
-- the right tool when legal review lands before the day the text is meant to
-- bind (see migration 060's note on the `effective_date <= CURRENT_DATE`
-- filter); here the text is already the operative one, and dating it forward
-- would leave the placeholder current in the meantime — which is to say, it
-- would keep showing people prose that says of itself that it is not legal
-- advice. It is written as a LITERAL and not as CURRENT_DATE, for migration
-- 060's reason: the day an edition became current is a fact about the edition,
-- and a date computed when the migration happens to run would give every
-- environment a different answer to a question an audit asks.
--
-- The hash is `policy.ContentHash()` over the embedded artifacts, and
-- backend/internal/consent/policy/seed_test.go recomputes it from THIS file: it
-- follows the current edition, so this row is now the one that must agree with
-- the text, and the placeholder's hash is left as the historical record of a
-- text this binary no longer serves.
INSERT INTO policy_versions (label, effective_date, content_hash)
VALUES (
    '1',
    DATE '2026-08-12',
    'deddc89049d1918ab8805f2a920f2f9ead6bfefba49cb018e7ba191a7386afc0'
);
