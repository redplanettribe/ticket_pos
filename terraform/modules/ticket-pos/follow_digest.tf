# The two Cloud Scheduler jobs that drive the weekly Follow Digest (#226,
# ADR 0030).
#
# This is the SECOND scheduled execution in the deployment, after ADR 0024's
# Reversal Reconciler, and the first that fans out to many recipients. It follows
# that file line for line on purpose: a dedicated service account per job, an
# OIDC token, run.invoker on the API service and nothing else, `paused` rather
# than absent, and an attempt deadline that is never what stops a run. Where this
# file differs from reversal_reconciler.tf, the difference is commented; where it
# does not, the reasoning is written down there and is not restated here.
#
# WHY TWO JOBS. The pipeline is two endpoints because the mail provider's rate
# limit — roughly two requests a second, which ADR 0009 records as unsolved for
# bulk sends — makes "mail everybody in one request" impossible inside any
# request timeout. The weekly job declares a week; the per-minute job paces the
# sending. Neither decides anything the other does: what is owed to whom lives in
# `follow_digests`, and the ticks only supply the heartbeat.
#
# WHAT THIS FILE CANNOT DO. Turning these jobs on does not, by itself, put a
# Digest in anybody's inbox. The Digest sends on its own domain (#225, ADR 0030)
# and a deployment with no Digest sending identity configured refuses every send
# loudly (platform.UnconfiguredDigestSender). Provisioning that domain in the
# mail provider and publishing its DNS records is an operator's job and no part
# of this file: until it is done, the drain will tick, claim, compose, and record
# every Digest as retrying and then failed.

# --- Identity -----------------------------------------------------------------

# ONE ACCOUNT PER JOB rather than one "digest" account for both, which is the one
# place this file is deliberately more expensive than reversal_reconciler.tf. Two
# reasons: the audit log has to be able to say "the weekly job enqueued a week"
# and "the minute tick sent forty Digests" as different sentences, and the two
# jobs have very different blast radii — the weekly one addresses every following
# Customer on the platform, and revoking it in an incident must not also stop the
# drain from finishing the work already queued.
resource "google_service_account" "follow_digest_enqueue" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-digest-enq"
  display_name = "Ticket POS ${var.environment} Follow Digest enqueue"
  description  = "Identity Cloud Scheduler presents when declaring the ${var.environment} Follow Digest week"
}

resource "google_service_account" "follow_digest_drain" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-digest-drain"
  display_name = "Ticket POS ${var.environment} Follow Digest drain"
  description  = "Identity Cloud Scheduler presents when draining the ${var.environment} Follow Digest queue"
}

# run.invoker on the API service and nothing else — not a project-level binding,
# not run.developer. Everything either account is allowed to do is "make one
# authenticated HTTP call to one Cloud Run service"; which endpoint it may reach
# is not expressible in IAM, and the endpoints themselves are what decide the
# rest.
resource "google_cloud_run_v2_service_iam_member" "follow_digest_enqueue_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.follow_digest_enqueue.email}"
}

resource "google_cloud_run_v2_service_iam_member" "follow_digest_drain_invokes_api" {
  project  = var.project_id
  location = google_cloud_run_v2_service.api.location
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.follow_digest_drain.email}"
}

# --- The weekly enqueue -------------------------------------------------------

resource "google_cloud_scheduler_job" "follow_digest_enqueue" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-follow-digest-enqueue"
  schedule = var.follow_digest_enqueue_schedule

  # Ecuador, and here the zone is load-bearing in a way the reconciler's is not.
  # The reconciler ticks every minute, so its cadence is timezone-independent and
  # the zone only makes an incident log readable. This job fires ONCE A WEEK at a
  # named hour, and that hour is a fact about the reader's morning: 09:00 in
  # Ecuador is where the Digest is meant to land, and the same expression in UTC
  # would land at 04:00 local.
  time_zone = "America/Guayaquil"

  description = "Declares the week and enqueues one pending Follow Digest per eligible Customer (ADR 0030)"

  paused = !var.follow_digest_enqueue_enabled

  retry_config {
    # NO RETRIES, and for a stronger reason than the reconciler's. A failed
    # weekly tick cannot be repaired by Scheduler retrying it, because the
    # endpoint is idempotent within a week and a retry would therefore do exactly
    # what the drain's next tick already does: nothing. What a failed weekly tick
    # actually needs is a human, and one curl of the same URL is the whole
    # recovery — the enqueue may be run by hand at any point in the week and will
    # produce the same rows.
    retry_count = 0
  }

  http_target {
    http_method = "POST"

    # The pinned contract. Internal-scoped: reachable by neither a Customer
    # Session nor a staff token. What the endpoint promises its callers — this
    # job and the operator curling the same URL — is on the handler
    # (digest/handler.EnqueueFollowDigests).
    uri = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/follow-digests/enqueue"

    # The audience is the service's root URL, not the enqueue path: Cloud Run
    # validates the token's `aud` against the service URL, and a path here
    # produces a 401 that looks like a broken IAM grant. See
    # reversal_reconciler.tf for why the token lands in `Authorization` and why
    # that is safe on this route.
    oidc_token {
      service_account_email = google_service_account.follow_digest_enqueue.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # The enqueue is one statement against the Follow tables and has no budget of
  # its own to stop it politely — there is no loop here to stop. Its deadline is
  # therefore only the outer guard, and it stays inside Cloud Run's request
  # timeout for the reason the drain's does: a deadline beyond it would leave
  # Scheduler waiting on a request Cloud Run has already killed, and an operator
  # unable to tell which limit ended the run. A backend test holds that
  # relationship (digest/service.TestTheDigestEnqueueDeadlineIsInsideTheRequestTimeout).
  attempt_deadline = "${var.follow_digest_enqueue_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.follow_digest_enqueue_invokes_api]
}

# --- The per-minute drain -----------------------------------------------------

resource "google_cloud_scheduler_job" "follow_digest_drain" {
  project  = var.project_id
  region   = var.region
  name     = "${var.environment}-ticket-pos-follow-digest-drain"
  schedule = var.follow_digest_drain_schedule

  # Timezone-independent, like the reconciler's minute tick. It is stated anyway
  # so that a human reading "this last ran at 09:04" during the one hour a week
  # this pipeline does any work is reading it in the same zone as the weekly job
  # beside it.
  time_zone = "America/Guayaquil"

  description = "Paces the Follow Digest send inside the mail provider's rate limit (ADR 0030)"

  paused = !var.follow_digest_drain_enabled

  retry_config {
    # No retries: the next tick is a minute away and is a strictly better attempt,
    # and per-Digest backoff already lives in follow_digests.next_attempt_at where
    # an operator can read it in SQL. Scheduler-level retries would race that
    # schedule with a second, invisible one — and here that second schedule would
    # be racing to send somebody an email.
    retry_count = 0
  }

  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.api.uri}/api/v1/internal/follow-digests/drain"

    oidc_token {
      service_account_email = google_service_account.follow_digest_drain.email
      audience              = google_cloud_run_v2_service.api.uri
    }
  }

  # This must never be what stops a run: the backend budgets itself, finishes and
  # returns a summary, and Scheduler is here only to catch a run that never
  # answers at all. The whole chain — what each term is and what breaks when one
  # moves alone — is written down once in
  # backend/internal/digest/service/service.go beside digestDrainBudget, which is
  # also where the number this must stay above lives.
  #
  # It exceeds the one-minute interval, and that is fine rather than something to
  # prevent. Cloud Scheduler promises at-least-once delivery, not that two runs
  # never overlap, so overlap has to be harmless — and it is: a claim moves a
  # Digest's `next_attempt_at` forward under FOR UPDATE SKIP LOCKED, so a second
  # tick either finds nothing due or skips the contended row. Correctness rests on
  # that claim and on the uniqueness of (Customer, week), never on the schedule.
  attempt_deadline = "${var.follow_digest_drain_attempt_deadline_seconds}s"

  depends_on = [google_cloud_run_v2_service_iam_member.follow_digest_drain_invokes_api]
}
