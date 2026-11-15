#!/usr/bin/env bash
# Throwaway probe for #716: ask the SRI's two status services what they say
# about a set of claves, from Cloud Run in the prod project (the SRI resets
# connections from the dev machine).
#
# It creates a one-off Cloud Run job running curl, executes it once, prints its
# log, and deletes the job. Read-only towards the SRI: only WSDL fetches,
# consultarEstadoAutorizacionComprobante and autorizacionComprobante.
# Prod services use PRIVATE_RANGES_ONLY egress, so their SRI calls leave by
# Cloud Run's default internet path; this job (no VPC) takes the same path.
#
# Usage: [REPEAT=3] [SPACING=0] scripts/probe-sri-status.sh [ANNULLED_CLAVE]
#   ANNULLED_CLAVE  a production factura clave the SRI holds as ANULADO
#                   (annulled at the SRI en línea portal). Optional; without it
#                   the annulled case is not probed.
#   REPEAT          consultas per clave (default 3)
#   SPACING         seconds between consultas (default 0)
set -euo pipefail

PROJECT=multiticketing
REGION=us-east1
JOB=probe-716-sri-status
REPEAT=${REPEAT:-3}
SPACING=${SPACING:-0}
LOG=${LOG:-probe-716-$(date +%Y%m%dT%H%M%S).log}

# label=clave pairs. Production claves from prod's invoicing_invoices_ec.
CLAVES="authorized-225=1809202601179322846800120010010000002250312603419"
CLAVES+=" never-held-25=3008202601179322846800120010010000000252802681612"
CLAVES+=" needs-attention-117=0709202601179322846800120010010000001172290597311"
CLAVES+=" malformed=0000000000000000000000000000000000000000000000000"
if [[ $# -ge 1 ]]; then CLAVES+=" annulled=$1"; fi

read -r -d '' PROBE <<'EOF' || true
set -u
out() { printf '\n===== %s\n' "$*"; }
call() { # url body
  curl -sS -m 30 -H 'Content-Type: text/xml; charset=utf-8' -H 'SOAPAction: ""' \
    -w '\n[http %{http_code}, connect %{time_connect}s, total %{time_total}s]\n' \
    --data "$2" "$1" 2>&1
}
consulta() { # host clave
  call "https://$1.sri.gob.ec/comprobantes-electronicos-ws/ConsultaComprobante" \
    "<soapenv:Envelope xmlns:soapenv=\"http://schemas.xmlsoap.org/soap/envelope/\" xmlns:ec=\"http://ec.gob.sri.ws.consultas\"><soapenv:Header/><soapenv:Body><ec:consultarEstadoAutorizacionComprobante><claveAcceso>$2</claveAcceso></ec:consultarEstadoAutorizacionComprobante></soapenv:Body></soapenv:Envelope>"
}
autorizacion() { # host clave
  call "https://$1.sri.gob.ec/comprobantes-electronicos-ws/AutorizacionComprobantesOffline" \
    "<soapenv:Envelope xmlns:soapenv=\"http://schemas.xmlsoap.org/soap/envelope/\" xmlns:ec=\"http://ec.gob.sri.ws.autorizacion\"><soapenv:Header/><soapenv:Body><ec:autorizacionComprobante><claveAccesoComprobante>$2</claveAccesoComprobante></ec:autorizacionComprobante></soapenv:Body></soapenv:Envelope>"
}
out "egress"; curl -sS -m 10 https://ifconfig.co/json 2>&1 | head -c 400; echo
for host in cel celcer; do
  for path in "ConsultaComprobante?wsdl" "ConsultaComprobante?xsd=1" "AutorizacionComprobantesOffline?wsdl"; do
    out "GET $host $path"
    curl -sS -m 30 -w '\n[http %{http_code}, total %{time_total}s]\n' \
      "https://$host.sri.gob.ec/comprobantes-electronicos-ws/$path" 2>&1
  done
done
for pair in $CLAVES; do
  label=${pair%%=*}; clave=${pair#*=}
  i=1; while [ "$i" -le "$REPEAT" ]; do
    out "consulta cel $label #$i $(date -u +%H:%M:%S)"; consulta cel "$clave"
    i=$((i + 1)); sleep "$SPACING"
  done
  out "autorizacion cel $label"; autorizacion cel "$clave"
done
first=${CLAVES%% *}
out "consulta celcer (wrong environment) ${first%%=*}"; consulta celcer "${first#*=}"
out "done"
EOF

B64=$(printf '%s' "$PROBE" | base64 -w0)

cleanup() { gcloud run jobs delete "$JOB" --project "$PROJECT" --region "$REGION" --quiet >/dev/null 2>&1 || true; }
trap cleanup EXIT

gcloud run jobs create "$JOB" --project "$PROJECT" --region "$REGION" \
  --image docker.io/curlimages/curl:latest \
  --command sh --args '^@^-c@echo "$PROBE_B64" | base64 -d | sh' \
  --set-env-vars "^@^PROBE_B64=$B64@CLAVES=$CLAVES@REPEAT=$REPEAT@SPACING=$SPACING" \
  --max-retries 0 --task-timeout 600s --quiet

gcloud run jobs execute "$JOB" --project "$PROJECT" --region "$REGION" --wait || true

sleep 20 # Cloud Logging lags the execution
gcloud logging read \
  "resource.type=cloud_run_job AND resource.labels.job_name=$JOB" \
  --project "$PROJECT" --freshness 1h --order asc --format 'value(textPayload)' --limit 5000 \
  | tee "$LOG"
echo "log saved to $LOG"
