#!/usr/bin/env bash
#
# Sentry's server-side data scrubbing on the three ticket-pos projects
# (ADR 0079, #777): the second net behind the scrubbing every deployable does in
# its own process before an event leaves it.
#
# The Terraform provider (jianyuan/sentry 0.16) cannot declare scrubbing at
# either level: sentry_project has no scrubbing argument and sentry_organization
# only a name and slug. So this is one API call per project, kept here, run by
# the operator after the apply that creates the projects, and again whenever
# this file changes. It is idempotent: it sets the same values every time.
#
#   SENTRY_AUTH_TOKEN=... scripts/sentry-scrubbing.sh            # apply
#   scripts/sentry-scrubbing.sh --dry-run                        # print, send nothing
#
# Project settings only, never organization settings: the redplanettribe
# organization is shared with another product, and an organization-wide
# scrubbing setting would change its projects too. The three slugs are fixed
# here; the script touches no other project.
#
# What it sets on each project (Settings, Security & Privacy):
#   - Data Scrubber on, with Sentry's default sensitive fields (passwords,
#     secrets, tokens, credit card numbers and the like).
#   - IP addresses not stored.
#   - Extra sensitive fields, matched against key names in the event data:
#     this product's personal data words (EXTRA_SENSITIVE_FIELDS below).
#   - Advanced data scrubbing on every string value: email addresses replaced,
#     credit card numbers masked, and Ecuadorian identity numbers (a cedula, a
#     RUC, or a cedula written with its check digit after a hyphen) replaced,
#     the shapes the in-process scrubbers redact (packages/sentry-rules/src/
#     scrub.ts and the API's reporter).
#
# The token is the operator's Terraform token (TF_VAR_sentry_auth_token), which
# has project:write; the release token (#776, org:ci) cannot change project
# settings. SENTRY_URL overrides the API's address, for trying this without
# Sentry. It stops at the first failure and says which project it was on.
set -euo pipefail

SENTRY_URL="${SENTRY_URL:-https://sentry.io}"
SENTRY_ORG="redplanettribe"
SENTRY_PROJECTS=(ticket-pos-api ticket-pos-staff ticket-pos-storefront)

EXTRA_SENSITIVE_FIELDS='["email", "phone", "cedula", "identity", "identification", "ruc", "address", "answer"]'

# Relay's PII configuration, the API's relayPiiConfig: rules by id, applied to
# every string ($string). @email:replace and @creditcard:mask are Sentry's
# built-in rules. The identity number pattern is a Rust regex, which has no
# look-behind, so \b stands in for the in-process scrubbers' "no letter or
# digit on either side".
RELAY_PII_CONFIG='{
  "rules": {
    "identity-number": {
      "type": "pattern",
      "pattern": "\\b(?:0[1-9]|1[0-9]|2[0-4]|30)(?:[0-9]{11}|[0-9]{8}|[0-9]{7}-[0-9])\\b",
      "redaction": { "method": "replace", "text": "[identity-number]" }
    }
  },
  "applications": {
    "$string": ["@email:replace", "@creditcard:mask", "identity-number"]
  }
}'

dry_run=false
case "${1:-}" in
  "") ;;
  --dry-run) dry_run=true ;;
  *)
    echo "usage: $0 [--dry-run]" >&2
    exit 2
    ;;
esac

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }

# relayPiiConfig travels as a JSON string inside the JSON body, as the
# settings page sends it.
body="$(jq -n \
  --argjson fields "$EXTRA_SENSITIVE_FIELDS" \
  --arg pii "$(jq -c . <<<"$RELAY_PII_CONFIG")" \
  '{
    dataScrubber: true,
    dataScrubberDefaults: true,
    scrubIPAddresses: true,
    sensitiveFields: $fields,
    relayPiiConfig: $pii
  }')"

if $dry_run; then
  for project in "${SENTRY_PROJECTS[@]}"; do
    echo "PUT $SENTRY_URL/api/0/projects/$SENTRY_ORG/$project/"
  done
  echo "$body"
  exit 0
fi

if [ -z "${SENTRY_AUTH_TOKEN:-}" ]; then
  echo "error: SENTRY_AUTH_TOKEN is not set (the Terraform token, TF_VAR_sentry_auth_token)" >&2
  exit 1
fi

for project in "${SENTRY_PROJECTS[@]}"; do
  url="$SENTRY_URL/api/0/projects/$SENTRY_ORG/$project/"
  response="$(mktemp)"
  status="$(curl --silent --show-error --max-time 30 \
    --output "$response" --write-out '%{http_code}' \
    --request PUT "$url" \
    --header "Authorization: Bearer $SENTRY_AUTH_TOKEN" \
    --header "Content-Type: application/json" \
    --data "$body")" || { rm -f "$response"; echo "error: $project: request failed" >&2; exit 1; }

  if [ "$status" != "200" ]; then
    echo "error: $project: HTTP $status" >&2
    head -c 2000 "$response" >&2
    echo >&2
    rm -f "$response"
    exit 1
  fi

  # Sentry answers with the project as saved; read the settings back from it
  # rather than trusting the 200.
  if ! jq -e --argjson want "$body" '
      .dataScrubber == $want.dataScrubber
      and .dataScrubberDefaults == $want.dataScrubberDefaults
      and .scrubIPAddresses == $want.scrubIPAddresses
      and (.sensitiveFields | sort) == ($want.sensitiveFields | sort)
      and (.relayPiiConfig | fromjson) == ($want.relayPiiConfig | fromjson)
    ' "$response" >/dev/null; then
    echo "error: $project: Sentry answered 200 but did not keep every setting; check Settings, Security & Privacy" >&2
    rm -f "$response"
    exit 1
  fi
  rm -f "$response"
  echo "$project: scrubbing set"
done
