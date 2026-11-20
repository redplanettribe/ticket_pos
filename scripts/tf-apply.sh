#!/usr/bin/env bash
#
# Plan and apply a Terraform environment (prod by default) from this machine,
# with the operator's secrets loaded and an explicit yes in between.
#
#   1. Load the gitignored repo-root .env (set -a; source .env; set +a), so
#      every TF_VAR_* secret is in the environment without sourcing it by hand.
#   2. Refuse to go on if any TF_VAR_* name listed in .env.example is empty.
#      An empty secret is not merely a failed plan: most secret versions are
#      `count = var == "" ? 0 : 1`, so an apply without one DESTROYS the
#      version and silently turns the integration off (#73). .env.example is
#      the list; a new TF_VAR_ secret goes there and is required from then on.
#   3. Check the credentials a plan needs (gcloud ADC, the AWS SSO profile, the
#      Sentry token) up front, so an expired login fails here with the command
#      that fixes it, not halfway through a plan.
#   4. terraform init, then terraform plan -out=<planfile>, which prints the plan.
#   5. Ask "Apply this plan? [y/N]" and run terraform apply <planfile> only on
#      an explicit yes. Applying the saved planfile, not a fresh apply, is what
#      makes the approved plan exactly the one that runs; Terraform refuses it
#      as stale if the state moved in between.
#   6. Delete the planfile on every exit path: a planfile embeds secret values.
#      It lives in a private temporary directory, never in the working tree.
#
# Nothing in .env is printed. A missing variable is reported by name only.
#
# Usage:
#   make tf-apply                   # plan prod, ask, apply
#   make tf-apply TF_ENV=<env>      # another terraform/envs/<env>
#   make tf-apply ARGS=--plan-only  # plan, show it, and stop without asking
#
# Environment:
#   ENV_FILE  the file to load instead of <repo root>/.env (for example the
#             primary checkout's .env when running from a git worktree)
set -euo pipefail

TF_ENV="${TF_ENV:-prod}"
PLAN_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --plan-only) PLAN_ONLY=1 ;;
    -h|--help) awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; exit 0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"
TF_DIR="terraform/envs/$TF_ENV"
ENV_FILE="${ENV_FILE:-$REPO_ROOT/.env}"
ENV_EXAMPLE="$REPO_ROOT/.env.example"

step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }

for tool in git terraform gcloud aws curl; do
  command -v "$tool" >/dev/null || die "$tool is not installed"
done
[ -d "$TF_DIR" ] || die "no Terraform environment at $TF_DIR"
if [ "$PLAN_ONLY" = 0 ] && [ ! -t 0 ]; then
  die "stdin is not a terminal; an apply needs a typed yes (use ARGS=--plan-only to only plan)"
fi

# --- 1. Load .env ----------------------------------------------------------------
step "Loading $ENV_FILE"
[ -f "$ENV_FILE" ] || die "$ENV_FILE does not exist; copy .env.example and fill in the TF_VAR_* values (from a worktree, pass ENV_FILE=<primary checkout>/.env)"
# .env is shell syntax written for `set -a; source .env; set +a`; nounset is off
# while it runs so a line that references an unset variable does not abort.
set +u
set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a
set -u

# --- 2. Every TF_VAR_ secret is set --------------------------------------------------
step "Checking the TF_VAR_* values listed in .env.example"
mapfile -t required < <(grep -oE '^TF_VAR_[A-Za-z0-9_]+' "$ENV_EXAMPLE" | sort -u)
[ "${#required[@]}" -gt 0 ] || die "found no TF_VAR_* names in $ENV_EXAMPLE"
missing=()
for name in "${required[@]}"; do
  [ -n "${!name:-}" ] || missing+=("$name")
done
if [ "${#missing[@]}" -gt 0 ]; then
  printf '  %s\n' "${missing[@]}" >&2
  die "the TF_VAR_* above are empty or absent in $ENV_FILE; an apply without them destroys their secret versions, so nothing was planned"
fi
echo "all ${#required[@]} set"

# --- 3. Credentials ------------------------------------------------------------------
step "Checking credentials"
gcloud auth application-default print-access-token >/dev/null 2>&1 \
  || die "no valid gcloud application-default credentials; run 'gcloud auth application-default login'"
echo "gcloud application-default credentials: ok"

aws_profile="${TF_VAR_aws_profile:-multiticketing-ses}"
aws sts get-caller-identity --profile "$aws_profile" >/dev/null 2>&1 \
  || die "AWS profile '$aws_profile' has no valid session; run 'aws sso login --profile $aws_profile'"
echo "AWS profile $aws_profile: ok"

# The header goes in on stdin so the token never appears in the process list.
# shellcheck disable=SC2154 # set by the sourced .env, checked non-empty above
printf 'Authorization: Bearer %s\n' "$TF_VAR_sentry_auth_token" \
  | curl -sf -o /dev/null -H @- https://sentry.io/api/0/organizations/redplanettribe/ \
  || die "Sentry refused TF_VAR_sentry_auth_token (or is unreachable); see terraform/README.md, \"Sentry\""
echo "Sentry token: ok"

# --- What is being applied -------------------------------------------------------------
sha="$(git rev-parse --short HEAD)"
step "Planning $TF_DIR at $sha ($(git log -1 --format=%s))"
git fetch -q origin main 2>/dev/null || warn "could not fetch origin/main"
if ! git merge-base --is-ancestor HEAD origin/main 2>/dev/null; then
  warn "HEAD is not on origin/main; this applies infrastructure nobody can see on GitHub"
elif ! git diff --quiet HEAD origin/main -- terraform; then
  warn "origin/main has Terraform changes this checkout lacks; the plan would undo them"
fi
if [ -n "$(git status --porcelain -- terraform)" ]; then
  warn "terraform/ has uncommitted changes; they are part of this plan"
fi

# --- 4. Plan -------------------------------------------------------------------------------
plan_dir="$(mktemp -d)"
chmod 700 "$plan_dir"
trap 'rm -rf "$plan_dir"' EXIT
trap 'exit 130' INT TERM
planfile="$plan_dir/tfplan"

terraform -chdir="$TF_DIR" init -input=false >/dev/null \
  || die "terraform init failed in $TF_DIR; run it there by hand to see why"

# -detailed-exitcode: 0 no changes, 1 error, 2 changes present.
set +e
terraform -chdir="$TF_DIR" plan -input=false -detailed-exitcode -out="$planfile"
plan_status=$?
set -e
case "$plan_status" in
  0) step "No changes; nothing to apply"; exit 0 ;;
  2) ;;
  *) die "terraform plan failed" ;;
esac

if [ "$PLAN_ONLY" = 1 ]; then
  step "--plan-only: stopping without applying; the planfile is deleted"
  exit 0
fi

# --- 5. Approve and apply ------------------------------------------------------------------
printf '\n'
read -r -p "Apply this plan to $TF_ENV? [y/N] " answer
case "$answer" in
  y|Y|yes|YES|Yes) ;;
  *) step "Not applied; the planfile is deleted"; exit 0 ;;
esac

step "Applying the approved plan"
terraform -chdir="$TF_DIR" apply -input=false "$planfile"
step "Applied. A second 'make tf-apply ARGS=--plan-only' should report no changes"
