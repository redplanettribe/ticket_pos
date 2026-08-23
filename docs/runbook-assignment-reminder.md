# Runbook: launching the Assignment Reminder

The Assignment Reminder (ADR 0051, #361) is a daily, rationed, transactional
mail to the buyer of an online Ticket Sale that still has Tickets nobody holds.
Its Cloud Scheduler job (`terraform/modules/ticket-pos/assignment_reminder.tf`)
ships **paused**: merging mails nobody. Launching it is a Platform Operator's
act, done in the order below, because the first run is also the catch-up for
every buyer who bought more than one Ticket before Ticket Assignment went live
on 2026-08-22 — the largest run this job will ever make.

The sweep endpoint is `POST /api/v1/internal/assignment-reminders/sweep`. It
takes no body and no parameters and reports counts only; the rationing (one
mail per Sale per 7 days, two ever, none in a Sale's first 24 hours, silence
once the Event has started) lives in the backend, not in the schedule.

## Launch

1. **Merge** the Assignment Reminder PRs (#362–#365) and let the deploy finish.
2. **Migrate.** The deploy runs migrations; confirm `assignment_reminders`
   exists in prod (the ledger table, #362) before going further. A sweep
   against a missing ledger fails every send and mails nobody, but do not rely
   on that.
3. **Copy production locally.** `make prod-to-local` (prompts; requires
   `make dev` up and a gcloud login). The local stack now holds real customer
   data — treat it as production.
4. **Run the candidate query locally and review the count.** See below. The
   number is how many buyers the first run will write to. If it is not roughly
   what you expect — a few hundred at most for this platform's size — stop and
   read the predicate against the sweep's SQL before enabling anything.
5. **Set the tfvar.** In `terraform/envs/prod/terraform.tfvars` change
   `assignment_reminder_enabled = false` to `true`. Commit it: the launch is a
   reviewable diff, never a console click.
6. **Apply.** From `terraform/envs/prod`, with the usual `.env` sourced so the
   secret-bearing variables are present: `terraform plan`, check the only
   change is the job's `paused` flag flipping, then `terraform apply`.
7. **Force one run, while watching.** Either in the Cloud Scheduler console
   ("Force run" on `prod-ticket-pos-assignment-reminder`) or:

       gcloud scheduler jobs run prod-ticket-pos-assignment-reminder \
         --location us-east1 --project multiticketing

   Read the API's log line for the sweep: `due`, `sent`, `skipped`, `failed`,
   `unrecorded`, `backlog`. `sent` should match the count from step 4 (less
   anything created in the last 24 hours); `unrecorded` must be 0 — a non-zero
   value means a mail went out without its ledger row and that buyer would be
   written to again; pause (below) and investigate before the next tick. If
   `backlog` is non-zero the batch limit was hit: force-run again until it is
   zero, or let the daily ticks drain it.
8. **Daily ticks thereafter.** The job fires at 10:30 America/Guayaquil. The
   second and final mail to anyone still qualifying goes automatically a week
   after their first. Nothing further to do.

## Stop

Set `assignment_reminder_enabled = false` in `terraform.tfvars` and
`terraform apply`. The job is paused, not deleted, and the state agrees with
reality — which a console pause does not, since the next apply would undo it.
Pausing is the first move on any suspicion that the mail reached the wrong
people or reached them too often; a mail that has gone cannot be recalled, so
pause first and read second.

## Candidate query (local dry run)

Run against the local copy after `make prod-to-local`:

    psql "postgres://ticket_pos:ticket_pos@localhost:64432/ticket_pos?sslmode=disable"

It mirrors the sweep's predicate minus the ledger rationing (the ledger is
empty before the first run, so the count is the first run's audience), and it
reports a **count only**. Do not widen it to select names or addresses; the
local database is a production copy.

```sql
-- Assignment Reminder, first-run candidates (ADR 0051).
WITH sale_tickets AS (
    SELECT
        s.id AS sale_id,
        COUNT(t.id)                                   AS ticket_count,
        COUNT(t.id) FILTER (WHERE t.holder_email IS NULL) AS unassigned_count
    FROM ticket_sales s
    JOIN ticket_sale_lines l ON l.ticket_sale_id = s.id
    JOIN tickets t           ON t.ticket_sale_line_id = l.id
    WHERE s.channel = 'online'
      AND s.status = 'active'
      AND s.reversed_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM sale_reversals r WHERE r.ticket_sale_id = s.id)
      AND s.created_at < TIMESTAMPTZ '2026-08-22T16:33:38Z'   -- go-live constant
    GROUP BY s.id
)
SELECT
    COUNT(*) AS candidate_sales
FROM sale_tickets st
JOIN ticket_sales s ON s.id = st.sale_id
JOIN events e       ON e.id = s.event_id
WHERE st.ticket_count > 1
  AND st.unassigned_count >= 1
  AND e.status = 'published'
  AND e.starts_at IS NOT NULL
  AND e.starts_at > NOW();
```

Drop the `created_at` line to count everyone the sweep would write to today
rather than only the pre-feature backlog; the backend also excludes Sales
younger than 24 hours, which this query does not.

Notes on the columns: `events.starts_at` is a `TIMESTAMPTZ` with the Event's
zone already inside it, so `> NOW()` is "has not started" in the Event's own
timezone without a conversion; `events.timezone` is for display only. A
Ticket's `holder_email IS NULL` is the `unassigned` state (migration 080); a
Self-held Ticket that the buyer reassigned carries the new address and so
counts as assigned. A Sale Reversal is whole-Sale and recorded on
`ticket_sales.status`/`reversed_at` (migrations 031–032) with a request row in
`sale_reversals` (migration 039); both are checked.
