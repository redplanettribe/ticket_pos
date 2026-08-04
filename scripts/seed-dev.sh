#!/usr/bin/env bash
#
# Restore the dev seed data into the running local stack.
#
# The dev seed lives in ordinary migrations (`backend/migrations/*seed_dev*.sql`)
# and is therefore applied exactly once, when a fresh database is migrated from
# nothing. That is the right shape for a dev machine and the wrong shape after
# `make prod-to-local`: production has never held these rows, but its
# `schema_migrations` records their versions as applied, so the restored
# database arrives with the seed permanently missing and the migrator convinced
# it is already there.
#
# The e2e suite is what notices. It buys from `midnight-synth-live` and reads
# `sunrise-jazz-brunch` and `unlisted-loft-session` by slug, so 10 of its 14
# specs fail on a database that has no demo Organization — for a reason that
# looks nothing like the cause.
#
# Rerunning the seed files is safe because every one of them was written to be:
# fixed UUIDs and ON CONFLICT DO NOTHING throughout. So this is not a repair
# with a blast radius, and it is not `docker compose down -v` either — the
# production copy someone deliberately pulled stays exactly where it is.
#
# Usage:
#   make seed-dev     # after make prod-to-local, or any time e2e cannot find its fixtures
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Local dev stack, matching docker-compose.yml.
LOCAL_PG_USER="${LOCAL_PG_USER:-ticket_pos}"
LOCAL_PG_DB="${LOCAL_PG_DB:-ticket_pos}"
PG_CONTAINER="${PG_CONTAINER:-ticket_pos-postgres-1}"
STOREFRONT_CONTAINER="${STOREFRONT_CONTAINER:-ticket_pos-storefront-1}"

log() { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
die() {
  printf '\033[1;31merror:\033[0m %s\n' "$*" >&2
  exit 1
}

command -v docker >/dev/null || die "docker is not installed"
docker inspect "$PG_CONTAINER" >/dev/null 2>&1 ||
  die "$PG_CONTAINER is not running. Start the stack first: make dev"

log "Applying dev seed migrations"
# Globbed rather than listed, so a seed migration added later is picked up
# without this script being the thing that was forgotten.
shopt -s nullglob
seeds=("$REPO_ROOT"/backend/migrations/*seed_dev*.sql)
((${#seeds[@]})) || die "no *seed_dev*.sql migrations found under backend/migrations"

for seed in "${seeds[@]}"; do
  name="$(basename "$seed")"
  printf '  %s\n' "$name"
  docker exec -i "$PG_CONTAINER" \
    psql -U "$LOCAL_PG_USER" -d "$LOCAL_PG_DB" -q -v ON_ERROR_STOP=1 <"$seed" ||
    die "failed applying $name"
done

# The Storefront walks the public event list to build /sitemap.xml and caches
# that walk for an hour (apps/storefront/app/sitemap.ts). Next keeps the cache
# on disk, so it outlives a container restart — seeded Events would be live on
# every page and absent from the sitemap until the hour was up, which is one
# more e2e failure with a cause nobody would guess.
if docker inspect "$STOREFRONT_CONTAINER" >/dev/null 2>&1; then
  log "Clearing the Storefront's fetch cache"
  docker exec "$STOREFRONT_CONTAINER" \
    rm -rf /app/apps/storefront/.next/cache/fetch-cache 2>/dev/null || true
  docker restart "$STOREFRONT_CONTAINER" >/dev/null
  printf '  waiting for the Storefront'
  for _ in $(seq 1 30); do
    if curl -sf -o /dev/null http://localhost:64300/en; then
      break
    fi
    printf '.'
    sleep 2
  done
  echo
fi

log "Done"
cat <<'EOF'
The demo Organization and its Events are back. The production data alongside
them is untouched.

  pnpm --filter @ticket-pos/e2e test
EOF
