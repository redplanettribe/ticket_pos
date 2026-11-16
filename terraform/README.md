# Terraform

Infrastructure for Ticket POS on GCP project `multiticketing` (number 706310105744), region
`us-east1`. The architecture and the reasoning behind it are in [docs/gcp-deployment.md](../docs/gcp-deployment.md)
and ADR [0007](../docs/adr/0007-deploy-on-cloud-run-with-cloud-sql.md).

```
terraform/
├── bootstrap/            # run once, local state: creates the state bucket
├── modules/ticket-pos/   # every resource, parameterised — knows nothing about prod
└── envs/prod/            # thin root: project, region, state prefix
```

## Why bootstrap is separate

Terraform stores its state in a GCS bucket, and Terraform cannot create the bucket that holds its own
state — the backend must already exist when `terraform init` runs. So bucket creation is a separate
configuration with **local state**, applied once by hand. `envs/prod` then points its `backend "gcs"`
block at the bucket that bootstrap made.

The bucket is **versioned**. That is the whole reason this configuration is worth having rather than a
`gsutil mb` in someone's shell history: a corrupted or half-written state object can be restored from
the previous generation. Without versioning the only recovery is rebuilding state by hand against the
live project.

Bootstrap's own state file is local and is **not** committed (it is gitignored — state may contain
secrets). Losing it is survivable: the bucket carries `prevent_destroy` and `force_destroy = false`,
and re-adopting it is a single `terraform import`.

## Running it

Prerequisites: `terraform` (~> 1.13) and `gcloud`, authenticated with rights on the project.

```bash
gcloud auth application-default login
gcloud config set project multiticketing
```

`envs/prod` also manages Amazon SES in the AWS member account (see [Email](#email)), so a plan needs the `aws` CLI signed in to that account's IAM Identity Center profile and the budget alert address in the environment:

```bash
aws sso login --profile multiticketing-ses
export TF_VAR_aws_budget_alert_email=...   # from the sourced, gitignored .env
```

It also manages the ticket-pos side of Sentry (see [Sentry](#sentry)), so a plan needs the Sentry token in the environment too:

```bash
export TF_VAR_sentry_auth_token=...        # from the sourced, gitignored .env
```

### 1. Bootstrap — once per project, ever

```bash
cd terraform/bootstrap
terraform init
terraform apply          # creates gs://multiticketing-tfstate
```

If the bucket already exists (someone ran this before, or the local state was lost):

```bash
terraform import google_storage_bucket.terraform_state multiticketing/multiticketing-tfstate
terraform plan           # expect no changes
```

### 2. Production

```bash
cd terraform/envs/prod
terraform init           # reads state from gs://multiticketing-tfstate/envs/prod
terraform plan
terraform apply
```

A second `terraform plan` immediately after apply must report no changes.

> **Build and push the API image before the first apply.** The Cloud Run service and
> the migrate Job are created pointing at `…/ticket-pos/api:latest`, and Cloud Run
> refuses to create a service whose image does not exist. Run step 1 of
> [Deploying by hand](#deploying-by-hand) — through `docker push` — first. The
> Artifact Registry repository is created by the same `terraform apply`, so the very
> first time round the order is: `terraform apply -target=module.ticket_pos.google_artifact_registry_repository.images`,
> push, then the full apply.

### Adding an environment later

Copy `envs/prod` to `envs/<name>`, change the backend `prefix` and the `environment` argument. The
module takes no `prod`-specific input, so nothing else moves.

## Conventions

- **Versions are pinned.** Roots pin Terraform `~> 1.13` and the Google provider `~> 6.0` (and, in
  `envs/prod`, the AWS provider `~> 6.0` and the Sentry provider `~> 0.16.0`); the module
  states looser constraints because the root is what decides. Run `terraform init` and commit the
  generated `.terraform.lock.hcl` in each root so provider hashes are locked too.
- **Nothing environment-specific inside the module.** Variables in, outputs out. If a resource needs to
  know it is production, that is a variable.
- **`terraform.tfvars` is committed** in each root. It holds project and region, which are not secrets.
  Secrets belong in Secret Manager, never in a `.tf` or `.tfvars` file.

## The data layer

`envs/prod` creates the image registry and everything the application stores state in.

**Network.** A VPC with one subnet. It exists so Cloud SQL can have a private address: a private-IP
instance lives in a Google-managed network peered to ours, and the peering needs a range reserved on
our side first (`10.240.0.0/24`). Private services access needs no subnet of its own; the single
subnet, `10.241.0.0/24`, is what Cloud Run attaches to for direct VPC egress. The two ranges must not
overlap, and neither can be narrowed after creation.

**Cloud SQL.** Postgres 18, `db-g1-small`, private IP only, single zone. Daily backups at 05:00 UTC with
seven kept, plus point-in-time recovery over a seven-day window of write-ahead logs. Deletion is blocked
twice: `deletion_protection` stops `terraform destroy`, and `settings.deletion_protection_enabled` stops
gcloud and the console. Both must be flipped off, in two applies, before the instance can go.

Single-zone is deliberate and recorded in ADR 0007. Regional HA is `availability_type = "REGIONAL"` in
`cloud_sql.tf` — an in-place update, not a recreate, and roughly double the bill.

**Bucket.** One public-read bucket for `covers/`, `logos/`, and `avatars/`. Two things about it matter:

- *Everything in it is world-readable to anyone with the URL, forever.* The `allUsers` grant is
  bucket-wide and uniform access gives no per-object escape. Nothing private goes here.
- *The CORS rule is load-bearing.* The API hands the browser a presigned PUT URL and the browser uploads
  directly, cross-origin — from Staff for covers and logos, from the Storefront for Customer Avatars.
  MinIO is permissive by default, so a missing origin here is invisible locally and surfaces in production
  as an opaque browser network error on every image upload. The origins come from `staff_origin` and
  `storefront_origin` in `envs/prod/terraform.tfvars`; a new browser surface that uploads needs its origin
  added to both.

Access is via the S3-compatible API with an HMAC key, so `platform/storage` runs the same code against
GCS and MinIO (ADR 0007). The key belongs to a dedicated service account holding `objectAdmin` on this
bucket only.

### Secrets

Three, all generated by Terraform and readable only from Secret Manager:

| Secret | Holds |
|---|---|
| `prod-ticket-pos-db-password` | Generated database password |
| `prod-ticket-pos-s3-access-key` | HMAC access ID |
| `prod-ticket-pos-s3-secret-key` | HMAC secret |

```bash
gcloud secrets versions access latest --secret=prod-ticket-pos-db-password
```

No secret value is a Terraform output, and no `.tf` or `.tfvars` file contains one. They do exist in
Terraform state, which is why the state bucket is private and versioned — treat state as a credential.

A fourth, `prod-ticket-pos-database-url`, holds the whole connection string —
`postgres://user:password@<private-ip>:5432/ticket_pos?sslmode=require`. The application reads one
variable, `DATABASE_URL`, and the two halves it would otherwise be assembled from are awkward in
opposite ways: the password must not sit in a Cloud Run env var, and the private IP is not known until
the instance exists. Assembling it in Terraform means Cloud Run mounts one secret and no plaintext
credential appears in the service definition.

Both runtime service accounts are granted `secretAccessor` **per secret**, never project-wide. The API
reads three of the four; the migrate Job reads only the connection string.

A `prod-ticket-pos-confirmation-link-secret` signs Confirmation Links (`confirmation_link.tf`), and a
`prod-ticket-pos-invoicing-certificate-key` — 32 random bytes as base64 — is what the Issuer's signing
certificate is AES-256-GCM encrypted under in Postgres (`invoicing_certificate.tf`, ADR 0059). The API
reads both; only the latter is optional to it, and rotating it makes the stored certificate unreadable
until it is uploaded again.

**Rotating the HMAC key** is manual: `terraform taint module.ticket_pos.google_storage_hmac_key.app`,
apply, redeploy the API so it picks up the new version. Automating it needs a two-key overlap the
application cannot express with one credential pair.

## The API service and the migrate Job

`cloud_run.tf` creates both, from one image built by `backend/Dockerfile`, which ships `/server` and
`/migrate` side by side. The service runs the Dockerfile's default entrypoint; the Job overrides
`command` to `/migrate`. Same image, same digest, different entrypoint.

**Migrations are a Job, not a boot step.** `migrate.Up` takes no advisory lock, and Cloud Run starts
instances concurrently, so boot-time migration is a race between instances. A failing migration at boot
is also a crash-looping revision instead of a failed job with a readable log. The service therefore runs
with `APP_ENV=production` and `RUN_MIGRATIONS` unset, which is what makes `shouldRunMigrations()` in
`backend/internal/platform/config.go` return false.

The discipline this creates: the Job runs **before** the new revision is deployed, so every migration
must be backward-compatible with the revision currently serving traffic. Expand and contract — a release
stops using a column, a later release drops it.

**Authentication.** The service is deployed with unauthenticated invocation *disabled*, which in Cloud
Run v2 is not a flag but the absence of an `allUsers` binding on `roles/run.invoker`. There is no such
binding anywhere in this module, so Cloud Run rejects every request that does not carry a Google-signed
OIDC ID token before the container is reached (ADR 0008). Ingress remains open to the internet on
purpose: internal-only ingress would force callers to route all traffic through the VPC, which needs
Cloud NAT at ~$35/month. **Granting `run.invoker` to the Staff and Storefront services is #48** — until
that lands, nothing but a developer's own `gcloud auth print-identity-token` can call the API.

**Identity.** Two dedicated service accounts, never the default compute account (which is project Editor):

| | `prod-ticket-pos-api` | `prod-ticket-pos-migrate` |
|---|---|---|
| `roles/cloudsql.client` (project) | yes | yes |
| `roles/storage.objectAdmin` on the media bucket | yes | no |
| `secretAccessor` on `…-database-url` | yes | yes |
| `secretAccessor` on the two S3 key secrets | yes | no |

**Reaching the database.** Cloud SQL has no public IP, so both workloads use *direct VPC egress* into
`prod-ticket-pos-run` (`10.241.0.0/24`) and connect to the instance's private address. Direct egress
rather than a Serverless VPC Access connector: a connector is a pair of always-on VMs at roughly
$10/month, direct egress costs nothing. `egress = "PRIVATE_RANGES_ONLY"` sends only RFC1918 destinations
into the VPC, so Secret Manager, Artifact Registry and `storage.googleapis.com` are still reached
directly and no Cloud NAT is needed.

**Terraform does not own which image is running.** The `image` field on both resources is in
`ignore_changes`. Terraform creates the service and the Job; deploys roll new revisions. Without this,
the next `terraform apply` would roll production back to whatever tag was last written in the
configuration, and `terraform plan` would never be clean after a deploy. Changing `api_image_tag`
therefore does not deploy anything — use the sequence below.

## Deploying

A push to `main` deploys the system automatically: `.github/workflows/deploy.yml` builds all three
images, runs the migrate Job to completion, and then rolls the new revisions. It authenticates with
**Workload Identity Federation** — no service account key exists anywhere. The manual sequence below is
what the workflow automates step-for-step, kept for a break-glass deploy from a laptop.

### The keyless CI path (#49)

`terraform/modules/ticket-pos/deploy_ci.tf` provisions a Workload Identity Pool, a GitHub OIDC provider,
and a dedicated deploy service account (`prod-ticket-pos-deploy`). The security of the whole thing is one
property: **only this repository may impersonate the deploy identity.** Two independent controls enforce
it, and both name `redplanettribe/ticket_pos` explicitly:

- the provider's attribute condition `assertion.repository == 'redplanettribe/ticket_pos'` — a token from
  any other repo is rejected by STS before a service account is chosen;
- the impersonation binding's principalSet `…/attribute.repository/redplanettribe/ticket_pos` — scoped to
  the repo attribute, not the whole pool and not the owner.

A condition scoped to the org, or omitted, would let any repository under the owner mint deploy tokens.
The repo slug is pinned once in `var.github_repository` and reused by both, and validated to be a single
`owner/repo` with no wildcard.

The deploy SA holds exactly: `artifactregistry.writer` on the images repo, `run.developer` (not
`run.admin` — it cannot set IAM policy, so it can never make the API public) on the three services and the
migrate Job, and `serviceAccountUser` on the four runtime SAs (needed to deploy a service or update a Job
that *runs as* another SA). No project-wide Owner/Editor, no secret access, no database reach.

The two values the workflow needs are Terraform outputs, neither secret:

```bash
terraform -chdir=envs/prod output workload_identity_provider
terraform -chdir=envs/prod output deploy_service_account_email
```

### Sentry releases and source maps (#776)

A release in Sentry is a commit (ADR 0079).
The workflow builds all three images with `--build-arg SENTRY_RELEASE=<sha>`, the release each deployable reports.

With one GitHub Actions secret it also tells Sentry about the release:

- **Name:** `SENTRY_AUTH_TOKEN`, a repository secret of `redplanettribe/ticket_pos`.
- **What it is:** an organization auth token of the `redplanettribe` Sentry organization (Settings, Developer Settings, Organization Tokens).
  Its fixed scope, `org:ci`, is the release scope: it can create releases and deploys and upload source maps, and read nothing else.
  A personal token would need `project:releases` (and `org:read`); prefer the organization token, which outlives any one person.
- **Who creates it:** the operator, by hand. It is never committed, and no agent creates it.

With the secret set:

- The Storefront and Staff builds receive it as a BuildKit secret (never a build argument, so it is in no layer or image), produce source maps, upload them to `ticket-pos-storefront` and `ticket-pos-staff`, and delete them.
- After the migration, `scripts/sentry-release.sh create` creates the release in all three projects and finalizes it.
- After each service's deploy, `scripts/sentry-release.sh deploy` marks a deploy of it to `production`, named after the service.

Every one of these fails soft.
An unreachable Sentry or a wrong token is a warning in the job's log and annotations, and the build and the deploy go on; that release's browser stack traces stay minified.
A Sentry that accepts the connection and never answers costs time, not the deploy: the upload's CLI gives up on its own (about 90 seconds per attempt, three attempts per Next build, seen locally), and each release step is bounded by a timeout.

Without the secret the Sentry steps are skipped, the Next builds produce no browser source maps and contact nothing, and the deploy is what it was before, apart from the release build argument.
Either way, a Next image that holds any `.map` file fails its own build: no source map is ever served.

`make deploy` does the same when `SENTRY_AUTH_TOKEN` is exported in the operator's shell.

### Rolling back

Every image is tagged with its commit SHA, never `latest` — that is what makes a rollback one command.
Redeploy a previous known-good SHA (find it in the Actions history or `git log`):

```bash
gcloud run deploy prod-ticket-pos-api \
  --region us-east1 \
  --image us-east1-docker.pkg.dev/multiticketing/ticket-pos/api:<good-sha>
```

Repeat for `prod-ticket-pos-staff` / `prod-ticket-pos-storefront` as needed. No Terraform change is
involved (image is in `ignore_changes`). Rolling a *migration* back is not a command — expand-and-contract
is what buys the ability to redeploy old code against the newer schema.

### Deploying by hand

A break-glass deploy is these four steps, in this order. Run them from the repository root.

```bash
PROJECT=multiticketing
REGION=us-east1
IMAGE="$REGION-docker.pkg.dev/$PROJECT/ticket-pos/api"
TAG=$(git rev-parse --short HEAD)     # never `latest` for a real deploy: an
                                      # immutable tag is what makes a rollback
                                      # a one-line command

gcloud auth login
gcloud config set project "$PROJECT"
gcloud auth configure-docker "$REGION-docker.pkg.dev"
```

**1. Build and push.** Cloud Run runs linux/amd64; an Apple Silicon machine builds arm64 by default and
the mismatch surfaces as a container that will not start.

```bash
docker build --platform linux/amd64 -t "$IMAGE:$TAG" backend
docker push "$IMAGE:$TAG"
```

**2. Migrate.** Before the new revision exists, so the schema is ready for it and still valid for the
revision currently serving.

```bash
gcloud run jobs update prod-ticket-pos-migrate \
  --region "$REGION" --image "$IMAGE:$TAG"

gcloud run jobs execute prod-ticket-pos-migrate \
  --region "$REGION" --wait
```

`--wait` is the point of the step: it exits non-zero if the migration failed. Stop here if it does. The
API is still on its old revision against a schema that still matches it, and the error is in the
execution's logs:

```bash
gcloud run jobs executions list --job prod-ticket-pos-migrate --region "$REGION" --limit 1
gcloud logging read \
  'resource.type=cloud_run_job AND resource.labels.job_name=prod-ticket-pos-migrate' \
  --limit 50 --format='value(textPayload)'
```

**3. Deploy the API.**

```bash
gcloud run deploy prod-ticket-pos-api \
  --region "$REGION" --image "$IMAGE:$TAG"
```

Everything else about the service — service account, secrets, VPC egress, scaling — comes from
Terraform and must not be passed here. A `gcloud run deploy` flag that contradicts the configuration
wins until the next apply, at which point it silently reverts.

**4. Verify.** The service rejects anonymous callers, so a bare `curl` returning 403 is the *correct*
answer, not a failure:

```bash
API_URL=$(gcloud run services describe prod-ticket-pos-api \
  --region "$REGION" --format='value(status.url)')

curl -sS -o /dev/null -w '%{http_code}\n' "$API_URL/health"
# 403 — Cloud Run rejected it before the container saw it

curl -sS -H "Authorization: Bearer $(gcloud auth print-identity-token)" "$API_URL/health"
# 200 with the health payload
```

`gcloud auth print-identity-token` works because your own account has `run.invoker` through project
ownership. It is the same token shape the frontends will present once #48 grants them the role.

**Rolling back** is step 3 with the previous tag; no Terraform change is involved. Rolling *migrations*
back is not a command — that is what the expand-and-contract rule above buys.

## The Reversal Reconciler schedule

`reversal_reconciler.tf` creates a Cloud Scheduler job that POSTs
`/api/v1/internal/reversals/drain` on the API about once a minute, a dedicated service account
(`prod-ticket-pos-reversals`), and that account's `run.invoker` grant on the API — the third and last
principal holding that role. It is the first scheduled execution in this deployment; ADR 0024 has the
reasoning, including why an in-process ticker cannot work against `min_instances = 0` and
`cpu_idle = true`.

**It needs `cloudscheduler.googleapis.com` enabled on the project** (`gcloud services enable
cloudscheduler.googleapis.com`). Enabling the API also creates the Cloud Scheduler service agent and
grants it the token-creator role it uses to mint the OIDC token as the account above; an OIDC failure
that looks like a broken `run.invoker` grant is usually that agent instead.

**It ships paused.** `reversal_reconciler_enabled` defaults to `false` in both the module and
`envs/prod`, because the rollout is to prove the endpoint by hand against production first:

```bash
API_URL=$(gcloud run services describe prod-ticket-pos-api \
  --region us-east1 --format='value(status.url)')

curl -sS -X POST -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  "$API_URL/api/v1/internal/reversals/drain"
# a small JSON summary; on an empty queue it does nothing, which is the point
```

That same curl is how reconciliation is driven manually during an incident. So is
`gcloud scheduler jobs run prod-ticket-pos-reversal-reconciler --location us-east1`, which fires one
tick even while the job is paused.

**Pausing.** The Terraform-owned switch, which is the one to use:

```bash
terraform -chdir=envs/prod apply -var reversal_reconciler_enabled=false
```

Faster, for the first minute of an incident:

```bash
gcloud scheduler jobs pause prod-ticket-pos-reversal-reconciler --location us-east1
```

A console or gcloud pause is real but not recorded: the next `terraform apply` resumes it. Follow it
with the variable change, or the tick returns without anyone deciding it should.

Pausing stops the platform pursuing stuck Reversal Requests; it does not strand them. Nothing expires
because the tick stopped — the Customer Area still drains a buyer's own request opportunistically, and
each request keeps its own `next_attempt_at`, so resuming picks up exactly where it left off.

Neither a failed nor an overlapping run can double-refund, for the reason `reversal_reconciler.tf`
gives beside `attempt_deadline`: correctness rests on the per-sale advisory lock, never on the
schedule. That is also why the job retries zero times — the next tick is a better retry, and
per-request backoff lives in `sale_reversals.next_attempt_at` where it can be read in SQL.

**The attempt deadline is not a tuning knob on its own.** `reversal_reconciler_attempt_deadline_seconds`
is the middle term of a chain: the backend's own drain budget expires first, Scheduler's deadline
second, `api_request_timeout_seconds` last. Each term is worthless without the ones around it, and the
chain — the numbers included — is written down once in
`backend/internal/sales/service/reconciler.go` beside `reversalDrainBudget`, where a backend test reads
both Terraform defaults and fails if the three stop agreeing. Read it before changing any of them.

## The Follow Digest schedule

`follow_digest.tf` creates the platform's **second** scheduled execution (ADR 0030), following the
reconciler above line for line: two Cloud Scheduler jobs, a dedicated service account per job, an
OIDC token, and a `run.invoker` grant per account on the API and nothing else.

| Job | Schedule | Endpoint |
| --- | --- | --- |
| `prod-ticket-pos-follow-digest-enqueue` | Thursday 09:00 `America/Guayaquil` | `POST /api/v1/internal/follow-digests/enqueue` |
| `prod-ticket-pos-follow-digest-drain` | every minute | `POST /api/v1/internal/follow-digests/drain` |

The weekly job declares the week and creates one pending Digest per eligible Customer; the drain
paces the actual sending inside the mail provider's rate limit, and prunes the sent-ledger's dead
rows at its tail. Neither decides anything the other does — what is owed to whom lives in
`follow_digests` and can be read in SQL.

**Two service accounts, not one.** `prod-ticket-pos-digest-enq` and `prod-ticket-pos-digest-drain`.
The audit log has to be able to say which of the two called, and revoking the weekly job in an
incident must not also stop the drain from finishing what is already queued.

**It needs `cloudscheduler.googleapis.com`**, as the reconciler does, and the same note about the
Cloud Scheduler service agent applies to an OIDC failure here.

**Both ship paused**, and this one has a prerequisite beyond confidence in the code: **the Digest
sends from its own domain** (ADR 0030, #225), which must exist at the mail provider with its DNS
records published before `follow_digest_enqueue_enabled` is set to `true`. A deployment without it
composes every Digest and refuses every send —
`platform.UnconfiguredDigestSender` logs each refusal at error level — so the whole week's queue
retries and is then abandoned. **Provision the domain first; turning these jobs on does not by
itself put a Digest in anybody's inbox.**

Proving it by hand, before any cron drives it:

```bash
API_URL=$(gcloud run services describe prod-ticket-pos-api \
  --region us-east1 --format='value(status.url)')

# safe on an empty queue, and the ordinary answer is zeros
curl -sS -X POST -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  "$API_URL/api/v1/internal/follow-digests/drain"

# declares THIS week; idempotent, so a second call reports already_enqueued
curl -sS -X POST -H "Authorization: Bearer $(gcloud auth print-identity-token)" \
  "$API_URL/api/v1/internal/follow-digests/enqueue"
```

**Pausing.** The Terraform-owned switches, which are the ones to use:

```bash
terraform -chdir=envs/prod apply -var follow_digest_enqueue_enabled=false
terraform -chdir=envs/prod apply -var follow_digest_drain_enabled=false
```

Faster, for the first minute of an incident, and subject to the same caveat as above — a gcloud
pause is real but unrecorded, and the next apply resumes it:

```bash
gcloud scheduler jobs pause prod-ticket-pos-follow-digest-drain --location us-east1
```

Which one to pause depends on what is wrong. Pausing the **drain** during a mail-provider incident
holds the week's Digests in the queue rather than spending their five attempts against a provider
that is refusing them; nothing expires because the tick stopped. Pausing the **enqueue** stops next
week's send without touching this week's.

**The drain's attempt deadline is not a tuning knob on its own.**
`follow_digest_drain_attempt_deadline_seconds` is the middle term of the same three-term chain the
reconciler has — the backend's drain budget expires first, Scheduler's deadline second,
`api_request_timeout_seconds` last — written down once in
`backend/internal/digest/service/service.go` beside `digestDrainBudget`, where a backend test reads
the Terraform defaults and fails if the three stop agreeing.

## Email

Two providers, two sending identities (ADR [0009](../docs/adr/0009-transactional-email-via-resend.md), [0030](../docs/adr/0030-follows-notify-as-a-weekly-digest.md), [0077](../docs/adr/0077-ses-stands-beside-resend-dormant-behind-an-explicit-provider-switch.md)).
Resend carries all mail today.
Amazon SES is provisioned beside it, dormant, on the same two sending subdomains: `send.multiticketing.com` for transactional mail and `digest.multiticketing.com` for the Follow Digest.

**The setup wizard.**
The steps only a human can perform (the AWS Organization and member account, the IAM Identity Center profile, the apply, the DNS records at Namecheap, verification of both identities, the send check against a sandbox-verified address, and the request for production access) are walked in order by the wizard:

```bash
make ses-setup-wizard                       # from step 1
make ses-setup-wizard ARGS='-from-step 5'   # resume at a step
```

Each step says what to do and how to tell it worked, and the wizard checks it with the AWS CLI or Terraform where it can: the profile reaches the AWS member account whose ID you give, both identities are verified, and the send check exits 0.
It refuses to move on while a check fails.
It writes no file: the budget alert address comes from `TF_VAR_aws_budget_alert_email` in the environment, and the send check's recipient is typed at a prompt.
`make ses-setup-wizard ARGS=-help` lists its flags (`-from-step`, `-profile`, `-region`, `-account`, `-terraform-dir`, `-email-from`, `-digest-email-from`).
Nothing it does switches a sending identity.

| Setting | Cloud Run env | Default |
| --- | --- | --- |
| `email_provider` | `EMAIL_PROVIDER` | `resend` |
| `digest_email_provider` | `DIGEST_EMAIL_PROVIDER` | `resend` |

Switching an identity to SES is setting one of these to `ses` and applying; rolling back is setting it back.
The two switch independently, in either direction.
On `ses` the Follow Digest still sends as `DIGEST_EMAIL_FROM` on its own subdomain and needs no Digest Resend key.
The SES settings (`SES_REGION`, `SES_CONFIGURATION_SET`, `AWS_ROLE_ARN`, `SES_WEB_IDENTITY_AUDIENCE`) are always mounted, and the API reads them only for an identity that selects `ses`.

**Resend** (`email.tf`).
The Resend domains and their records were added by hand; Terraform holds only the API keys (`TF_VAR_resend_api_key`, `TF_VAR_digest_resend_api_key`) and prints the Digest domain's checklist as `terraform output digest_email_dns_setup`.
On each subdomain Resend publishes a DKIM TXT at `resend._domainkey.<domain>` and its return path at `send.<domain>` (an MX to `feedback-smtp.<region>.amazonses.com` and an SPF TXT).

**SES** (`ses.tf`).
Everything on the AWS side lives in a dedicated member account of an AWS Organization, in `us-east-1`, reached through the `aws_profile` IAM Identity Center profile.
The Organization and the account are created by hand, never in Terraform.
`ses.tf` creates:

- an email identity per sending subdomain with Easy DKIM, each defaulting to the configuration set;
- a custom MAIL FROM per identity on `bounce.<domain>` (`ses_mail_from_label`);
- the configuration set `prod-ticket-pos`, with no event destination and engagement tracking disabled, so open and click tracking stay off;
- account-level suppression for `BOUNCE` and `COMPLAINT`;
- the role `prod-ticket-pos-api-ses`, which the API assumes with a Google-signed ID token, so no AWS key is stored anywhere;
- a monthly AWS Budget on the member account (`aws_budget_limit_usd`, 10 USD by default) that emails `TF_VAR_aws_budget_alert_email` at 80% spent and at a forecast over 100%.

The role's trust accepts `accounts.google.com` only when `sub` and `aud` (Google's `azp`) are the API service account's unique ID, read from `google_service_account.api`, and `oaud` is the audience the API requests.
Its policy allows only `ses:SendEmail` on the two identities and the configuration set.
The budget alert address has no default and no committed value, so a plan without it stops and asks.

**DNS for SES.**
`terraform output -raw ses_dns_setup` prints every record to add at Namecheap, per subdomain:

| Record | Name | Value |
| --- | --- | --- |
| CNAME x3 | `<token>._domainkey.<domain>` | `<token>.dkim.amazonses.com` |
| MX | `bounce.<domain>` | `10 feedback-smtp.us-east-1.amazonses.com` |
| TXT | `bounce.<domain>` | `v=spf1 include:amazonses.com ~all` |

They sit beside Resend's and replace none of them.
SES's DKIM selectors are random tokens, so they never meet `resend._domainkey`, and its return path is `bounce.<domain>`, never Resend's `send.<domain>`.
DMARC needs nothing new: `_dmarc.multiticketing.com` already covers both subdomains, and SES mail aligns with it through DKIM and the MAIL FROM.
Until the records are in and SES reads both identities as verified, SES refuses to send from them; nothing sends through SES before a provider variable says `ses` anyway.

## Sentry

The ticket-pos side of Sentry is declared in `modules/ticket-pos/sentry.tf` (ADR [0079](../docs/adr/0079-sentry-reports-errors-traces-and-job-check-ins-behind-one-seam-holding-no-personal-data-and-dormant-until-a-dsn-is-set.md), #777), with the community provider `jianyuan/sentry`.
It lives in the existing `redplanettribe` organization, which is shared with another product.
Nothing here reads, imports or changes the organization's settings or any project but the three below; the other product's `m3t-*` projects are never referenced.

**The inputs.**

| Input | Where it comes from | Default |
| --- | --- | --- |
| `sentry_auth_token` | `TF_VAR_sentry_auth_token`, from the sourced, gitignored `.env` | none: a plan without it stops and asks |
| `sentry_api_enabled` | `terraform.tfvars`, when the operator writes it | `false` |
| `sentry_staff_enabled` | `terraform.tfvars`, when the operator writes it | `false` |
| `sentry_storefront_enabled` | `terraform.tfvars`, when the operator writes it | `false` |

The token is the operator's and is never committed.
It is not the `SENTRY_AUTH_TOKEN` GitHub Actions secret of [Sentry releases and source maps](#sentry-releases-and-source-maps-776): that one is an organization token whose fixed `org:ci` scope can only make releases.
This one creates a team, projects, keys, filters and alert rules, so it needs Organization Read, Team Admin, Project Admin and Alerts Read & Write.
Prefer an internal integration's token (Settings, Developer Settings, Custom Integrations, a new internal integration with those permissions), which outlives any one person; a personal token needs `org:read`, `team:admin`, `project:admin` and `alerts:write`.
Every plan now needs it, because the Sentry resources are always declared.

**The switches.**
Each deployable reports only when its switch is on, and they are independent, so the API can go first.
Off, the service has no `SENTRY_DSN`, which is what dormant means: the API builds no reporter and the Next apps initialise no SDK.
On, the service gets its project's public DSN as a plain environment value (a DSN is not a secret), and the Staff app and the Storefront also get `SENTRY_ENVIRONMENT=production`; the API reports as `production` because its `APP_ENV` is.
Turning one on is an apply that changes that service's environment and so rolls a new revision of it, and no other.
Turn any of them on only after the policy edition that names Sentry is published (ADR 0079); each is a gate, absent from `terraform.tfvars` until somebody writes it.

**What a first apply creates**, with every switch off (19 to add, nothing to change or destroy):

| Resource | What it is |
| --- | --- |
| `module.ticket_pos.sentry_team.ticket_pos` | the `ticket-pos` team |
| `module.ticket_pos.sentry_project.deployable["api"]` | `ticket-pos-api`, platform `go` |
| `module.ticket_pos.sentry_project.deployable["staff"]` | `ticket-pos-staff`, platform `javascript-nextjs` |
| `module.ticket_pos.sentry_project.deployable["storefront"]` | `ticket-pos-storefront`, platform `javascript-nextjs` |
| `module.ticket_pos.sentry_key.deployable["api"]` | the API's key, `prod-ticket-pos-api` |
| `module.ticket_pos.sentry_key.deployable["staff"]` | the Staff app's key, `prod-ticket-pos-staff` |
| `module.ticket_pos.sentry_key.deployable["storefront"]` | the Storefront's key, `prod-ticket-pos-storefront`, rate limited |
| `module.ticket_pos.sentry_project_inbound_data_filter.storefront_browser_extensions` | Storefront: browser extensions filtered |
| `module.ticket_pos.sentry_project_inbound_data_filter.storefront_web_crawlers` | Storefront: web crawlers filtered |
| `module.ticket_pos.sentry_project_inbound_data_filter.storefront_legacy_browsers` | Storefront: legacy browsers filtered |
| `module.ticket_pos.sentry_alert.new_issue["api"]`, `["staff"]`, `["storefront"]` | a new issue in production |
| `module.ticket_pos.sentry_alert.regression["api"]`, `["staff"]`, `["storefront"]` | a resolved issue happens again in production |
| `module.ticket_pos.sentry_alert.error_spike["api"]`, `["staff"]`, `["storefront"]` | more than 50 errors in an hour in production, across the project |

The plan also reads `data.sentry_project_error_monitor.deployable` for each project, the default error monitor Sentry creates with a project, which the alert rules attach to; it creates nothing.
If the plan shows any change to a Cloud Run service while every switch is off, stop: it should show none.

The projects are created without Sentry's default key and default alert rule, so the only key and rules are the ones above.
The three alert rules on each project email the `ticket-pos` team, at most every 30 minutes per issue for a new issue or a regression and every 60 for a spike.
The Storefront key accepts 500 events an hour (`sentry_storefront_rate_limit_count`, `sentry_storefront_rate_limit_window_seconds` in the module); Sentry enforces per-key rate limits only on a plan that has them, so apply after the plan upgrade.
The legacy browser filter takes every current subfilter (Internet Explorer, and Edge, Chrome and Firefox up to 110, Safari up to 15, Opera up to 99, Opera Mini up to 34, Android up to 3).

**What the provider cannot declare**, for the operator's checklist:

- **Server-side data scrubbing.**
  The provider has no scrubbing setting at the organization or the project level.
  After the apply, run `SENTRY_AUTH_TOKEN="$TF_VAR_sentry_auth_token" scripts/sentry-scrubbing.sh`; it sets, on the three projects only, the Data Scrubber with Sentry's defaults, no stored IP addresses, extra sensitive field names, and advanced rules that replace email addresses and Ecuadorian identity numbers and mask card numbers in every string.
  It reads each project back and stops on the first that did not keep a setting.
  `--dry-run` prints the request without a token.
  Run it again whenever the script changes.
  The organization's own scrubbing settings are left as they are, because they would apply to the other product's projects too.
- **Team membership.**
  The alerts go to the members of `ticket-pos`.
  Whoever should receive them joins the team in Sentry (Settings, Teams); a person's email is not something to commit here.
- **Checking the spike rule once.**
  Its trigger is the provider's "number of events seen by the workflow" above 50 in an hour, meant as every error in the project, whichever issue.
  After the first apply, open it in Sentry and check it reads that way rather than per issue.

**Cron and uptime monitors (#778).**
The operator is emailed when a scheduled job fails or does not run, and when the Storefront or the Staff app stops answering.
Each Cloud Scheduler job has a `sentry_cron_monitor` in its own file, beside it, in the `ticket-pos-api` project; each Next app has a `sentry_uptime_monitor` in `cloud_run_frontends.tf`, beside its service, in its own project.
What they share, and the two alert rules that watch them, are at the end of `sentry.tf`.

A cron monitor exists only while its job is unpaused and `sentry_api_enabled` is on, so a paused job can never alert that it did not run.
In the module every job defaults to paused, but `envs/prod/terraform.tfvars` already unpauses five: the Sale Invoice Drainer, Owed Assignment Mail, the Holder Address Purge and the two reminders.
So the apply that turns `sentry_api_enabled` on in production also creates those five cron monitors and the check-in alert, in one go; on the Developer plan, with its one cron monitor, upgrade first.
Setting a job's `<job>_enabled` to true, with the API reporting, is the one change that both unpauses the job and creates its monitor.
The monitor's name, schedule and time zone are references to the scheduler job's own `name`, `schedule` and `time_zone`, so moving a `<job>_schedule` moves both.
Pausing a job in an incident with `gcloud scheduler jobs pause` leaves its monitor in place, and it will report missed runs until the job is resumed; set `<job>_enabled = false` and apply to stop both.

The provider's cron monitor has no slug argument: Sentry makes the slug from the name.
Each monitor is named its slug, the scheduler job's name without the `${environment}-` prefix, which is the job name the API checks in with (`scheduledJobs` in `backend/internal/server/jobs.go`).
After the first apply that creates a monitor, check in Sentry (Crons) that its slug reads exactly as below; Sentry adds a suffix only when the slug is already taken in the organization.

| Job | Scheduler resource | Monitor slug |
| --- | --- | --- |
| Reversal Reconciler | `google_cloud_scheduler_job.reversal_reconciler` | `ticket-pos-reversal-reconciler` |
| Sale Invoice Drainer | `google_cloud_scheduler_job.sale_invoice_drainer` | `ticket-pos-sale-invoice-drainer` |
| Follow Digest enqueue | `google_cloud_scheduler_job.follow_digest_enqueue` | `ticket-pos-follow-digest-enqueue` |
| Follow Digest drain | `google_cloud_scheduler_job.follow_digest_drain` | `ticket-pos-follow-digest-drain` |
| Abandoned Answer Purge | `google_cloud_scheduler_job.answer_purge` | `ticket-pos-answer-purge` |
| Answer Reminder | `google_cloud_scheduler_job.answer_reminder` | `ticket-pos-answer-reminder` |
| Assignment Reminder | `google_cloud_scheduler_job.assignment_reminder` | `ticket-pos-assignment-reminder` |
| Owed Assignment Mail | `google_cloud_scheduler_job.owed_assignment_mail` | `ticket-pos-owed-assignment-mail` |
| Holder Address Purge | `google_cloud_scheduler_job.holder_address_purge` | `ticket-pos-holder-address-purge` |

How much each monitor tolerates follows how often its job runs (`local.sentry_cron_tolerance`):

| Cadence | Jobs | Grace | Issue after | Resolved after |
| --- | --- | --- | --- | --- |
| Every minute | Reversal Reconciler, Follow Digest drain, Owed Assignment Mail | 1 minute | 5 bad runs in a row | 2 good runs |
| Every five minutes | Sale Invoice Drainer | 2 minutes | 2 bad runs in a row | 1 good run |
| Daily or weekly | Follow Digest enqueue, the two purges, the two reminders | 15 minutes | the first bad run | 1 good run |

A bad run is a failed check-in (the route answered a 5xx or panicked), a missed one (none within the grace) or a timed-out one.
The cadence is chosen per job in its file from its default schedule; moving a `<job>_schedule` to another cadence (every minute to hourly, say) means moving that job's `local.sentry_cron_tolerance` key with it.
The maximum runtime is the job's own `<job>_attempt_deadline_seconds` rounded up to the minute, plus one, so a run is timed out only after Cloud Scheduler has given up on it: 3 minutes for every job at today's deadlines.

The uptime monitors exist only while their app's switch is on and its hostname is set:

| Monitor | Probes | Exists when |
| --- | --- | --- |
| `sentry_uptime_monitor.storefront` | `GET https://<storefront_domain>/api/health`, the deep-health route (#775) | `sentry_storefront_enabled` |
| `sentry_uptime_monitor.staff` | `GET https://<staff_domain>/login`, the sign-in page | `sentry_staff_enabled` |

In production these are `https://discover.multiticketing.com/api/health` and `https://pos.multiticketing.com/login`.
Each probes every minute, passes on a 2xx answer only, waits 10 seconds (longer than the deep-health route's own 4 second bound), and is down after three failures in a row.

Two alert rules email the `ticket-pos` team, in production, when a monitor's issue opens or comes back, at most every 30 minutes per issue: `sentry_alert.job_check_in` watches every cron monitor (a failed, missed or timed-out check-in), and `sentry_alert.uptime` both uptime monitors.
Each exists only while there is a monitor for it to watch.

**What a full apply creates**, with every switch on and every job unpaused, on top of the 19 above: 9 cron monitors, 2 uptime monitors and the 2 alert rules.
The free Developer plan allows one cron monitor and one uptime monitor, so the full set needs a paid plan with pay-as-you-go for the other eight cron monitors and the second uptime monitor; upgrade before switching on the API (five cron monitors at once in production, above) or both Next apps.
Nothing in the code depends on the plan: every job checks in whether or not its monitor exists.

**Testing the switches.**
`modules/ticket-pos/tests/sentry.tftest.hcl` plans the module against mocked providers, so it needs no credential and reaches nothing: with every switch off no service has a Sentry variable, and each switch sets the DSN on its own service only.
`modules/ticket-pos/tests/monitors.tftest.hcl` does the same for the monitors: no cron monitor while every job is paused, one per unpaused job named as `backend/internal/server/jobs.go` names it (the test reads that file), with its scheduler job's schedule and time zone, and each uptime monitor on its app's hostname only while that app reports.

```bash
cd terraform/modules/ticket-pos
terraform init
terraform test
```

## Enabling Sentry: the operator's checklist

Everything for Sentry is merged and dormant (ADR [0079](../docs/adr/0079-sentry-reports-errors-traces-and-job-check-ins-behind-one-seam-holding-no-personal-data-and-dormant-until-a-dsn-is-set.md)).
These are the steps only the operator can take, in this order.
Each says what it makes true, how to check it, and how to take it back.
Nothing in the code waits on any of them: until step 5, production behaves as it did before Sentry, apart from the three fixes that stand on their own (the cause of a 500 logged, the stack of a panic, the recover on the drainer kick).

**The rule that orders them.**
No DSN is set on any service until the Policy Version that names Sentry is published (step 4).
It is the rule [ADR 0045](../docs/adr/0045-ticket-questions-ship-dark-until-the-privacy-policy-describes-them.md) set for Ticket Questions: the mechanism merges, and nothing about a person leaves the platform until the published policy says it may.
Steps 1 to 3 send Sentry nothing about a person: they create empty projects, and the release token only sends commit SHAs and source maps.
The deployables are independent, and the API may be switched on alone first.

### 1. Upgrade the Sentry plan

- **Do:** in Sentry, Settings, Subscription, move `redplanettribe` from the free Developer plan to a paid plan with pay-as-you-go for monitors.
  Check the monitor prices first (spec #761 noted $0.78 per extra cron monitor and $1.00 per extra uptime monitor a month).
- **Makes true:** room for the nine cron monitors and two uptime monitors of a full apply (the Developer plan has one of each), and the Storefront key's rate limit is enforced, which Sentry does only on a plan that has per-key rate limits.
  The organization's 5,000 errors a month are shared with the other product either way.
- **Check:** Settings, Subscription names the paid plan.
  After step 3, the Storefront's key (Project Settings, Client Keys) shows its rate limit of 500 events an hour.
- **Undo:** downgrade the plan after switching the API and both Next apps off (step 5's undo), so no monitor is left over the free plan's one of each.

### 2. Create the two tokens

Two tokens, never the same one: one acts for Terraform and can create things, the other acts for the deploy and can only make releases.

| | Terraform provider token | Release token |
| --- | --- | --- |
| Kind | an internal integration's token (Settings, Developer Settings, Custom Integrations, a new internal integration), which outlives any one person | an organization auth token (Settings, Developer Settings, Organization Tokens) |
| Scopes | Organization Read, Team Admin, Project Admin, Alerts Read & Write (a personal token instead: `org:read`, `team:admin`, `project:admin`, `alerts:write`) | `org:ci`, fixed, the release scope (a personal token instead: `project:releases` and `org:read`) |
| Where it lives | `TF_VAR_sentry_auth_token` in the operator's gitignored root `.env`, sourced before every plan | the GitHub Actions repository secret `SENTRY_AUTH_TOKEN` of `redplanettribe/ticket_pos` |
| What uses it | the `sentry` provider in `envs/prod` (variable `sentry_auth_token`), and `scripts/sentry-scrubbing.sh` | `.github/workflows/deploy.yml` (the Next builds' source map upload and `scripts/sentry-release.sh`), and `make deploy` when exported |

- **Do:** create both in Sentry, then put the Terraform token in `.env`.
  Store the release token once step 3 has created the projects, with `gh secret set SENTRY_AUTH_TOKEN -R redplanettribe/ticket_pos`, which asks for the value rather than taking it on the command line.
  Stored before then, every deploy warns that the projects do not exist and carries on.
- **Makes true:** a plan can reach Sentry, and every deploy after the secret is stored uploads the Next apps' source maps, creates the release for the deployed SHA in all three projects and marks each service's deploy, all failing soft ([Sentry releases and source maps](#sentry-releases-and-source-maps-776)).
- **Check:** the Terraform token reaches the organization:

  ```bash
  set -a; source .env; set +a
  curl -sf -H "Authorization: Bearer $TF_VAR_sentry_auth_token" \
    https://sentry.io/api/0/organizations/redplanettribe/ | jq -r .slug   # redplanettribe
  ```

  The release token: `gh secret list -R redplanettribe/ticket_pos` lists `SENTRY_AUTH_TOKEN`; the next deploy runs "Create the Sentry release" and the three "Mark ... deploy in Sentry" steps with no warning, and Sentry, Releases, shows the deployed SHA in all three projects.
- **Undo:** revoke either token in Sentry.
  `gh secret delete SENTRY_AUTH_TOKEN -R redplanettribe/ticket_pos` puts the deploy back to what it was before, apart from the release build argument; without the Terraform token a plan stops and asks for it.

### 3. Apply, and what Terraform could not declare

- **Do:** with `.env` sourced and every `sentry_*_enabled` still absent from `terraform.tfvars`, `terraform plan` then `terraform apply` in `envs/prod` ([Running it](#running-it)).
  Then the steps Terraform could not declare, below.
- **Makes true:** the `ticket-pos` team, the three projects and their keys, the Storefront's rate limit and inbound filters, and nine alert rules, 19 resources in all (the [Sentry](#sentry) section's table, "What a first apply creates").
  No Cloud Run service changes and no DSN is set anywhere, so nothing reports yet.
- **Check:** the plan adds 19 and changes or destroys nothing; stop if it touches a Cloud Run service.
  A second plan reports no changes.
  In Sentry, Settings, Teams lists `ticket-pos`, and Projects lists `ticket-pos-api`, `ticket-pos-staff` and `ticket-pos-storefront`, each with one key and three alert rules.
- **Undo:** nothing to undo while every switch is off: the projects hold nothing and receive nothing.
  Removing them is a targeted `terraform destroy` of the `module.ticket_pos.sentry_*` resources, which deletes their events with them.

What Terraform could not declare, each found by the ticket that tried (#777, #778), and what covers it:

| Not declarable | Covered by | Check |
| --- | --- | --- |
| Server-side data scrubbing: the provider has no scrubbing setting at either level | `SENTRY_AUTH_TOKEN="$TF_VAR_sentry_auth_token" scripts/sentry-scrubbing.sh`, after this apply and again whenever the script changes; project settings only, never the shared organization's | the script reads every setting back and stops on the first project that did not keep one; Project Settings, Security & Privacy shows them |
| Who receives the alert emails | the operator adds each recipient to the `ticket-pos` team (Settings, Teams); no email address is committed | the team's member list |
| Whether the error spike rule counts the whole project | open `error_spike` on each project once and read it | it reads as every error in the project above 50 in an hour, not per issue |
| A cron monitor's slug: the provider sends a name and Sentry makes the slug | each monitor is named its slug | after step 6 creates a monitor, Crons shows the slug exactly as in the [Sentry](#sentry) section's slug table; a suffix means the slug was taken in the organization |
| The plan, the tokens, the Actions secret, the Policy Version | steps 1, 2 and 4 of this list | as each step says |

### 4. Publish the Policy Version that names Sentry

- **Do:** publish, in the Staff app's Legal Center (`/operator/legal`, as a Platform Operator), a Policy Version whose Privacy Policy names Sentry as a processor of error reports.
  Its wording, and whether it is a Gating Edition owing every Customer a fresh Policy Acceptance or a Correction, are counsel's decisions and outside this spec; batch it with any other pending policy edits, as ADR 0045 asks.
- **Makes true:** the published policy describes what step 5 starts sending.
- **Check:** the edition is current: its effective date has arrived, the Legal Center lists it as current, and the Storefront's privacy policy page shows it.
- **Undo:** an edition dated for a later day can be cancelled before it takes effect; once current it is kept forever, as every Policy Version is.
  Taking Sentry back out is step 5's undo, and a later edition.

### 5. Switch each deployable on

- **Do:** write the switch in `envs/prod/terraform.tfvars`, one deployable at a time, the API first if you want one alone, and apply:

  ```hcl
  sentry_api_enabled        = true
  sentry_staff_enabled      = true
  sentry_storefront_enabled = true
  ```

- **Makes true:** that service gets its project's `SENTRY_DSN` (and the Next apps `SENTRY_ENVIRONMENT=production`) and rolls a new revision; no other service changes.
  For the API it also makes the cron monitors of the jobs already unpaused: five in production today (the Sale Invoice Drainer, Owed Assignment Mail, the Holder Address Purge and the two reminders) and the check-in alert, so the plan adds 6.
  For the Staff app it makes the uptime monitor on `https://pos.multiticketing.com/login`, for the Storefront the one on `https://discover.multiticketing.com/api/health`, and whichever comes first the uptime alert.
- **Check:** the plan changes that one service and adds only what is above, apart from one in-place update: switching on the second Next app adds its monitor to the uptime alert that already exists.
  The service carries the DSN:

  ```bash
  gcloud run services describe prod-ticket-pos-api --region us-east1 \
    --format=json | jq '.spec.template.spec.containers[0].env[] | select(.name | startswith("SENTRY_"))'
  ```

  (`prod-ticket-pos-staff` and `prod-ticket-pos-storefront` the same.)
  The API: within five minutes Crons shows check-ins from `ticket-pos-owed-assignment-mail` (every minute) and `ticket-pos-sale-invoice-drainer` (every five).
  A Next app: its pages carry `<meta name="ticket-pos-sentry">`, and Uptime shows its monitor passing.
  That an event arrives readable, a trace joins browser to API and an alert email reaches the team is the end-to-end walk of #780, done after the first switch is on.
- **Undo:** set the switch to `false` (or delete the line) and apply: the DSN leaves the service, which rolls a revision that reports nothing, and its monitors go with it.
  An alert rule goes only with the last monitor it watches; switching off one Next app while the other reports updates the uptime alert in place.

### 6. Unpause and monitor each job

- **Do:** for each job still paused (today the Reversal Reconciler, the Follow Digest enqueue and drain, and the Abandoned Answer Purge), set its `<job>_enabled = true` in `terraform.tfvars` when the job itself is ready to run, and apply.
  Each job has its own preconditions, in its own section of this README; a monitor is never a reason to unpause one.
- **Makes true:** with the API switched on, the one change unpauses the Cloud Scheduler job and creates its cron monitor, with the scheduler's own schedule and time zone, so the job is never running unmonitored.
- **Check:** the plan changes the job's `paused`, adds one `sentry_cron_monitor` and updates `sentry_alert.job_check_in` in place to watch it too.
  After the job's next run, Crons shows the monitor, its slug as in the [Sentry](#sentry) section's slug table, and an ok check-in.
- **Undo:** set `<job>_enabled = false` and apply, which pauses the job and removes its monitor together.
  Not `gcloud scheduler jobs pause`: that leaves the monitor reporting missed runs until the job resumes.

### A developer's local stack

Locally, in CI and in agent runs no DSN is set, and nothing reports: the API builds no reporter, and the Next apps initialise no SDK and their tunnels forward nothing.
That default matters because a local database can hold a copy of production data, real people's included, which must never leave the machine.

To try the integration end to end, opt in with the DSN of a project you own, or of a local stand-in that accepts envelopes, never one of the three production keys:

- The API: `SENTRY_DSN=...` in the root `.env`, which `docker-compose.yml` passes to the backend; it reports as `development`.
- A Next app: `SENTRY_DSN=... pnpm start` on a production build, as each app's README, "Walking it locally against a fake endpoint", describes; it reports as `development` unless `SENTRY_ENVIRONMENT` says otherwise.

Do it against a database seeded with fixtures, not against a copy of production, and unset the DSN when done.

## What is not here yet

The frontend `run.invoker` grants (#48) and the keyless deploy pipeline (#49) have landed — see
[Deploying](#deploying). What remains out of scope is recorded in
[docs/gcp-deployment.md](../docs/gcp-deployment.md): regional HA, the partner API load balancer, and a
real email provider (which currently blocks staff sign-in on a deployed environment).
