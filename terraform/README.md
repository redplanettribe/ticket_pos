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

- **Versions are pinned.** Roots pin Terraform `~> 1.13` and the Google provider `~> 6.0`; the module
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

## What is not here yet

The frontend `run.invoker` grants (#48) and the keyless deploy pipeline (#49) have landed — see
[Deploying](#deploying). What remains out of scope is recorded in
[docs/gcp-deployment.md](../docs/gcp-deployment.md): regional HA, the partner API load balancer, and a
real email provider (which currently blocks staff sign-in on a deployed environment).
