#!/usr/bin/env bash
# Idempotently add gha-scheduler (and peers) to a remotely-managed Cloudflare Tunnel.
set -euo pipefail

log() { printf '==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

: "${GHA_WEBHOOK_HOSTNAME:?set GHA_WEBHOOK_HOSTNAME}"
: "${GHA_TUNNEL_SERVICE_URL:=http://gha-scheduler.gha-runners.svc.cluster.local:8080}"

CF_API_TOKEN="${GHA_CF_API_TOKEN:-${CLOUDFLARE_API_TOKEN:-}}"
CF_ACCOUNT_ID="${GHA_CF_ACCOUNT_ID:-${CLOUDFLARE_ACCOUNT_ID:-}}"
CF_TUNNEL_ID="${GHA_CF_TUNNEL_ID:-${CLOUDFLARE_TUNNEL_ID:-}}"
KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

if [[ -z "${CF_API_TOKEN}" && -n "${KUBECONFIG:-}" ]]; then
  if kubectl -n gateway get secret cloudflare-api-token >/dev/null 2>&1; then
    CF_API_TOKEN="$(kubectl -n gateway get secret cloudflare-api-token -o jsonpath='{.data.cloudflare-api-token}' | base64 -d)"
  fi
fi

[[ -n "${CF_API_TOKEN}" ]] || die "set GHA_CF_API_TOKEN or CLOUDFLARE_API_TOKEN (Cloudflare One Connectors Write + Zone DNS)"

cf_api() {
  local method="$1" path="$2"
  shift 2
  curl -sfS -X "${method}" \
    -H "Authorization: Bearer ${CF_API_TOKEN}" \
    -H "Content-Type: application/json" \
  "https://api.cloudflare.com/client/v4${path}" "$@"
}

if [[ -z "${CF_ACCOUNT_ID}" ]]; then
  log "Resolving Cloudflare account ID"
  CF_ACCOUNT_ID="$(cf_api GET /accounts | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["result"][0]["id"] if d.get("result") else "")')"
  [[ -n "${CF_ACCOUNT_ID}" ]] || die "could not resolve account ID — set GHA_CF_ACCOUNT_ID"
fi

if [[ -z "${CF_TUNNEL_ID}" ]]; then
  log "Resolving tunnel ID"
  CF_TUNNEL_ID="$(cf_api GET "/accounts/${CF_ACCOUNT_ID}/cfd_tunnel" | python3 -c '
import json, sys
data = json.load(sys.stdin)
for t in data.get("result", []):
    if t.get("deleted_at") in (None, ""):
        print(t["id"])
        break
')"
  [[ -n "${CF_TUNNEL_ID}" ]] || die "could not resolve tunnel ID — set GHA_CF_TUNNEL_ID"
fi

log "Tunnel ${CF_TUNNEL_ID} (account ${CF_ACCOUNT_ID})"

CURRENT="$(cf_api GET "/accounts/${CF_ACCOUNT_ID}/cfd_tunnel/${CF_TUNNEL_ID}/configurations")"

UPDATED="$(echo "${CURRENT}" | python3 - "${GHA_WEBHOOK_HOSTNAME}" "${GHA_TUNNEL_SERVICE_URL}" <<'PY'
import json, sys

hostname, service = sys.argv[1:3]
current = json.load(sys.stdin)
ingress = current.get("result", {}).get("config", {}).get("ingress", [])

required = [
    ("sanad.itchallenge.fr", "http://nova-app.prod-nova.svc.cluster.local:8080"),
    ("sanad-admin.itchallenge.fr", "http://admin-app.prod-nova.svc.cluster.local:9090"),
    ("dex.itchallenge.fr", "http://dex.prod-nova.svc.cluster.local:5556"),
    (hostname, service),
]

by_host = {}
catch_all = None
for rule in ingress:
    host = rule.get("hostname")
    if host:
        by_host[host] = rule
    elif rule.get("service"):
        catch_all = rule

for host, svc in required:
    by_host.setdefault(host, {"hostname": host, "service": svc, "originRequest": {}})

ordered = []
for host, _ in required:
    if host in by_host:
        ordered.append(by_host.pop(host))
ordered.extend(by_host.values())
ordered.append(catch_all or {"service": "http_status:404"})

print(json.dumps({"config": {"ingress": ordered}}))
PY
)"

cf_api PUT "/accounts/${CF_ACCOUNT_ID}/cfd_tunnel/${CF_TUNNEL_ID}/configurations" -d "${UPDATED}" >/dev/null

log "Updated tunnel ingress (includes ${GHA_WEBHOOK_HOSTNAME})"

# DNS: ensure CNAME exists for webhook hostname
ZONE_NAME="${GHA_CF_ZONE_NAME:-itchallenge.fr}"
ZONE_ID="$(cf_api GET "/zones?name=${ZONE_NAME}" | python3 -c 'import json,sys; r=json.load(sys.stdin).get("result",[]); print(r[0]["id"] if r else "")')"
if [[ -n "${ZONE_ID}" ]]; then
  RECORD_NAME="${GHA_WEBHOOK_HOSTNAME%.${ZONE_NAME}}"
  RECORD_NAME="${RECORD_NAME%.}"  # strip trailing dot if any
  EXISTING="$(cf_api GET "/zones/${ZONE_ID}/dns_records?type=CNAME&name=${GHA_WEBHOOK_HOSTNAME}" || true)"
  if ! echo "${EXISTING}" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("result") else 1)' 2>/dev/null; then
    log "Creating DNS CNAME ${GHA_WEBHOOK_HOSTNAME} -> ${CF_TUNNEL_ID}.cfargotunnel.com"
    cf_api POST "/zones/${ZONE_ID}/dns_records" \
      --data "{\"type\":\"CNAME\",\"proxied\":true,\"name\":\"${RECORD_NAME}\",\"content\":\"${CF_TUNNEL_ID}.cfargotunnel.com\"}" >/dev/null
  else
    log "DNS record exists for ${GHA_WEBHOOK_HOSTNAME}"
  fi
fi

log "Waiting for tunnel config propagation"
for _ in $(seq 1 12); do
  if curl -sf --max-time 10 "https://${GHA_WEBHOOK_HOSTNAME}/healthz" >/dev/null 2>&1; then
    log "OK: https://${GHA_WEBHOOK_HOSTNAME}/healthz"
    exit 0
  fi
  sleep 5
done

die "healthz still failing — check gha-scheduler pod and tunnel logs"
