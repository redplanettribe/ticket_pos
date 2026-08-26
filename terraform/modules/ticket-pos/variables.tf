variable "project_id" {
  description = "GCP project the environment is deployed into."
  type        = string
}

variable "region" {
  description = "Region every regional resource is created in. Cloud Run, Cloud SQL and the bucket must share it."
  type        = string
}

variable "environment" {
  description = "Environment name, e.g. `prod`. Used to name and label resources; nothing here assumes a particular value."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.environment))
    error_message = "environment must be lowercase alphanumeric with hyphens, 2-21 characters."
  }
}

variable "artifact_registry_repository_id" {
  description = "Name of the Docker repository container images are pushed to."
  type        = string
  default     = "ticket-pos"
}

# --- Database -----------------------------------------------------------------

variable "database_version" {
  description = "Cloud SQL Postgres major version. Must match what local development and CI run, or migrations are validated against a different engine."
  type        = string
  default     = "POSTGRES_18"
}

variable "database_tier" {
  description = "Cloud SQL machine type. `db-g1-small` is 1 shared vCPU / 1.7GB, roughly $25/month single-zone."
  type        = string
  default     = "db-g1-small"
}

variable "database_disk_size_gb" {
  description = "Initial data disk size in GB. Autoresize is on, so this is a floor rather than a limit."
  type        = number
  default     = 10

  validation {
    condition     = var.database_disk_size_gb >= 10
    error_message = "database_disk_size_gb must be at least 10; Cloud SQL rejects smaller disks."
  }
}

variable "database_backup_start_time" {
  description = "HH:MM UTC at which the daily automated backup starts."
  type        = string
  default     = "05:00"

  validation {
    condition     = can(regex("^([01][0-9]|2[0-3]):[0-5][0-9]$", var.database_backup_start_time))
    error_message = "database_backup_start_time must be HH:MM in 24-hour UTC."
  }
}

variable "database_retained_backups" {
  description = "How many daily automated backups to keep."
  type        = number
  default     = 7

  validation {
    condition     = var.database_retained_backups >= 1
    error_message = "database_retained_backups must be at least 1."
  }
}

variable "database_transaction_log_retention_days" {
  description = "How far back point-in-time recovery can rewind. Longer retention costs more log storage."
  type        = number
  default     = 7

  validation {
    condition     = var.database_transaction_log_retention_days >= 1 && var.database_transaction_log_retention_days <= 35
    error_message = "database_transaction_log_retention_days must be between 1 and 35."
  }
}

variable "database_maintenance_day" {
  description = "Day of week (1 = Monday, 7 = Sunday) on which Google may apply maintenance."
  type        = number
  default     = 7

  validation {
    condition     = var.database_maintenance_day >= 1 && var.database_maintenance_day <= 7
    error_message = "database_maintenance_day must be between 1 and 7."
  }
}

variable "database_maintenance_hour" {
  description = "Hour of day (UTC) at which maintenance may start."
  type        = number
  default     = 6

  validation {
    condition     = var.database_maintenance_hour >= 0 && var.database_maintenance_hour <= 23
    error_message = "database_maintenance_hour must be between 0 and 23."
  }
}

variable "database_name" {
  description = "Application database created on the instance."
  type        = string
  default     = "ticket_pos"
}

variable "database_user" {
  description = "Application database user. Its password is generated and stored in Secret Manager, never set here."
  type        = string
  default     = "ticket_pos_app"
}

# --- Object storage -----------------------------------------------------------

variable "storage_bucket_name" {
  description = "Globally unique name for the public media bucket. Defaults to `<project>-ticket-pos-<environment>`."
  type        = string
  default     = null
}

variable "storage_cors_origins" {
  description = "Origins allowed to PUT to the bucket: every browser-facing surface that uploads directly with a presigned URL. Staff does cover images and logos, the Storefront does Customer Avatars. An origin missing here fails uploads in production while working against MinIO locally."
  type        = list(string)

  validation {
    condition     = length(var.storage_cors_origins) > 0
    error_message = "storage_cors_origins must list at least the Staff app origin, or direct browser uploads cannot work."
  }

  validation {
    condition     = alltrue([for o in var.storage_cors_origins : can(regex("^https?://[^/]+$", o))])
    error_message = "Each origin must be scheme://host[:port] with no trailing path; browsers match the Origin header literally."
  }
}

variable "storage_cors_max_age_seconds" {
  description = "How long a browser may cache the CORS preflight response."
  type        = number
  default     = 3600
}

variable "storage_s3_region" {
  description = "Region name the API signs S3-compatible requests with. GCS checks it against the bucket location in the SigV4 credential scope of a presigned URL, so a mismatch fails uploads rather than reads. Defaults to the deployment region, which is where the bucket is."
  type        = string
  default     = null
}

# --- Network ------------------------------------------------------------------

variable "cloud_run_subnet_cidr" {
  description = "Range for the subnet Cloud Run uses for direct VPC egress. Every running instance holds one address. Must not overlap the 10.240.0.0/24 reserved for the Cloud SQL peering, and cannot be narrowed after creation."
  type        = string
  default     = "10.241.0.0/24"

  validation {
    condition     = can(cidrhost(var.cloud_run_subnet_cidr, 0))
    error_message = "cloud_run_subnet_cidr must be a valid CIDR block."
  }
}

# --- API service and migrate Job ----------------------------------------------

variable "api_image_name" {
  description = "Image name within the Artifact Registry repository. The service and the migrate Job run this same image, differing only in entrypoint."
  type        = string
  default     = "api"
}

variable "api_image_tag" {
  description = "Tag used when the service and Job are first created. Terraform does not own the running image afterwards — deploys roll a new revision and the image field is in ignore_changes — so changing this does not deploy anything."
  type        = string
  default     = "latest"
}

variable "api_min_instances" {
  description = "Instances kept warm. Zero means the deployment costs nothing while idle and the first request after idling pays a cold start."
  type        = number
  default     = 0

  validation {
    condition     = var.api_min_instances >= 0
    error_message = "api_min_instances cannot be negative."
  }
}

variable "api_max_instances" {
  description = "Ceiling on concurrent instances. Bounds both the bill and the number of Postgres connection pools opened against a db-g1-small."
  type        = number
  default     = 4

  validation {
    condition     = var.api_max_instances >= 1
    error_message = "api_max_instances must be at least 1."
  }
}

variable "api_concurrency" {
  description = "Requests one instance serves at a time. Raising it trades instance count for pressure on each instance's database pool."
  type        = number
  default     = 80

  validation {
    condition     = var.api_concurrency >= 1 && var.api_concurrency <= 1000
    error_message = "api_concurrency must be between 1 and 1000."
  }
}

variable "api_request_timeout_seconds" {
  description = "How long Cloud Run waits for a response before killing the request. Cloud Run's own default; lowering it would put a new ceiling on the slowest existing endpoint (sale import parses an uploaded file in memory) that nothing else in the system imposes. It is also the outermost term of the reversal drain's deadline chain, written down once in backend/internal/sales/service/reconciler.go beside reversalDrainBudget — read that before lowering it near reversal_reconciler_attempt_deadline_seconds."
  type        = number
  default     = 300

  validation {
    condition     = var.api_request_timeout_seconds >= 1 && var.api_request_timeout_seconds <= 3600
    error_message = "api_request_timeout_seconds must be between 1 and 3600."
  }
}

variable "api_cpu" {
  description = "CPU limit per API instance. Billed only while a request is in flight."
  type        = string
  default     = "1"
}

variable "api_memory" {
  description = "Memory limit per API instance. This is what an idle instance is billed for when min_instances is above zero."
  type        = string
  default     = "512Mi"
}

variable "api_log_level" {
  description = "LOG_LEVEL for the API and the migrate Job. One of debug, info, warn, error."
  type        = string
  default     = "info"

  validation {
    condition     = contains(["debug", "info", "warn", "error"], var.api_log_level)
    error_message = "api_log_level must be one of debug, info, warn, error."
  }
}

variable "migrate_cpu" {
  description = "CPU limit for a migration task."
  type        = string
  default     = "1"
}

variable "migrate_memory" {
  description = "Memory limit for a migration task."
  type        = string
  default     = "512Mi"
}

variable "migrate_timeout_seconds" {
  description = "How long a migration task may run before Cloud Run kills it. Note that cmd/migrate imposes its own 30s context deadline, so this is the outer bound, not the effective one."
  type        = number
  default     = 900

  validation {
    condition     = var.migrate_timeout_seconds >= 1
    error_message = "migrate_timeout_seconds must be at least 1."
  }
}

variable "migrate_max_retries" {
  description = "Retries after a failed migration task. Zero on purpose: a failed migration is a thing to read, not to repeat."
  type        = number
  default     = 0

  validation {
    condition     = var.migrate_max_retries >= 0
    error_message = "migrate_max_retries cannot be negative."
  }
}

# --- Frontends ------------------------------------------------------------------

variable "staff_image_name" {
  description = "Image name within the Artifact Registry repository for the Staff Next server."
  type        = string
  default     = "staff"
}

variable "storefront_image_name" {
  description = "Image name within the Artifact Registry repository for the Storefront Next server."
  type        = string
  default     = "storefront"
}

variable "frontend_image_tag" {
  description = "Tag used when the frontend services are first created. As with api_image_tag, Terraform does not own the running image afterwards, so changing this deploys nothing."
  type        = string
  default     = "latest"
}

variable "staff_min_instances" {
  description = "Minimum Staff instances. Zero means Members pay a cold start on the first request after an idle period."
  type        = number
  default     = 0

  validation {
    condition     = var.staff_min_instances >= 0
    error_message = "staff_min_instances cannot be negative."
  }
}

variable "staff_max_instances" {
  description = "Ceiling on Staff instances, and so on the bill."
  type        = number
  default     = 4

  validation {
    condition     = var.staff_max_instances >= 1
    error_message = "staff_max_instances must be at least 1."
  }
}

variable "storefront_min_instances" {
  description = "Minimum Storefront instances. Zero costs a cold start on the surface where a slow first paint costs a ticket sale; raise it before an on-sale."
  type        = number
  default     = 0

  validation {
    condition     = var.storefront_min_instances >= 0
    error_message = "storefront_min_instances cannot be negative."
  }
}

variable "storefront_max_instances" {
  description = "Ceiling on Storefront instances."
  type        = number
  default     = 10

  validation {
    condition     = var.storefront_max_instances >= 1
    error_message = "storefront_max_instances must be at least 1."
  }
}

variable "frontend_concurrency" {
  description = "Requests served concurrently per frontend instance. Next servers are IO-bound proxies here, so this can be far higher than the API's limit — they hold no database connections."
  type        = number
  default     = 80

  validation {
    condition     = var.frontend_concurrency >= 1 && var.frontend_concurrency <= 1000
    error_message = "frontend_concurrency must be between 1 and 1000."
  }
}

variable "frontend_cpu" {
  description = "CPU limit per frontend instance."
  type        = string
  default     = "1"
}

variable "frontend_memory" {
  description = "Memory limit per frontend instance. Next standalone servers idle around 100-150MiB."
  type        = string
  default     = "512Mi"
}

# --- CI deploy (Workload Identity Federation) ---------------------------------

variable "github_repository" {
  description = "The `owner/repo` GitHub Actions may impersonate the deploy service account from. This single value is the WIF attribute condition AND the impersonation principalSet, so it must name one repository exactly. A value matching a whole org, or a wildcard, would let any repository under that owner mint deploy tokens."
  type        = string
  default     = "redplanettribe/ticket_pos"

  validation {
    # Exactly one `owner/repo`, no wildcard, no extra path segments. This is the
    # guard that keeps the federation from ever being scoped broader than a
    # single repository.
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.github_repository))
    error_message = "github_repository must be a single owner/repo (no wildcards, no trailing path). Scoping WIF to anything broader lets other repositories impersonate the deploy identity."
  }
}

variable "staff_domain" {
  description = "Hostname for the Staff console, e.g. pos.multiticketing.com. Null skips the domain mapping and leaves Staff reachable only on its run.app URL."
  type        = string
  default     = null
}

variable "storefront_domain" {
  description = "Hostname for the Storefront, e.g. discover.multiticketing.com. Null skips the domain mapping."
  type        = string
  default     = null
}

variable "otp_global_ceiling" {
  description = "Platform-wide cap on passcode emails per rate-limit window. Null uses the API's own default. Exists so the ceiling can be retuned under attack without a code deploy."
  type        = number
  default     = null

  validation {
    condition     = var.otp_global_ceiling == null || var.otp_global_ceiling > 0
    error_message = "otp_global_ceiling must be a positive integer; the API rejects a non-positive value at startup."
  }
}

variable "ticket_questions_enabled" {
  description = "Whether an Organization may define Ticket Questions in the staff app (#309). STARTS FALSE, and the prerequisite is legal rather than technical: a Ticket Question can ask anything an Organization types, including dietary requirements, and the published Privacy Policy does not describe collecting that (ADR 0045). It flips only once a Policy Version that does has published — which re-gates every Customer, so it should ride with other policy changes. With it false the staff endpoints answer 404 and no Storefront surface or export differs from a build without the feature."
  type        = bool
  default     = false
}

variable "ticket_assignment_enabled" {
  description = "Whether a buyer may assign one of their own Tickets to an email address (#324, parent #322). A SEPARATE VARIABLE FROM ticket_questions_enabled ABOVE AND NEVER A SECOND USE OF IT: the two features are separable, and two flags are what let assignment be killed without taking Ticket Questions dark. STARTS FALSE, and the prerequisite is legal rather than technical — what this opens is the platform storing, and later mailing, an email address supplied by somebody with no authority to supply it, and disclosing it to the Organization as a separate controller. None of that is described by the published Privacy Policy. It flips only once a Policy Version that does has published; that clause is batched with the one ticket_questions_enabled waits on, so the re-acceptance of every Customer is paid for once. With it false the buyer's assign endpoint answers 404, no address is stored, no mail is sent and the buyer's own payload is byte-identical to a build without the feature."
  type        = bool
  default     = false
  # THE MAIL PROMISES A DELETION THIS ALONE DOES NOT PERFORM. The Assignment mail
  # tells a stranger, in their own language, "we delete your email address then" --
  # meaning at Event start. That sentence is made true by the Holder Address Purge
  # and by nothing else, and the purge deliberately does NOT read this flag (see
  # holder_address_purge_enabled): gating it here would mean a deployment that shut
  # assignment kept every address it had already collected, forever, precisely
  # because it had stopped collecting.
  #
  # So the two switches have to be thrown together, and this is the only place that
  # can insist on it. Opening assignment while the purge is paused would have the
  # platform make a retention promise to somebody who never came here and then not
  # keep it -- the one failure this feature cannot absorb, because the promise is
  # most of what makes collecting the address defensible at all.
  validation {
    condition     = !var.ticket_assignment_enabled || var.holder_address_purge_enabled
    error_message = "ticket_assignment_enabled requires holder_address_purge_enabled: the Assignment mail promises the address is deleted when the Event starts, and the Holder Address Purge is what keeps that promise."
  }
}

# --- Reversal Reconciler ------------------------------------------------------

variable "reversal_reconciler_enabled" {
  description = "Whether the Cloud Scheduler tick actually fires. False leaves the job, its identity and its run.invoker grant in place but paused — which is how ADR 0024's rollout wants it applied (the drain endpoint is exercised by hand first), and how the schedule is stopped during an incident without deleting anything."
  type        = bool
  default     = false
}

variable "reversal_reconciler_schedule" {
  description = "Unix cron for the drain tick. A minute because a Customer waiting on a refund they were told was 'processing' should not wait tens of minutes, and no faster because nothing here is sub-minute urgent. Changing it changes how quickly a stuck request is picked up, never whether it is: overlapping and skipped runs are both harmless for the reason reversal_reconciler.tf gives beside attempt_deadline, and per-request backoff lives in sale_reversals.next_attempt_at."
  type        = string
  default     = "* * * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.reversal_reconciler_schedule))
    error_message = "reversal_reconciler_schedule must be five space-separated cron fields, e.g. \"* * * * *\"."
  }
}

variable "reversal_reconciler_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one drain before abandoning it. It is the middle term of a chain — the backend's own drain budget must expire first, this second, api_request_timeout_seconds last — written down once, with what breaks when a term moves alone, in backend/internal/sales/service/reconciler.go beside reversalDrainBudget. Read that before moving this. A backend test reads this default and fails if the chain stops holding, which is the only place the three numbers are ever compared."
  type        = number
  default     = 90

  validation {
    # The floor is not Cloud Scheduler's own 15: it is the backend's drain budget
    # plus the provider timeout a probe already in flight can still be paying.
    # Sixty leaves room for both at their current values, and the backend test
    # named above is what actually holds the relationship — Terraform cannot read
    # a Go constant, so this bound stays deliberately loose rather than restating
    # a number that would go stale here.
    condition     = var.reversal_reconciler_attempt_deadline_seconds >= 60 && var.reversal_reconciler_attempt_deadline_seconds <= 1800
    error_message = "reversal_reconciler_attempt_deadline_seconds must be between 60 and 1800: at least 60 so it outlives the backend's drain budget plus a probe in flight, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Sale Invoice Drainer -----------------------------------------------------

variable "sale_invoice_drainer_enabled" {
  description = "Whether the Cloud Scheduler tick driving the Sale Invoice Drainer actually fires (#474, ADR 0060). False leaves the job, its identity and its run.invoker grant in place but paused, which is how it ships: it is enabled once an Issuer in `production` exists and the drain endpoint has been curled by hand. Until then the post-commit kick after every paid House checkout still works each document once, so the state machine is visible on the invoicing list; what stays unworked is only what the kick could not finish. Also the incident switch — set false and apply to stop the tick without deleting anything."
  type        = bool
  default     = false
}

variable "sale_invoice_drainer_schedule" {
  description = "Unix cron for the drain tick, read in America/Guayaquil. Every five minutes: the normal path is the kick right after checkout, so the tick only catches what that could not finish, and the backend's own ladder (1 min, 5 min, 15 min, then hourly in invoicing_invoices.next_attempt_at) is what says when a document is due — a tick this often means a document is never more than a few minutes later than its rung. Changing it changes how promptly a stuck document is picked up, never whether it is: overlapping and skipped runs are both harmless for the reason sale_invoice_drainer.tf gives beside attempt_deadline."
  type        = string
  default     = "*/5 * * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.sale_invoice_drainer_schedule))
    error_message = "sale_invoice_drainer_schedule must be five space-separated cron fields, e.g. \"*/5 * * * *\"."
  }
}

variable "sale_invoice_drainer_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one drain before abandoning it. The middle term of a chain — the backend's own drain budget must expire first, this second, api_request_timeout_seconds last — written down once, with what breaks when a term moves alone, in backend/internal/invoicing/service/drainer.go beside saleInvoiceDrainBudget. Read that before moving this. A backend test reads this default and fails if the chain stops holding."
  type        = number
  default     = 120

  validation {
    # The floor is the backend's drain budget plus the poll budget a document
    # already being submitted can still be paying; the ceiling is Cloud
    # Scheduler's own limit for an HTTP target. The backend test named above
    # is what holds the relationship exactly.
    condition     = var.sale_invoice_drainer_attempt_deadline_seconds >= 90 && var.sale_invoice_drainer_attempt_deadline_seconds <= 1800
    error_message = "sale_invoice_drainer_attempt_deadline_seconds must be between 90 and 1800: at least 90 so it outlives the backend's drain budget plus a submission in flight, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Follow Digest ------------------------------------------------------------
#
# Two jobs, two switches, two schedules and two deadlines (#226, ADR 0030). They
# are separate variables rather than one "follow_digest_enabled" because the two
# halves are paused independently: stopping the weekly enqueue leaves the drain
# to finish what is already queued, and stopping the drain during a mail-provider
# incident leaves the week's rows waiting rather than lost.

variable "follow_digest_enqueue_enabled" {
  description = "Whether the weekly Cloud Scheduler job actually fires. False leaves the job, its identity and its run.invoker grant in place but paused — which is how this is applied first (the endpoint is curled by hand before a cron drives it), and how a send is stopped without deleting anything. It is also the switch that matters most: this is the job that addresses every following Customer on the platform."
  type        = bool
  default     = false
}

variable "follow_digest_enqueue_schedule" {
  description = "Unix cron for the weekly enqueue, read in America/Guayaquil. Thursday 09:00: Thursday because a Digest lands before the weekend while the tickets it advertises are still buyable, and 09:00 because that is a morning rather than a notification in the night. Moving it moves when Customers are written to and nothing else — the drain sends whatever is queued, whenever it was queued."
  type        = string
  default     = "0 9 * * 4"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.follow_digest_enqueue_schedule))
    error_message = "follow_digest_enqueue_schedule must be five space-separated cron fields, e.g. \"0 9 * * 4\"."
  }
}

variable "follow_digest_enqueue_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for the weekly enqueue before abandoning it. The enqueue is one statement and has no budget of its own, so this is the innermost term there is; it must stay below api_request_timeout_seconds, which a backend test asserts (digest/service.TestTheDigestEnqueueDeadlineIsInsideTheRequestTimeout). Generous rather than tight: an abandoned run costs a week that was not declared, and one curl of the same URL completes it."
  type        = number
  default     = 120

  validation {
    condition     = var.follow_digest_enqueue_attempt_deadline_seconds >= 30 && var.follow_digest_enqueue_attempt_deadline_seconds <= 1800
    error_message = "follow_digest_enqueue_attempt_deadline_seconds must be between 30 and 1800: at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

variable "follow_digest_drain_enabled" {
  description = "Whether the per-minute drain tick fires. False leaves the job paused with its identity and grant intact. Pausing this is the move during a mail-provider incident: the week's Digests stay queued and go out when it is resumed, since nothing about a pending row expires except the week it is about."
  type        = bool
  default     = false
}

variable "follow_digest_drain_schedule" {
  description = "Unix cron for the drain tick. A minute, because the send is paced by the provider's rate limit rather than by this number: one run sends at most a batch, so the cadence is what decides how long a week's backlog takes to clear. Overlapping and skipped runs are both harmless for the reason follow_digest.tf gives beside attempt_deadline."
  type        = string
  default     = "* * * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.follow_digest_drain_schedule))
    error_message = "follow_digest_drain_schedule must be five space-separated cron fields, e.g. \"* * * * *\"."
  }
}

variable "follow_digest_drain_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one drain before abandoning it. It is the middle term of a chain — the backend's own drain budget must expire first, this second, api_request_timeout_seconds last — written down once, with what breaks when a term moves alone, in backend/internal/digest/service/service.go beside digestDrainBudget. Read that before moving this. A backend test reads this default and fails if the chain stops holding, which is the only place the three numbers are ever compared."
  type        = number
  default     = 90

  validation {
    # The floor is not Cloud Scheduler's own 15: it is the backend's drain budget
    # plus the mail provider timeout a send already in flight can still be paying.
    # Sixty leaves room for both at their current values, and the backend test
    # named above is what actually holds the relationship — Terraform cannot read
    # a Go constant, so this bound stays deliberately loose rather than restating
    # a number that would go stale here.
    condition     = var.follow_digest_drain_attempt_deadline_seconds >= 60 && var.follow_digest_drain_attempt_deadline_seconds <= 1800
    error_message = "follow_digest_drain_attempt_deadline_seconds must be between 60 and 1800: at least 60 so it outlives the backend's drain budget plus a send in flight, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Abandoned Answer Purge ---------------------------------------------------
#
# One job, one switch (#316, ADR 0044). Unlike the Follow Digest's pair there is
# nothing to split: the purge has no queue to fill and then drain, only a
# predicate.

variable "answer_purge_enabled" {
  description = "Whether the Abandoned Answer Purge tick actually fires. False leaves the job, its identity and its run.invoker grant in place but paused, which is how it ships. It stays off until TICKET_QUESTIONS_ENABLED has been open long enough for there to be Answers to purge (ADR 0045): before that this is a daily DELETE against an empty table, and turning it on is a separate decision from deploying it. It is also the first move if the purge is ever suspected of deleting more than it should — pause, then read, because the rows are gone."
  type        = bool
  default     = false
}

variable "answer_purge_schedule" {
  description = "Unix cron for the purge tick. Daily, in the small hours, because the retention promise is measured in days and gains nothing from being measured in minutes — a run deletes by age, so one a day is never behind. Deliberately NOT per-minute like the reversal drain: this is the only scheduled job in the deployment that deletes anything, and a bug in a job that fires 1,440 times a day empties the table before the first alert is read. Changing it changes how promptly the 30-day window is enforced, never whether it is: a missed day is caught up by the next run at no cost, because nothing accumulates."
  type        = string
  default     = "20 3 * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.answer_purge_schedule))
    error_message = "answer_purge_schedule must be five space-separated cron fields, e.g. \"20 3 * * *\"."
  }
}

variable "answer_purge_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one purge before abandoning it. Unlike the reversal drain and the Digest drain this run has no budget of its own to expire first — it is a single DELETE against a predicate — so this is not the middle term of a three-term chain but simply a bound below api_request_timeout_seconds. A backend test reads this default and fails if it ever rises above that timeout, because a deadline above it means Scheduler waiting on a request the platform has already abandoned."
  type        = number
  default     = 120

  validation {
    # The floor is Cloud Scheduler's own practical minimum plus room for one
    # statement against a table holding a month of abandoned checkouts. The
    # ceiling is Cloud Scheduler's own limit for an HTTP target.
    condition     = var.answer_purge_attempt_deadline_seconds >= 60 && var.answer_purge_attempt_deadline_seconds <= 1800
    error_message = "answer_purge_attempt_deadline_seconds must be between 60 and 1800: at least 60 so one DELETE has room to finish, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Answer Reminder ----------------------------------------------------------
#
# One job, one switch (#317, ADR 0044). Like the purge and unlike the Follow
# Digest there is nothing to split: the sweep has no queue to fill and then
# drain, it reads state and writes to whoever is owed a message.

variable "answer_reminder_enabled" {
  description = "Whether the Answer Reminder sweep tick actually fires. False leaves the job, its identity and its run.invoker grant in place but paused, which is how it ships. It stays off until TICKET_QUESTIONS_ENABLED has been open long enough for buyers to owe Answers (ADR 0045) — the backend's side reads that same flag and returns no candidates while it is closed, so both switches must be thrown deliberately. This is also the first move if the reminder is ever suspected of writing to the wrong people, or of writing too often: pause, then read, because a mail that has gone cannot be recalled."
  type        = bool
  default     = false
}

variable "answer_reminder_schedule" {
  description = "Unix cron for the sweep tick, read in America/Guayaquil. Daily at 10:00, which is a working morning rather than a notification in the night: this job puts messages in buyers' inboxes, so the hour it fires is the hour they arrive. Deliberately not per-minute like the reversal drain — but note that the cadence is NOT what limits how often a buyer is written to. At most one mail per Ticket Sale per 7 days and at most two ever are enforced in the backend against a ledger (catalog.MayRemind), so an hourly cron would still mail nobody twice. What this number changes is how promptly a new debt is chased and how large one run is."
  type        = string
  default     = "0 10 * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.answer_reminder_schedule))
    error_message = "answer_reminder_schedule must be five space-separated cron fields, e.g. \"0 10 * * *\"."
  }
}

variable "answer_reminder_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one sweep before abandoning it. It is the middle term of a chain — the backend's own run budget must expire first, this second, api_request_timeout_seconds last — written down once, with what breaks when a term moves alone, in backend/internal/sales/service/answerreminder.go beside answerReminderBudget. Read that before moving this. A backend test reads this default and fails if the chain stops holding, because a deadline that expires before the run's own budget abandons the request mid-send and loses the ledger write for a mail the provider already accepted, which costs a buyer a duplicate."
  type        = number
  default     = 120

  validation {
    # The floor is the backend's run budget plus the mail provider timeout a send
    # already in flight can still be paying. The ceiling is Cloud Scheduler's own
    # limit for an HTTP target. The relationship that matters is held by the
    # backend test named above — Terraform cannot read a Go constant — so this
    # bound stays deliberately loose rather than restating a number that would go
    # stale here.
    condition     = var.answer_reminder_attempt_deadline_seconds >= 90 && var.answer_reminder_attempt_deadline_seconds <= 1800
    error_message = "answer_reminder_attempt_deadline_seconds must be between 90 and 1800: at least 90 so it outlives the backend's 60s run budget plus a send in flight, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Assignment Reminder ------------------------------------------------------
#
# One job, one switch (#361, #365, ADR 0051), on the Answer Reminder's pattern
# and sharing its identity. The sweep reads state and writes to the buyer of
# every online Sale that still has a Ticket nobody holds.

variable "assignment_reminder_enabled" {
  description = "Whether the Assignment Reminder sweep tick actually fires. False leaves the job in place but paused, which is how it ships: merging mails nobody, and launching is an Operator's act made by setting this true and applying, then forcing one run by hand while watching (docs/runbook-assignment-reminder.md). The backend's side reads TICKET_ASSIGNMENT_ENABLED and returns no candidates while it is closed, so both switches must be thrown deliberately. This is also the first move if the reminder is ever suspected of writing to the wrong people or too often: pause, then read, because a mail that has gone cannot be recalled."
  type        = bool
  default     = false
}

variable "assignment_reminder_schedule" {
  description = "Unix cron for the sweep tick, read in America/Guayaquil. Daily at 10:30, half an hour after the Answer Reminder, so a buyer owed one of each receives two mails a little apart. The cadence is NOT what limits how often a buyer is written to: at most one mail per Ticket Sale per 7 days, at most two ever, and none in the Sale's first 24 hours are enforced in the backend against a ledger, so an hourly cron would still mail nobody twice."
  type        = string
  default     = "30 10 * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.assignment_reminder_schedule))
    error_message = "assignment_reminder_schedule must be five space-separated cron fields, e.g. \"30 10 * * *\"."
  }
}

variable "assignment_reminder_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one sweep before abandoning it. The middle term of the same chain the Answer Reminder keeps — the backend's own run budget first, this second, api_request_timeout_seconds last; read answer_reminder_attempt_deadline_seconds before moving it. A deadline that expires before the run's budget abandons the request mid-send and loses the ledger write for a mail already accepted, which costs a buyer a duplicate."
  type        = number
  default     = 120

  validation {
    condition     = var.assignment_reminder_attempt_deadline_seconds >= 90 && var.assignment_reminder_attempt_deadline_seconds <= 1800
    error_message = "assignment_reminder_attempt_deadline_seconds must be between 90 and 1800: at least 90 so it outlives the backend's run budget plus a send in flight, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}

# --- Holder Address Purge -----------------------------------------------------
#
# One job, one switch (#331, parent #322, ADR 0046). Like the Abandoned Answer
# Purge and unlike the Follow Digest there is nothing to split: the run has no
# queue to fill and then drain, it is one statement against a predicate.
#
# It is the SECOND job here that deletes, and the only one that deletes personal
# data belonging to somebody who never came to this platform. Read the three
# variables below beside answer_purge_*: the shape is identical on purpose, and
# every place the reasoning diverges is written down where it diverges.

variable "holder_address_purge_enabled" {
  description = "Whether the Holder Address Purge tick actually fires. False leaves the job, its identity and its run.invoker grant in place but paused, which is how it ships. It stays off until TICKET_ASSIGNMENT_ENABLED has been open long enough for Tickets to be carrying holder addresses (ADR 0045): before that this is a daily UPDATE matching no rows. Unlike the Answer Reminder sweep the backend does NOT refuse independently — the purge is deliberately not gated on the feature flag, because the switch that turns a deletion off must never be the switch that turns collection off — so this is the ONLY switch on this job. That cuts both ways. It is the first move if the purge is ever suspected of taking more than it should: pause, then read, because the addresses are gone. And it is a switch somebody must remember to throw ON once assignment opens, because an open assignment flag with this paused is the platform holding third-party contact details with no scheduled end."
  type        = bool
  default     = false
}

variable "holder_address_purge_schedule" {
  description = "Unix cron for the holder address purge tick, read in America/Guayaquil. Daily at 03:40, twenty minutes after the Abandoned Answer Purge so the two deletions are separate lines in the log and do not contend for the same table. Deliberately NOT per-minute like the reversal drain: the promise is \"when the Event starts\", and enforcing that within a minute rather than within a day is not worth a job that fires 1,440 times a day, because a bug in such a job empties the column before the first alert is read. The timezone this cron is read in changes nothing about WHICH Events qualify — events.starts_at is a TIMESTAMPTZ with the Event's own zone already inside it. Changing this changes how promptly an unaccepted address is taken, never whether it is: a missed day is caught up by the next run at no cost, because nothing accumulates."
  type        = string
  default     = "40 3 * * *"

  validation {
    condition     = can(regex("^\\S+( \\S+){4}$", var.holder_address_purge_schedule))
    error_message = "holder_address_purge_schedule must be five space-separated cron fields, e.g. \"40 3 * * *\"."
  }
}

variable "holder_address_purge_attempt_deadline_seconds" {
  description = "How long Cloud Scheduler waits for one holder address purge before abandoning it. Like the Abandoned Answer Purge and unlike the reversal drain and the Digest drain this run has no budget of its own to expire first — it is a single UPDATE against a predicate — so this is not the middle term of a three-term chain but simply a bound below api_request_timeout_seconds. A backend test reads this default and fails if it ever rises above that timeout, because a deadline above it means Scheduler waiting on a request the platform has already abandoned."
  type        = number
  default     = 120

  validation {
    # The floor is Cloud Scheduler's own practical minimum plus room for one
    # statement across tickets, their Sale Lines, their Sales and the Events.
    # The ceiling is Cloud Scheduler's own limit for an HTTP target.
    condition     = var.holder_address_purge_attempt_deadline_seconds >= 60 && var.holder_address_purge_attempt_deadline_seconds <= 1800
    error_message = "holder_address_purge_attempt_deadline_seconds must be between 60 and 1800: at least 60 so one UPDATE has room to finish, and at most 1800 because Cloud Scheduler rejects more for an HTTP target."
  }
}
