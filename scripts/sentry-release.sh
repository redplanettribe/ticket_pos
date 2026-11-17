#!/usr/bin/env bash
#
# Tell Sentry about a release and its deploys (ADR 0079, #776). Used by
# .github/workflows/deploy.yml and scripts/deploy.sh, at the same points:
#
#   scripts/sentry-release.sh create <sha>            # after the migration
#   scripts/sentry-release.sh deploy <sha> <service>  # after each service deploys
#
# A release is a commit: <sha> is the commit every image was built from, the
# SENTRY_RELEASE each deployable reports. "create" makes that release in all
# three projects and finalizes it; "deploy" records a deploy of it to the
# production environment, named after the Cloud Run service.
#
# It reads the token from SENTRY_AUTH_TOKEN (the GitHub Actions secret of the
# same name; an organization auth token, which has the release scope). With
# none, it does nothing.
#
# It never fails: an unreachable Sentry, a wrong token or a missing CLI is one
# warning line and exit 0, and every call is bounded by a timeout. The
# monitoring vendor can never block a release.
#
# The CLI is the `sentry` npm package at the version @sentry/nextjs's build
# plugin uses for the source map upload (see pnpm-lock.yaml), run through npx.
# SENTRY_CLI overrides the command, for trying this without Sentry.
# No `set -e`, unlike the other scripts: a failed call is handled, never fatal.
set -uo pipefail

SENTRY_ORG="redplanettribe"
SENTRY_PROJECTS="ticket-pos-api,ticket-pos-staff,ticket-pos-storefront"
SENTRY_ENVIRONMENT="production"
SENTRY_CLI_VERSION="0.49.0"
# Seconds for one call, npx's first download of the CLI included.
TIMEOUT="${SENTRY_RELEASE_TIMEOUT:-120}"

warn() {
  if [ "${GITHUB_ACTIONS:-}" = "true" ]; then
    echo "::warning title=Sentry::$*"
  else
    echo "warning: $*" >&2
  fi
}

usage() {
  echo "usage: $0 create <sha> | deploy <sha> <service>" >&2
  exit 2
}

# The token passed is the only one used: never a login stored on this machine.
# The CLI reports nothing about itself.
sentry_cli() {
  local cli
  read -r -a cli <<<"${SENTRY_CLI:-npx --yes sentry@${SENTRY_CLI_VERSION}}"
  SENTRY_FORCE_ENV_TOKEN=1 SENTRY_CLI_NO_TELEMETRY=1 timeout "$TIMEOUT" "${cli[@]}" "$@"
}

command="${1:-}"
sha="${2:-}"
[ -n "$sha" ] || usage
case "$command" in
  create) [ "$#" -eq 2 ] || usage ;;
  deploy) [ "$#" -eq 3 ] || usage ;;
  *) usage ;;
esac

if [ -z "$(printf '%s' "${SENTRY_AUTH_TOKEN:-}" | tr -d '[:space:]')" ]; then
  echo "SENTRY_AUTH_TOKEN is not set; nothing is sent to Sentry."
  exit 0
fi

case "$command" in
  create)
    if sentry_cli release create "${SENTRY_ORG}/${sha}" --project "$SENTRY_PROJECTS" --finalize; then
      echo "Sentry release ${sha} created in ${SENTRY_PROJECTS}."
    else
      warn "could not create the Sentry release ${sha} (exit $?); the deploy goes on without it."
    fi
    ;;
  deploy)
    service="$3"
    if sentry_cli release deploy "${SENTRY_ORG}/${sha}" "$SENTRY_ENVIRONMENT" "$service"; then
      echo "Sentry deploy of ${sha} recorded for ${service}."
    else
      warn "could not record the Sentry deploy of ${sha} for ${service} (exit $?); the deploy goes on without it."
    fi
    ;;
esac
exit 0
