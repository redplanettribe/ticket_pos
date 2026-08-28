#!/usr/bin/env bash
#
# Deploy production from this machine, doing exactly what .github/workflows/
# deploy.yml does on a push to main. Reach for it when Actions cannot run (out
# of minutes, GitHub down); the workflow remains the normal path.
#
# The sequence is the workflow's, step for step, and the ORDER is load-bearing:
#
#   1. Sync: on `main`, clean tree, fast-forwarded to origin/main. What gets
#      deployed is what CI would have deployed -- a commit that is on origin.
#   2. Build and push the api, storefront and staff images, tagged with the
#      full commit SHA (never `latest`: an immutable tag is what makes the
#      rollback in deploy.yml a one-line command).
#   3. Run the migrate Job on the new api image and WAIT for it. A failed
#      migration stops the script here, before any service has moved: the
#      three services keep serving their previous revisions against a schema
#      that still matches them.
#   4. Only then roll the three services to the new images. Only --image is
#      passed; SA, secrets, VPC egress and scaling come from Terraform and must
#      not be restated here (terraform/README.md). image is in Terraform's
#      ignore_changes, so this does not drift a later plan.
#
# Authentication is your own gcloud login (`gcloud auth login`), not the WIF
# exchange the workflow uses -- that provider only issues tokens to GitHub. To
# deploy as the same service account CI uses instead of your user, export
# DEPLOY_IMPERSONATE=prod-ticket-pos-deploy@multiticketing.iam.gserviceaccount.com
# (you need roles/iam.serviceAccountTokenCreator on it).
#
# Usage:
#   make deploy                 # from any checkout of the repo; it deploys
#                               # the checkout's `main` after fast-forwarding
#   make deploy ARGS=--no-pull  # skip the fetch/fast-forward (deploy HEAD as is;
#                               # still must be `main` and clean)
set -euo pipefail

# --- Configuration ------------------------------------------------------------
# Identical to the `env:` block of .github/workflows/deploy.yml.
PROJECT_ID="${PROJECT_ID:-multiticketing}"
REGION="${REGION:-us-east1}"
IMAGE_REPO="${IMAGE_REPO:-${REGION}-docker.pkg.dev/${PROJECT_ID}/ticket-pos}"
MIGRATE_JOB="${MIGRATE_JOB:-prod-ticket-pos-migrate}"
API_SERVICE="${API_SERVICE:-prod-ticket-pos-api}"
STAFF_SERVICE="${STAFF_SERVICE:-prod-ticket-pos-staff}"
STOREFRONT_SERVICE="${STOREFRONT_SERVICE:-prod-ticket-pos-storefront}"
DEPLOY_BRANCH="${DEPLOY_BRANCH:-main}"

PULL=1
for arg in "$@"; do
  case "$arg" in
    --no-pull) PULL=0 ;;
    -h|--help) sed -n '2,33p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

for tool in git docker gcloud; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done

# --- 1. Sync to origin/main ----------------------------------------------------
step "Checking the working tree"
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "$DEPLOY_BRANCH" ] || die "on branch '$branch', must be on '$DEPLOY_BRANCH' -- the workflow only ever deploys $DEPLOY_BRANCH"
[ -z "$(git status --porcelain)" ] || die "working tree is not clean; commit, or set it aside, before deploying"

if [ "$PULL" = 1 ]; then
  step "Fast-forwarding $DEPLOY_BRANCH to origin/$DEPLOY_BRANCH"
  git fetch origin "$DEPLOY_BRANCH"
  # --ff-only: a local main that has diverged from origin is not something to
  # deploy silently. Merge it deliberately, then rerun.
  git merge --ff-only "origin/$DEPLOY_BRANCH"
else
  git fetch origin "$DEPLOY_BRANCH" >/dev/null 2>&1 || true
  if ! git merge-base --is-ancestor HEAD "origin/$DEPLOY_BRANCH" 2>/dev/null; then
    echo "warning: HEAD is not on origin/$DEPLOY_BRANCH; deploying a commit nobody can see on GitHub" >&2
  fi
fi

SHA="$(git rev-parse HEAD)"
step "Deploying $SHA ($(git log -1 --format=%s))"

# --- Auth ----------------------------------------------------------------------
step "Configuring gcloud"
account="$(gcloud config get-value account 2>/dev/null || true)"
[ -n "$account" ] || die "no active gcloud account; run 'gcloud auth login' first"
if [ -n "${DEPLOY_IMPERSONATE:-}" ]; then
  export CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT="$DEPLOY_IMPERSONATE"
  echo "as $account impersonating $DEPLOY_IMPERSONATE"
else
  echo "as $account"
fi
gcloud config set project "$PROJECT_ID" --quiet
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

# --- 2. Build and push ---------------------------------------------------------
# Same contexts, Dockerfiles and platform as the workflow. NEXT_PUBLIC_API_URL is
# deliberately not passed to either frontend: API_URL is set at runtime by
# Terraform and always wins (see the comments in deploy.yml).
step "Building and pushing the API image"
docker build --platform linux/amd64 \
  -t "${IMAGE_REPO}/api:${SHA}" \
  -f backend/Dockerfile backend
docker push "${IMAGE_REPO}/api:${SHA}"

step "Building and pushing the Storefront image"
docker build --platform linux/amd64 \
  -t "${IMAGE_REPO}/storefront:${SHA}" \
  -f apps/storefront/Dockerfile .
docker push "${IMAGE_REPO}/storefront:${SHA}"

step "Building and pushing the Staff image"
docker build --platform linux/amd64 \
  -t "${IMAGE_REPO}/staff:${SHA}" \
  -f apps/staff/Dockerfile .
docker push "${IMAGE_REPO}/staff:${SHA}"

# --- 3. Migrate, and wait ------------------------------------------------------
step "Running database migrations to completion"
gcloud run jobs update "$MIGRATE_JOB" \
  --region "$REGION" \
  --image "${IMAGE_REPO}/api:${SHA}"
gcloud run jobs execute "$MIGRATE_JOB" \
  --region "$REGION" \
  --wait

# --- 4. Deploy -----------------------------------------------------------------
step "Deploying the API"
gcloud run deploy "$API_SERVICE" \
  --region "$REGION" \
  --image "${IMAGE_REPO}/api:${SHA}"

step "Deploying the Storefront"
gcloud run deploy "$STOREFRONT_SERVICE" \
  --region "$REGION" \
  --image "${IMAGE_REPO}/storefront:${SHA}"

step "Deploying the Staff console"
gcloud run deploy "$STAFF_SERVICE" \
  --region "$REGION" \
  --image "${IMAGE_REPO}/staff:${SHA}"

step "Deployed $SHA to $PROJECT_ID ($REGION)"
