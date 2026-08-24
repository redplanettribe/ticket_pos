-- The Ticket Questions already in production are grandfathered (#405, ADR 0056).
--
-- ADR 0056 considered starting them as drafts and the user overruled it: the one
-- real Event's six questions are known and safe, and nothing collects before
-- the ADR 0045 flag flips either way. So this migration APPROVES every question
-- and Option that exists when it runs, and signs the approval with its own
-- name, because approval is a record of a review and never a column default
-- (089). A reader of `approved_by` can tell a question the Operator read from
-- one this file waved through, which is the whole point of writing it here
-- rather than defaulting the column.
--
-- A DATA BACKFILL WITH NO SCHEMA CHANGE, in the family of 071, 084 and 088, and
-- kept apart from 089 so the integration suite can replay it over rows it
-- staged (executeMigration). It touches drafts only — a row some later verdict
-- has already reached is that verdict's business — and a question authored
-- after it has run is a draft like any other.
UPDATE ticket_questions
SET review_status = 'approved',
    approved_at = NOW(),
    approved_by = 'migration:090_backfill_ticket_question_approval'
WHERE review_status = 'draft';

UPDATE ticket_question_options
SET review_status = 'approved',
    approved_at = NOW(),
    approved_by = 'migration:090_backfill_ticket_question_approval'
WHERE review_status = 'draft';
