-- The passcode-only Consent Withdrawal gets a channel of its own (#270, parent
-- #265).
--
-- #266 widened the vocabulary once, to `operator_request`, and #270 landed
-- beside it recording its withdrawals as `signin` — the door the act genuinely
-- came through — with a comment explaining how to tell them apart:
-- `channel = 'signin' AND policy_acceptance IS NULL AND session_id IS NULL`.
-- That discriminator is correct and this migration does not contradict it. It
-- replaces it, for one reason: the parent spec requires a compliance officer to
-- be able to say which surface a withdrawal came from — the account page, an
-- unsubscribe link, a passcode-only surface, or an Operator — and a surface
-- identified by a three-clause predicate over the absence of two other columns
-- is a surface nobody will find. The channel column exists to answer exactly
-- that question, and answering it by inference is answering it in the one place
-- an auditor is least likely to look.
--
-- It also stops being true the moment somebody changes something else. The
-- predicate rests on a sign-in ALWAYS recording a Policy Acceptance and a
-- session id, which is true today and is not a property anybody has promised to
-- preserve; the day a sign-in surface records a capture without minting a
-- session, every passcode withdrawal in the log retroactively becomes
-- indistinguishable from it. A stored string cannot rot that way.
--
-- WHY A SEVENTH STRING AND NOT A COLUMN SAYING "this was a withdrawal". Because
-- a withdrawal is not a kind of act — it is a capture act whose answers are
-- false (ADR 0038), and the platform deliberately has no second write path and
-- no revocation table. What differs between the surfaces here is the SURFACE,
-- which is what `channel` already names for the other six.
ALTER TABLE consent_records DROP CONSTRAINT consent_records_channel_check;

ALTER TABLE consent_records ADD CONSTRAINT consent_records_channel_check
    CHECK (channel IN ('signin', 'checkout', 'account_settings', 'unsubscribe_link',
                       'email_confirmation', 'operator_request', 'passcode_withdrawal'));

-- NO BACKFILL, and the absence is the point. Rows already recorded as `signin`
-- were recorded by a platform in which this string did not exist, and rewriting
-- them would be editing an append-only evidence log to say something other than
-- what it said at the time — the one thing migration 061 forbids outright. Any
-- such rows are identified by the predicate above, which remains true of them
-- forever precisely because nothing after this migration writes `signin` for a
-- withdrawal.
--
-- In practice there are none: #270 has not been deployed. The rule is stated
-- anyway, because the next person to widen this vocabulary will be tempted.
