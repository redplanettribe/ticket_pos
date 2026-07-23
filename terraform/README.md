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

`envs/prod` currently creates the image registry and everything the application stores state in.

**Network.** A VPC with no subnets. It exists so Cloud SQL can have a private address: a private-IP
instance lives in a Google-managed network peered to ours, and the peering needs a range reserved on
our side first (`10.240.0.0/24`). Cloud Run's subnet arrives with Cloud Run (#47).

**Cloud SQL.** Postgres 18, `db-g1-small`, private IP only, single zone. Daily backups at 05:00 UTC with
seven kept, plus point-in-time recovery over a seven-day window of write-ahead logs. Deletion is blocked
twice: `deletion_protection` stops `terraform destroy`, and `settings.deletion_protection_enabled` stops
gcloud and the console. Both must be flipped off, in two applies, before the instance can go.

Single-zone is deliberate and recorded in ADR 0007. Regional HA is `availability_type = "REGIONAL"` in
`cloud_sql.tf` — an in-place update, not a recreate, and roughly double the bill.

**Bucket.** One public-read bucket for `covers/` and `logos/`. Two things about it matter:

- *Everything in it is world-readable to anyone with the URL, forever.* The `allUsers` grant is
  bucket-wide and uniform access gives no per-object escape. Nothing private goes here.
- *The CORS rule is load-bearing.* The API hands the browser a presigned PUT URL and the browser uploads
  directly, cross-origin, from the Staff app. MinIO is permissive by default, so a missing origin here is
  invisible locally and surfaces in production as an opaque browser network error on every image upload.
  The origin comes from `staff_origin` in `envs/prod/terraform.tfvars`.

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

Granting the workload permission to read these secrets is #48.

**Rotating the HMAC key** is manual: `terraform taint module.ticket_pos.google_storage_hmac_key.app`,
apply, redeploy the API so it picks up the new version. Automating it needs a two-key overlap the
application cannot express with one credential pair.

## What is not here yet

Cloud Run services, the migrate Job, workload IAM, domain mappings, and the deploy pipeline are tracked
as issues #47-#49 and land in that order.
