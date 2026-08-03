#!/usr/bin/env bash
#
# Replicate production into the local dev stack: the whole database, and every
# object in the media bucket.
#
# READ THIS FIRST: production carries real Customer names, emails, phone numbers
# and Payment records. This script copies all of it onto this machine in the
# clear, into a Postgres and a MinIO that both listen on localhost with the
# password `ticket_pos`/`minio123`. Treat the local stack as production data
# once it has run.
#
# It is destructive *locally* and read-only against production: the local
# `ticket_pos` database is dropped and recreated, and the local MinIO bucket is
# mirrored to match prod exactly (extra local objects are deleted). Nothing is
# ever written to the production database or the production bucket.
#
# Why the database arrives via GCS rather than a direct pg_dump: the Cloud SQL
# instance has no public IP (terraform/modules/ticket-pos/cloud_sql.tf), so no
# socket can be opened to it from a laptop. `gcloud sql export` runs the dump
# inside Google's network and drops the file in a bucket, which needs no
# network path to the database at all.
#
# Usage:
#   make prod-to-local            # prompts before touching local data
#   make prod-to-local ARGS=--yes # no prompt
#   scripts/prod-to-local.sh --db-only | --media-only
set -euo pipefail

# --- Configuration ------------------------------------------------------------
# Production facts, matching terraform/envs/prod. Overridable by environment for
# a future staging root, but the defaults are the deployed production values.
GCP_PROJECT="${GCP_PROJECT:-multiticketing}"
SQL_INSTANCE="${SQL_INSTANCE:-prod-ticket-pos}"
PROD_DB="${PROD_DB:-ticket_pos}"
PROD_DB_USER="${PROD_DB_USER:-ticket_pos_app}"
MEDIA_BUCKET="${MEDIA_BUCKET:-multiticketing-ticket-pos-prod}"

# Staging bucket for the SQL dump. Deliberately NOT the media bucket: that one
# grants `allUsers` object read, so a dump written there would publish the
# entire customer database to anyone who guessed the URL. This bucket is
# created private, with public access prevention enforced so it cannot later be
# opened up by accident, and a 1-day lifecycle rule as a backstop for dumps this
# script fails to clean up.
DUMP_BUCKET="${DUMP_BUCKET:-${GCP_PROJECT}-ticket-pos-dumps}"

# Local dev stack, matching docker-compose.yml.
LOCAL_PG_HOST="${LOCAL_PG_HOST:-localhost}"
LOCAL_PG_PORT="${LOCAL_PG_PORT:-64432}"
LOCAL_PG_USER="${LOCAL_PG_USER:-ticket_pos}"
LOCAL_PG_PASSWORD="${LOCAL_PG_PASSWORD:-ticket_pos}"
LOCAL_DB="${LOCAL_DB:-ticket_pos}"
LOCAL_MINIO_BUCKET="${LOCAL_MINIO_BUCKET:-ticket-pos}"
LOCAL_MINIO_USER="${LOCAL_MINIO_USER:-minio}"
LOCAL_MINIO_PASSWORD="${LOCAL_MINIO_PASSWORD:-minio123}"

# The prefixes storage.tf says belong in the bucket. Each is served anonymously
# in production, so each needs the same anonymous download policy on MinIO.
MEDIA_PREFIXES=(covers videos logos avatars)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="${REPO_ROOT}/.prod-snapshot"

DO_DB=true
DO_MEDIA=true
ASSUME_YES=false

for arg in "$@"; do
  case "$arg" in
    --db-only) DO_MEDIA=false ;;
    --media-only) DO_DB=false ;;
    -y | --yes) ASSUME_YES=true ;;
    -h | --help)
      sed -n '2,30p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *)
      echo "unknown argument: $arg" >&2
      exit 2
      ;;
  esac
done

log() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die() {
  printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
  exit 1
}

# --- Preflight ----------------------------------------------------------------
preflight() {
  log "Preflight"

  for tool in gcloud docker psql; do
    command -v "$tool" >/dev/null || die "$tool is not installed"
  done

  gcloud auth list --filter=status:ACTIVE --format='value(account)' | grep -q . ||
    die "not logged in to gcloud. Run: gcloud auth login"

  gcloud projects describe "$GCP_PROJECT" >/dev/null 2>&1 ||
    die "cannot reach project ${GCP_PROJECT}. Check the account has access."

  # The dev stack has to be up: this script restores *into* it, it does not
  # start it. Identified by the published ports rather than by container name so
  # a renamed compose project still works.
  docker compose --project-directory "$REPO_ROOT" ps --status running --format '{{.Service}}' 2>/dev/null |
    grep -qx postgres || die "the dev stack is not running. Start it with: make dev"

  echo "  project:      ${GCP_PROJECT}"
  echo "  sql instance: ${SQL_INSTANCE} (database ${PROD_DB})"
  echo "  media bucket: gs://${MEDIA_BUCKET}"
  echo "  local pg:     ${LOCAL_PG_HOST}:${LOCAL_PG_PORT}/${LOCAL_DB}"
}

confirm() {
  $ASSUME_YES && return 0

  cat <<EOF

This replaces LOCAL data with a copy of PRODUCTION:
  - drops and recreates the local database '${LOCAL_DB}'
  - mirrors gs://${MEDIA_BUCKET} into the local MinIO bucket '${LOCAL_MINIO_BUCKET}',
    deleting local objects that production does not have

Production itself is only read from. Real customer data will land on this machine.

EOF
  read -r -p "Type 'yes' to continue: " reply
  [ "$reply" = "yes" ] || die "aborted"
}

# --- Database -----------------------------------------------------------------

# The export bucket, created on first run. Kept out of Terraform on purpose: it
# holds no state the deployment depends on, and a developer convenience bucket
# appearing in the prod plan invites someone to delete it mid-apply.
ensure_dump_bucket() {
  if gcloud storage buckets describe "gs://${DUMP_BUCKET}" --project="$GCP_PROJECT" >/dev/null 2>&1; then
    return 0
  fi

  log "Creating private dump bucket gs://${DUMP_BUCKET}"
  gcloud storage buckets create "gs://${DUMP_BUCKET}" \
    --project="$GCP_PROJECT" \
    --location=US-EAST1 \
    --uniform-bucket-level-access \
    --public-access-prevention

  # Backstop for a dump this script fails to delete: nothing survives a day.
  local lifecycle
  lifecycle="$(mktemp)"
  cat >"$lifecycle" <<'JSON'
{"rule": [{"action": {"type": "Delete"}, "condition": {"age": 1}}]}
JSON
  gcloud storage buckets update "gs://${DUMP_BUCKET}" --lifecycle-file="$lifecycle"
  rm -f "$lifecycle"
}

# Cloud SQL exports run as the *instance's* service agent, not as the caller, so
# the agent needs write access to the destination bucket. Idempotent.
grant_export_access() {
  local sa
  sa="$(gcloud sql instances describe "$SQL_INSTANCE" --project="$GCP_PROJECT" \
    --format='value(serviceAccountEmailAddress)')"
  [ -n "$sa" ] || die "could not resolve the Cloud SQL service account"

  gcloud storage buckets add-iam-policy-binding "gs://${DUMP_BUCKET}" \
    --member="serviceAccount:${sa}" \
    --role=roles/storage.objectAdmin \
    --quiet >/dev/null
}

export_database() {
  local stamp object
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  object="gs://${DUMP_BUCKET}/${SQL_INSTANCE}-${PROD_DB}-${stamp}.sql.gz"
  DUMP_FILE="${WORK_DIR}/db.sql.gz"

  ensure_dump_bucket
  grant_export_access

  log "Exporting ${SQL_INSTANCE}/${PROD_DB} to ${object}"
  echo "  (server-side dump; takes a minute or two even on a small database)"
  gcloud sql export sql "$SQL_INSTANCE" "$object" \
    --project="$GCP_PROJECT" \
    --database="$PROD_DB"

  log "Downloading the dump"
  mkdir -p "$WORK_DIR"
  gcloud storage cp "$object" "$DUMP_FILE"

  # The dump is customer data sitting in a bucket; it has served its purpose the
  # moment it is on disk here.
  gcloud storage rm "$object" >/dev/null
  echo "  $(du -h "$DUMP_FILE" | cut -f1) at ${DUMP_FILE} (remote copy deleted)"
}

psql_local() {
  PGPASSWORD="$LOCAL_PG_PASSWORD" psql \
    -h "$LOCAL_PG_HOST" -p "$LOCAL_PG_PORT" -U "$LOCAL_PG_USER" "$@"
}

restore_database() {
  log "Restoring into ${LOCAL_DB}"

  # The API holds a connection pool open, and an open connection blocks DROP
  # DATABASE. Stopping the container is cleaner than terminating backends it
  # would immediately re-establish. It comes back up at the end of the run.
  docker compose --project-directory "$REPO_ROOT" stop backend >/dev/null 2>&1 || true

  psql_local -d postgres -v ON_ERROR_STOP=1 -q <<SQL
SELECT pg_terminate_backend(pid) FROM pg_stat_activity
 WHERE datname = '${LOCAL_DB}' AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS ${LOCAL_DB};
CREATE DATABASE ${LOCAL_DB} OWNER ${LOCAL_PG_USER};
SQL

  # A Cloud SQL dump carries ownership and GRANT statements naming roles that
  # exist on the instance but not in a stock Postgres container. Creating them
  # as NOLOGIN roles up front turns what would be hundreds of errors into a
  # clean restore; they can log in to nothing, so they add no local access.
  psql_local -d "$LOCAL_DB" -v ON_ERROR_STOP=1 -q <<SQL
DO \$\$
DECLARE r text;
BEGIN
  FOREACH r IN ARRAY ARRAY['${PROD_DB_USER}', 'cloudsqlsuperuser', 'cloudsqladmin'] LOOP
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
      EXECUTE format('CREATE ROLE %I NOLOGIN', r);
    END IF;
    EXECUTE format('GRANT %I TO ${LOCAL_PG_USER}', r);
  END LOOP;
END
\$\$;
SQL

  # Not ON_ERROR_STOP: a plain-format dump replayed outside Cloud SQL always
  # trips over a few statements only a real superuser may run (comments on
  # extensions, ALTER of system objects). Those are noise. Errors are collected
  # and verify_database() decides whether the result is actually usable.
  local err_log="${WORK_DIR}/restore-errors.log"
  gunzip -c "$DUMP_FILE" |
    psql_local -d "$LOCAL_DB" -q -v ON_ERROR_STOP=0 2>"$err_log" >/dev/null || true

  local errors
  errors="$(grep -c '^ERROR:' "$err_log" || true)"
  if [ "$errors" -gt 0 ]; then
    warn "${errors} statement error(s) during restore; see ${err_log}"
    grep '^ERROR:' "$err_log" | sort | uniq -c | sort -rn | head -10 >&2
  fi

  neutralize_dev_seeds
}

# Without this the API will not start against a production database.
#
# The dev backend runs migrate.Up with includeDevSeeds=true; production's
# migrate Job passes false, so `*_seed_dev_*` migrations never ran there and
# production's schema_migrations has no row for them. Restore that table and the
# seeds are suddenly "pending" against production data, where they do not merely
# duplicate -- 038_seed_dev_unlisted_event INSERTs an Event owned by the demo
# Organization, which production has never had, and the boot dies on a foreign
# key violation.
#
# They are recorded as applied rather than executed. The point of this script is
# a local stack that looks like production, and production has no demo
# Organization; running the seeds would inject fixture rows that no environment
# actually has. The cost is that dev seed data is gone until the next restore
# from scratch -- see the note the script prints at the end.
neutralize_dev_seeds() {
  local versions=() name
  for name in "$REPO_ROOT"/backend/migrations/*_seed_dev_*.sql; do
    [ -e "$name" ] || continue
    versions+=("$(basename "$name" .sql)")
  done
  [ ${#versions[@]} -gt 0 ] || return 0

  local values=""
  for name in "${versions[@]}"; do
    values+="('${name}'),"
  done

  psql_local -d "$LOCAL_DB" -v ON_ERROR_STOP=1 -q <<SQL
INSERT INTO schema_migrations (version) VALUES ${values%,}
ON CONFLICT (version) DO NOTHING;
SQL
  echo "  marked ${#versions[@]} dev seed migration(s) as applied without running them"
}

# --- Media --------------------------------------------------------------------
mirror_media() {
  local media_dir="${WORK_DIR}/media"
  mkdir -p "$media_dir"

  log "Syncing gs://${MEDIA_BUCKET} to ${media_dir}"
  # rsync, not cp: a second run transfers only what changed, and
  # --delete-unmatched-destination-objects makes the local copy an exact mirror
  # rather than an accumulation of every object prod has ever had.
  gcloud storage rsync -r --delete-unmatched-destination-objects \
    "gs://${MEDIA_BUCKET}" "$media_dir"

  log "Loading into MinIO bucket '${LOCAL_MINIO_BUCKET}'"
  local network
  network="$(compose_network)"
  [ -n "$network" ] || die "could not determine the compose network for MinIO"

  local policy_cmds=""
  for prefix in "${MEDIA_PREFIXES[@]}"; do
    policy_cmds+="mc anonymous set download local/${LOCAL_MINIO_BUCKET}/${prefix} > /dev/null && "
  done

  mc_run "$network" -v "${media_dir}:/media:ro" -- "
    mc mirror --overwrite --remove /media local/${LOCAL_MINIO_BUCKET} &&
    ${policy_cmds} echo '  anonymous download re-applied to: ${MEDIA_PREFIXES[*]}'
  "
}

# mc against the local MinIO, on the compose network so it can address it as
# `minio:9000` exactly as minio-init does. Args before `--` go to docker run.
mc_run() {
  local network="$1"
  shift
  local docker_args=()
  while [ "$1" != "--" ]; do
    docker_args+=("$1")
    shift
  done
  shift

  docker run --rm --network "$network" "${docker_args[@]}" \
    --entrypoint /bin/sh minio/mc:latest -c "
      mc alias set local http://minio:9000 ${LOCAL_MINIO_USER} ${LOCAL_MINIO_PASSWORD} > /dev/null &&
      mc mb --ignore-existing local/${LOCAL_MINIO_BUCKET} > /dev/null &&
      $*
    "
}

compose_network() {
  docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}' \
    "$(docker compose --project-directory "$REPO_ROOT" ps -q minio)"
}

# --- Verification -------------------------------------------------------------
verify_database() {
  log "Verifying the database"

  local missing=0
  for table in organizations events ticket_types ticket_sales customers payments; do
    psql_local -d "$LOCAL_DB" -tAc "SELECT to_regclass('public.${table}')" |
      grep -qx "$table" || {
      warn "expected table '${table}' is missing"
      missing=1
    }
  done
  [ "$missing" -eq 0 ] || die "the restore did not produce a usable schema"

  echo "  migration version: $(psql_local -d "$LOCAL_DB" -tAc \
    'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1' 2>/dev/null || echo unknown)"
  psql_local -d "$LOCAL_DB" -c "
    SELECT 'organizations' AS table, count(*) FROM organizations
    UNION ALL SELECT 'events', count(*) FROM events
    UNION ALL SELECT 'ticket_types', count(*) FROM ticket_types
    UNION ALL SELECT 'ticket_sales', count(*) FROM ticket_sales
    UNION ALL SELECT 'customers', count(*) FROM customers
    UNION ALL SELECT 'members', count(*) FROM members
    UNION ALL SELECT 'payments', count(*) FROM payments"
}

verify_media() {
  log "Verifying object storage"

  local remote local_count
  remote="$(gcloud storage ls -r "gs://${MEDIA_BUCKET}/**" | grep -c . || true)"
  # Counted with mc, not an anonymous HTTP list: the bucket policy grants
  # download on the four prefixes and nothing else, so listing it unauthenticated
  # is a 403 by design.
  local_count="$(mc_run "$(compose_network)" -- \
    "mc ls --recursive local/${LOCAL_MINIO_BUCKET} | wc -l" | tr -d '[:space:]')"

  echo "  production: ${remote} objects"
  echo "  local:      ${local_count} objects"
  [ "$local_count" = "$remote" ] ||
    warn "object counts differ; re-run with --media-only if this is unexpected"

  # The counts agreeing does not prove the objects are *servable*. The app hands
  # browsers plain unauthenticated URLs (S3_PUBLIC_URL), so fetch one the way a
  # browser would and check it comes back with a body.
  local sample
  sample="$(gcloud storage ls -r "gs://${MEDIA_BUCKET}/**" | grep -m1 '\.\(jpg\|png\|webp\)$' |
    sed "s|gs://${MEDIA_BUCKET}/||")"
  if [ -n "$sample" ]; then
    local code
    code="$(curl -so /dev/null -w '%{http_code}' \
      "http://localhost:64900/${LOCAL_MINIO_BUCKET}/${sample}")"
    if [ "$code" = "200" ]; then
      echo "  anonymous fetch of ${sample%%/*}/... : 200 OK"
    else
      warn "anonymous fetch of ${sample} returned HTTP ${code}; images will be broken in the UI"
    fi
  fi
}

# --- Main ---------------------------------------------------------------------
preflight
confirm
mkdir -p "$WORK_DIR"

if $DO_DB; then
  export_database
  restore_database
fi
$DO_MEDIA && mirror_media

log "Restarting the API"
docker compose --project-directory "$REPO_ROOT" start backend >/dev/null

# Not cosmetic: the API applies migrations at boot, so this is where a restored
# database that the application cannot actually run against shows up. Without
# the check the script reports success while the container crash-loops.
printf '  waiting for /health'
api_up=false
for _ in $(seq 1 30); do
  if curl -sf -o /dev/null http://localhost:64080/health; then
    api_up=true
    break
  fi
  printf '.'
  sleep 2
done
echo
if $api_up; then
  echo "  API is up"
else
  docker compose --project-directory "$REPO_ROOT" logs backend --tail=20 >&2
  die "the API did not come up against the restored database (logs above)"
fi

$DO_DB && verify_database
$DO_MEDIA && verify_media

log "Done"
cat <<EOF
The local stack now holds a copy of production. Three things it is NOT:

  - The dev seed data is gone. Production has no demo Organization and no seeded
    Events, so neither does this stack now, and the dev seed migrations are
    recorded as applied rather than run. Anything pinned to a fixture UUID --
    most of the e2e suite -- will fail until the database is rebuilt from
    scratch: docker compose down -v && make dev.
  - Signed artifacts do not carry over. Ticket QR codes and confirmation links
    were signed with the production HMAC keys, which are in Secret Manager and
    not on this machine, so production tickets will not validate locally.
  - Payments are stubbed locally unless PAYPHONE_* is set in .env, so the
    imported sales are history, not something the local checkout can continue.

Snapshot artifacts are in ${WORK_DIR} (gitignored). Delete it when finished --
it is a plaintext copy of the customer database.
EOF
