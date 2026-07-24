-- OTP challenges gain a purpose scope (staff | customer).
--
-- Issuing records the purpose and verifying requires an exact match, so a
-- passcode minted for the Storefront cannot be redeemed for a Staff Session.
-- Rate-limit counters are scoped per purpose too, so one surface's traffic
-- cannot exhaust the other's allowance. See ADR 0010.
--
-- Every challenge that exists today was minted for staff sign-in, so the
-- default backfills them to 'staff'; the default is then dropped so every
-- future insert must state its purpose explicitly.

ALTER TABLE otp_challenges ADD COLUMN purpose TEXT NOT NULL DEFAULT 'staff';
ALTER TABLE otp_challenges ALTER COLUMN purpose DROP DEFAULT;

ALTER TABLE otp_challenges
    ADD CONSTRAINT otp_challenges_purpose_check CHECK (purpose IN ('staff', 'customer'));

CREATE INDEX otp_challenges_purpose_email_created_at_idx
    ON otp_challenges (purpose, email, created_at);
CREATE INDEX otp_challenges_purpose_request_ip_created_at_idx
    ON otp_challenges (purpose, request_ip, created_at);

DROP INDEX otp_challenges_email_created_at_idx;
DROP INDEX otp_challenges_request_ip_created_at_idx;
