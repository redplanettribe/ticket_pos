# The keyless CI deploy path (#49, ADR 0007 decision 6).
#
# GitHub Actions authenticates to GCP with Workload Identity Federation, not a
# downloaded service account key. A JSON key in a GitHub secret is a permanent
# credential to the production project living in a third-party system; WIF hands
# out short-lived tokens instead and there is no long-lived secret anywhere.
#
# The whole security of this file rests on one thing: the federation must only
# ever mint tokens for THIS repository. Two independent controls enforce it, and
# both must be present — one alone is not enough:
#
#   1. The provider's `attribute_condition` — GitHub's OIDC token is only
#      accepted at all if it was issued to `redplanettribe/ticket_pos`. A token
#      from any other repo is rejected by STS before an SA is ever named.
#   2. The `roles/iam.workloadIdentityUser` binding's `principalSet` — only the
#      identity carrying `attribute.repository == redplanettribe/ticket_pos` may
#      impersonate the deploy SA.
#
# A condition scoped to the whole GitHub *account/org* (e.g. matching
# `assertion.repository_owner`), or omitted entirely, silently lets ANY
# repository — including a fork or a newly created repo under the same owner —
# impersonate the deploy identity. This is the single highest-risk detail in the
# ticket, so the repository slug is pinned in one place (var.github_repository),
# validated to be a single owner/repo with no wildcard, and reused by both
# controls below.

# --- Workload Identity Pool + GitHub OIDC provider ----------------------------

resource "google_iam_workload_identity_pool" "github" {
  project                   = var.project_id
  workload_identity_pool_id = "${var.environment}-github-actions"
  display_name              = "GitHub Actions (${var.environment})"
  description               = "Federates GitHub Actions OIDC tokens from ${var.github_repository} for keyless deploys."
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = var.project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-oidc"
  display_name                       = "GitHub OIDC"
  description                        = "GitHub Actions OIDC, restricted to ${var.github_repository}."

  # Only the claims that are actually used downstream are mapped. attribute.repository
  # is what both the condition below and the principalSet binding key off; mapping
  # more claims would be attack surface for no benefit.
  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
  }

  # SECURITY-CRITICAL. A GitHub OIDC token is accepted by this pool ONLY if its
  # `repository` claim is exactly this repo. `==` on the full `owner/repo` slug —
  # not `startsWith`, not a match on `repository_owner`, not omitted. Any other
  # repository's token is rejected here, before an SA is chosen.
  attribute_condition = "assertion.repository == '${var.github_repository}'"

  oidc {
    # GitHub's public OIDC issuer. google-github-actions/auth requests a token
    # from this issuer with the provider resource name as its audience (the
    # default), which is what makes `credentials_json` / a key file unnecessary.
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# --- Deploy service account ---------------------------------------------------

# The identity GitHub Actions impersonates. It is never the default compute
# account and holds no project-wide Owner/Editor: exactly enough to push images,
# roll revisions on the three services, and run the migrate Job — nothing that
# can read a secret, touch the database, or change IAM.
resource "google_service_account" "deploy" {
  project      = var.project_id
  account_id   = "${var.environment}-ticket-pos-deploy"
  display_name = "Ticket POS ${var.environment} deploy (GitHub Actions)"
  description  = "CI deploy identity, impersonated from ${var.github_repository} via Workload Identity Federation. No key exists."
}

# The impersonation grant. SECURITY-CRITICAL twin of the attribute_condition:
# the principalSet is scoped to `attribute.repository/<repo>`, NOT to the whole
# pool (`.../*`) and NOT to `attribute.repository_owner/<owner>`. Even if the
# condition above were ever loosened, this binding still refuses any principal
# whose repository attribute is not exactly this repo.
resource "google_service_account_iam_member" "deploy_wif" {
  service_account_id = google_service_account.deploy.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repository}"
}

# --- Least-privilege grants for the deploy SA ---------------------------------

# 1. Push images. artifactregistry.writer is scoped to THIS repository, not the
#    project: the deploy SA can push to ticket-pos and nowhere else, and cannot
#    delete the repository or change its IAM.
resource "google_artifact_registry_repository_iam_member" "deploy_push" {
  project    = var.project_id
  location   = google_artifact_registry_repository.images.location
  repository = google_artifact_registry_repository.images.name
  role       = "roles/artifactregistry.writer"
  member     = "serviceAccount:${google_service_account.deploy.email}"
}

# 2. Roll revisions and run the Job. roles/run.developer, NOT run.admin: the two
#    differ in exactly one permission that matters here — run.services.setIamPolicy.
#    Withholding it is what stops this identity from ever adding an `allUsers`
#    invoker binding and turning the IAM-locked API into a public service (ADR
#    0008). run.developer is the least-privileged predefined role that can update
#    services, update the Job image, and execute the Job.
#
#    Granted at PROJECT scope rather than per-resource. A resource-level binding
#    on each service would be tighter, but `gcloud run deploy` and `gcloud run
#    jobs execute` poll a long-running operation via run.operations.get, and
#    operations are a location-level collection — siblings of the services, not
#    children of them — so a role bound only to a service resource does not cover
#    the polling and the deploy fails partway. Project scope is the tightest
#    binding that reliably works; it still cannot touch IAM, secrets, the
#    database, or anything outside Cloud Run. (The actAs grants below, which DO
#    scope cleanly per-SA, are what keep it from deploying a service that runs as
#    an arbitrary identity.)
resource "google_project_iam_member" "deploy_run_developer" {
  project = var.project_id
  role    = "roles/run.developer"
  member  = "serviceAccount:${google_service_account.deploy.email}"
}

# 3. actAs on the runtime identities. Deploying (or updating the Job image on) a
#    Cloud Run resource that RUNS AS another service account requires
#    iam.serviceAccounts.actAs on that SA — without it, `gcloud run deploy`
#    returns PERMISSION_DENIED naming the runtime SA. Granted per runtime SA, so
#    the deploy identity can impersonate exactly these four workload identities
#    for deployment and nothing else; it cannot mint tokens as them or use them
#    to reach the database or secrets.
#
#    The migrate SA is included alongside the three the ticket names because the
#    pipeline runs `gcloud run jobs update --image` on the migrate Job, and
#    updating a Job that runs as the migrate SA needs actAs on it just as a
#    service deploy does. Four workloads are deployed, so four actAs grants.
resource "google_service_account_iam_member" "deploy_actas_api" {
  service_account_id = google_service_account.api.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_service_account_iam_member" "deploy_actas_staff" {
  service_account_id = google_service_account.staff.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_service_account_iam_member" "deploy_actas_storefront" {
  service_account_id = google_service_account.storefront.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}

resource "google_service_account_iam_member" "deploy_actas_migrate" {
  service_account_id = google_service_account.migrate.name
  role               = "roles/iam.serviceAccountUser"
  member             = "serviceAccount:${google_service_account.deploy.email}"
}
