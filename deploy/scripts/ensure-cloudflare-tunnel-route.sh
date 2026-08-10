#!/usr/bin/env bash
# Idempotently add gha-scheduler to a remotely-managed Cloudflare Tunnel via API.
set -euo pipefail

log() { printf '==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

: "${GHA_WEBHOOK_HOSTNAME:?set GHA_WEBHOOK_HOSTNAME}"
: "${GHA_TUNNEL_SERVICE_URL:=http://gha-scheduler.gha-runners.svc.cluster.local:8080}"

CF_API_TOKEN="${GHA_CF_API_TOKEN:-${CLOUDFLARE_API_TOKEN:-}}"
CF_ACCOUNT_ID="${GHA_CF_ACCOUNT_ID:-${CLOUDFLARE_ACCOUNT_ID:-}}"
CF_TUNNEL_ID="${GHA_CF_TUNNEL_ID:-${CLOUDFLARE_TUNNEL_ID:-}}"
KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

load_tunnel_ids_from_cluster() {
  command -v kubectl >/dev/null 2>&1 || return 0
  command -v python3 >/dev/null 2>&1 || return 0
  kubectl -n gateway get secret cloudflared-credentials >/dev/null 2>&1 || return 0

  local decoded
  decoded="$(kubectl -n gateway get secret cloudflared-credentials -o jsonpath='{.data.token}' | base64 -d | python3 -c '
import base64, json, sys
token = sys.stdin.read().strip()
payload = json.loads(base64.b64decode(token))
print(payload["a"])
print(payload["t"])
')"
  CF_ACCOUNT_ID="${CF_ACCOUNT_ID:-$(printf "%s" "$decoded" | sed -n '1p')}"
  CF_TUNNEL_ID="${CF_TUNNEL_ID:-$(printf "%s" "$decoded" | sed -n '2p')}"
}

if [[ -z "${CF_API_TOKEN}" && -n "${KUBECONFIG:-}" ]]; then
  if kubectl -n gateway get secret cloudflare-api-token >/dev/null 2>&1; then
    CF_API_TOKEN="$(kubectl -n gateway get secret cloudflare-api-token -o jsonpath='{.data.api-token}' | base64 -d)"
  fi
fi

load_tunnel_ids_from_cluster

[[ -n "${CF_API_TOKEN}" ]] || die "set GHA_CF_API_TOKEN (needs Account → Cloudflare One Connectors → cloudflared:Edit)"

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
  CF_ACCOUNT_ID="$(cf_api GET "/zones?name=${GHA_CF_ZONE_NAME:-itchallenge.fr}" | python3 -c 'import json,sys; r=json.load(sys.stdin).get("result",[]); print(r[0]["account"]["id"] if r else "")')"
  [[ -n "${CF_ACCOUNT_ID}" ]] || die "could not resolve account ID — set GHA_CF_ACCOUNT_ID"
fi

[[ -n "${CF_TUNNEL_ID}" ]] || die "could not resolve tunnel ID — set GHA_CF_TUNNEL_ID"

log "Tunnel ${CF_TUNNEL_ID} (account ${CF_ACCOUNT_ID})"

CURRENT="$(cf_api GET "/accounts/${CF_ACCOUNT_ID}/cfd_tunnel/${CF_TUNNEL_ID}/configurations" 2>/dev/null)" || CURRENT=""

if [[ -z "${CURRENT}" ]] || ! echo "${CURRENT}" | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("success") else 1)' 2>/dev/null; then
  die "$(cat <<EOF
Cloudflare API rejected tunnel configuration access.
Create an API token with Account → Cloudflare One Connectors → cloudflared:Edit, set:
  export GHA_CF_API_TOKEN=<token>
Then re-run install.sh.

Or add Public Hostname manually:
  https://one.dash.cloudflare.com/${CF_ACCOUNT_ID}/networks/connectors/tunnels/${CF_TUNNEL_ID}
  Hostname: ${GHA_WEBHOOK_HOSTNAME}
  Service:  ${GHA_TUNNEL_SERVICE_URL}
EOF
)"
fi

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

for _ in $(seq 1 12); do
  if curl -sf --max-time 10 "https://${GHA_WEBHOOK_HOSTNAME}/healthz" >/dev/null 2>&1; then
    log "OK: https://${GHA_WEBHOOK_HOSTNAME}/healthz"
    exit 0
  fi
  sleep 5
done

die "healthz still failing — check gha-scheduler pod: kubectl -n gha-runners get pods -l app=gha-scheduler"
