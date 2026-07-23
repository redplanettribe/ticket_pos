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

## What is not here yet

This is the foundation only — state, module structure, and the image registry. Cloud SQL, the
application bucket, Cloud Run services, Secret Manager entries, workload IAM, and the deploy pipeline
are tracked as issues #46-#49 and land in that order.
