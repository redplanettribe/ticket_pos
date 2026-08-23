# The Cloud Scheduler tick that drives the Assignment Reminder sweep (#361,
# #365, ADR 0051): one mail to the buyer of an online Ticket Sale that still has
# Tickets nobody holds, pointing them at their Sale's page in the Customer Area
# so they can name a Holder for each.
#
# The shape is answer_reminder.tf's, read that file first: the argument for a
# scheduler rather than an in-process ticker (ADR 0024), the daily cadence, the
# no-retry rule and the un-aimable endpoint are all the same argument made
# once. What is written here is only where this job differs.
#
# IT IS THE SAME KIND OF MAIL — sent to people who did not ask to be written
# to, about a purchase they made — and so it ships paused on the same terms.
# Its first run is also a catch-up: every buyer of more than one Ticket before
# Ticket Assignment went live (the go-live constant beside the message type in
# the backend) qualifies at once, which makes the first tick the largest this
# job will ever send. That run is made by hand, while somebody is watching, and
# the runbook for it is docs/runbook-assignment-reminder.md.
#
# THE RATIONING IS NOT HERE, and must never be moved here. At most one mail per
# TICKET SALE per 7 days, at most two ever, nothing in the first 24 hours after
# the Sale, silence once the Event has started: all of it lives in the backend
# beside the sweep, where it holds however the endpoint is called. A schedule
# is not a rate limit.
#
# A missed run costs nothing and needs no catch-up: the sweep works from state
# rather than from events, so a day's outage means the same buyers are due
# tomorrow.

# --- Identity -----------------------------------------------------------------
#
# No account of its own. #365 drives this job with the EXISTING scheduler
# identity and invoker grant — google_service_account.answer_reminder and its
# run.invoker binding in answer_reminder.tf — and that is a deliberate departure
# from the one-identity-per-job rule the other files keep. The two sweeps are
# the same act by the same sender (the transactional identity, to buyers, about
# their own Sale), rationed by the same kind of ledger and paused by the same
# kind of switch; what the audit log needs to tell apart is the URI each call
# hit, and it records that. Revoking the shared account stops both reminders at
# once, which for the failure that would prompt it — mail reaching the wrong
# people — is the right blast radius rather than the wrong one.
#
# If the two ever need separate revocation, the move is a second account with
# its own run.invoker member, made on answer_reminder.tf's pattern.

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "assignment_reminder" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-assignment-reminder"
  schedule = var.assignment_reminder_schedule

  # Ecuador, for the reason answer_reminder.tf gives: the hour this fires is
  # the hour a buyer's mail arrives. It fires half an hour after the Answer
  # Reminder so a buyer who is owed one of each gets them as two mails a little
  # apart rather than a pair in the same second.
  time_zone = "America/Guayaquil"

  description = "Drives the Assignment Reminder sweep: mails the buyer of every online Ticket Sale that still has unassigned Tickets, rationed per Sale and silent once the Event has started (ADR 0051)"

  # Paused, not absent, when disabled — and this job SHIPS PAUSED. Merging
  # mails nobody; launching is an Operator's act, made by setting
  # assignment_reminder_enabled in terraform.tfvars and applying, then forcing
  # one run by hand (docs/runbook-assignment-reminder.md).
  #
  # The backend refuses independently: the sweep returns no candidates while
  # TICKET_ASSIGNMENT_ENABLED is off. Two switches, both thrown deliberately.
  paused = !var.assignment_reminder_enabled

  retry_config {
    # NO RETRIES, for the reason answer_reminder.tf gives: a retry of a run
    # whose ledger writes were failing would mail the same buyers again, and
    # the next tick is a day away and works from the same state.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped, no body, no parameters, no moment:
    # the 7-day window is arithmetic on the backend's own clock, so possession
    # of this account's token is not a mail-everybody-now button.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/assignment-reminders/sweep"

    # Same token placement and audience rule as the Answer Reminder: the
    # audience is the service root, never the sweep path.
    oidc_token {
      service_account_email = google_service_account.answer_reminder.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Middle term of the same deadline chain the Answer Reminder keeps: the run's
  # own budget first, this second, api_request_timeout_seconds last.
  attempt_deadline = "${var.assignment_reminder_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.answer_reminder_invokes_api]
}
