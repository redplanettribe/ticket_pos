-- Index supporting the global outbound OTP ceiling.
--
-- The ceiling caps passcode emails sent platform-wide per window, across both
-- purposes, to protect the reputation of the single verified sending subdomain
-- (ADR 0009, PRD decision 12). Its counter is this table: one row is already
-- written per passcode issued, so the count is durable and shared by every
-- Cloud Run instance — an in-process tally would let N instances each send a
-- full ceiling's worth.
--
-- No new table and no new counter: the only thing missing is an index. The
-- existing indexes are all purpose-leading, and this query deliberately spans
-- purposes, so none of them serve it.

CREATE INDEX otp_challenges_created_at_idx ON otp_challenges (created_at);
