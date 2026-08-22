# The Cloud Scheduler tick that drives the Holder Address Purge (#331,
# parent #322, ADR 0046): the address a buyer typed for a friend who never
# accepted it, taken once the Event has started.
#
# The shape is answer_purge.tf's, and the argument for a scheduler rather than an
# in-process ticker is the one ADR 0024 records — min_instances = 0 and
# cpu_idle = true mean there is frequently no process alive to tick.
#
# THIS IS THE SECOND SCHEDULED JOB IN THE DEPLOYMENT THAT DELETES ANYTHING, and
# what it deletes is unlike anything else here. The reversal drain asks a
# provider a question; the Digest and reminder jobs send mail; the Abandoned
# Answer Purge removes rows a BUYER typed about themselves at a checkout they
# abandoned. This one removes contact details for a person who never came to this
# platform, supplied at the word of somebody with no authority to supply them.
# That is the cost ADR 0046 priced, and this job is the payment — which cuts both
# ways: it is the reason the tick must actually run once assignment is open, and
# the reason it must be stoppable in one edit if it is ever suspected of taking
# more than it should.
#
# WHY THE CADENCE IS DAILY AND NOT PER-MINUTE, on the purge's reasoning: the
# promise is "when the Event starts", and the difference between enforcing that
# within a minute and within a day is not worth a job that fires 1,440 times a
# day, because a bug in such a job empties the column before anybody reads the
# first alert. A missed run costs nothing and needs no catch-up: the endpoint
# purges by predicate, so a day's outage means the next tick takes two days'
# worth. There is no queue here, no cursor and nothing to fall behind on.

# --- Identity -----------------------------------------------------------------

# Its own identity, on the terms every job here has one: the audit log has to be
# able to say "the holder address purge ran" as a different sentence from "the
# answer purge ran", and revoking one must never silently revoke the other. It
# matters most for the two accounts whose calls destroy data — an unexplained
# deletion in the log is an incident, and it can only be traced to a caller if
# the caller is distinguishable from the other one that also deletes.
#
# The account_id is "holder-purge" and not "holder-address-purge" because GCP
# caps a service account id at 30 characters: with the "prod-ticket-pos-" prefix
# the full name overruns by six, the same wall answer_reminder.tf hit. The
# Scheduler job below keeps the unabbreviated name — its own limit is far higher
# — so the pair reads as one thing everywhere except this line.
resource "google_service_account" "holder_address_purge" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-holder-purge"
  display_name = "Ticket POS ${var.environment} Holder Address Purge"
  description  = "Identity Cloud Scheduler presents when driving the ${var.environment} Holder Address Purge (ADR 0046)"
}

# run.invoker on the API service and nothing else. This account never deploys,
# never reads a secret and never reaches the database: everything it may do is
# "make one authenticated HTTP call to one Cloud Run service", and what that call
# is allowed to delete is the endpoint's decision and not this grant's.
resource "google_cloud_run_v2_service_iam_member" "holder_address_purge_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.holder_address_purge.email}"
}

# --- Schedule -----------------------------------------------------------------

resource "google_cloud_scheduler_job" "holder_address_purge" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-holder-address-purge"
  schedule = var.holder_address_purge_schedule

  # Ecuador, so "this last ran at 03:40" reads against the working day of the
  # people who would be looking. The cadence itself is timezone-independent: the
  # rule is "the Event has started", and the Event's own timezone is already
  # baked into the TIMESTAMPTZ the backend compares against — nothing about which
  # zone this cron is read in changes which Events qualify.
  time_zone = "America/Guayaquil"

  description = "Drives the Holder Address Purge: deletes the holder email address from Tickets still in assigned whose Event has started, leaving the Ticket, its Answers and the fact of the assignment (ADR 0046)"

  # Paused, not absent, when disabled — and this job SHIPS PAUSED, like every
  # scheduled job in this module before it.
  #
  # It ships paused for the reason the answer purge does, and it comes off pause
  # on a schedule of its own. The feature that fills this column is behind
  # TICKET_ASSIGNMENT_ENABLED, which is a SEPARATE flag from the questions one
  # and is off until a Policy Version describes this collection (ADR 0045), so
  # until that flag flips there is nothing to purge and a firing job would be a
  # daily UPDATE matching no rows. Turning it on is a different decision, made
  # later, by a human who can see that the addresses_held figure has started
  # moving — and it is a decision that must actually be made, because an open
  # assignment flag with this job still paused is the platform holding third
  # party addresses with no scheduled end.
  #
  # THE BACKEND DOES NOT REFUSE INDEPENDENTLY HERE, unlike the reminder sweep,
  # and that is deliberate: the purge is not gated on the feature flag, because
  # the switch that turns a deletion off must never be the switch that turns
  # collection off. This pause is therefore the ONLY switch on this job, which is
  # exactly what makes it the first move when a purge is suspected of deleting
  # more than it should — pause, then read, because the addresses are gone.
  paused = !var.holder_address_purge_enabled

  retry_config {
    # No retries, on the answer purge's reasoning. A failed tick is not worth
    # repeating because the next one is a day away and takes exactly what this
    # one would have: the predicate is state-based, so nothing is lost by waiting
    # and nothing accumulates that a catch-up would have to work through.
    # Retrying a deletion that may have partially succeeded buys nothing and
    # makes the log harder to read during the one kind of incident this job can
    # have.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. Note what is NOT in this URI and may never be
    # added — a moment, an Event, an Organization, a Ticket. Which addresses go
    # comes from the backend's clock and the database precisely so that
    # possession of this account's token is not possession of a
    # take-every-holder-address button; see
    # catalog/handler.PurgeUnacceptedHolderAddresses.
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/holder-addresses/purge"

    # Cloud Scheduler puts the ID token in `Authorization` and cannot be told to
    # put it anywhere else, which matters because ADR 0008 keeps that header for
    # the end-user session token. Why that is safe on this route is written down
    # beside the route itself, in backend/internal/server/routes.go.
    #
    # The audience is the service's root URL, not the purge path: Cloud Run
    # validates the token's `aud` against the service URL, and a path here
    # produces a 401 that looks like a broken IAM grant.
    oidc_token {
      service_account_email = google_service_account.holder_address_purge.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # Generous, because like the answer purge this run has no budget of its own to
  # expire first. It is a single UPDATE against a predicate — there is no loop to
  # bound and no per-item work to abandon halfway — so the only thing this
  # deadline can catch is a statement wedged behind a lock, and the honest
  # response to that is to give up and let tomorrow's tick try again.
  #
  # It stays below api_request_timeout_seconds for the reason the chain elsewhere
  # gives: a deadline above the request timeout means Scheduler is waiting on a
  # request the platform has already abandoned, and the run is recorded as a
  # timeout rather than as the failure it was.
  attempt_deadline = "${var.holder_address_purge_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.holder_address_purge_invokes_api]
}
