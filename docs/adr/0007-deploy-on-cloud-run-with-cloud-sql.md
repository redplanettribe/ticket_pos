# Deploy on Cloud Run with Cloud SQL

Ticket POS is three stateless HTTP containers (Go API, Staff, Storefront) plus Postgres and object
storage, so we deploy to **Cloud Run** in `us-east1`, backed by **Cloud SQL for PostgreSQL** on a private
IP and a **GCS bucket** accessed through its S3-compatible API. Cloud Run scales to zero and carries no
operational floor, which suits a system that is not yet taking real money; GKE Autopilot would add ~$70/mo
and a cluster to run for no benefit at three services, and VMs would mean owning patching and rollout
ourselves.

## Consequences

- **No background workers.** Cloud Run has no long-running process model. The first queue or scheduled
  job needs Cloud Run Jobs, Cloud Tasks, or Cloud Scheduler — not a goroutine in the API.
- **GCS is used through the S3 API**, not the native Go client, so `platform/storage` stays on one code
  path shared with MinIO in local development. The cost is a static HMAC key in Secret Manager instead of
  workload identity.
- **Single-zone database.** A zonal outage takes the system down. Moving to regional HA is a change to
  `availability_type` and roughly doubles the database bill.
- Cloud SQL, Cloud Run, and the bucket must stay in the same region; splitting them puts cross-region
  latency on every request in a way that is invisible in the infrastructure code.
