#!/usr/bin/env bash
# Pre-canary validation checklist (automated checks).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

log() { printf '==> %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

log "Offline smoke"
"${ROOT}/scripts/smoke.sh"

log "Unit tests"
cd "${ROOT}" && go test ./... -count=1

log "Scheduler deployment"
kubectl -n gha-runners get deploy/gha-scheduler >/dev/null 2>&1 || fail "gha-scheduler not deployed"
kubectl -n gha-runners rollout status deployment/gha-scheduler --timeout=2m

log "SeaweedFS S3"
kubectl -n gha-runners get svc seaweedfs-s3 >/dev/null 2>&1 || fail "seaweedfs-s3 service missing"
kubectl -n gha-runners get deploy seaweedfs >/dev/null 2>&1 || fail "seaweedfs deployment missing"

READY="$(kubectl -n gha-runners get pods -l app=gha-scheduler -o jsonpath='{.items[0].status.conditions[?(@.type=="Ready")].status}')"
[[ "$READY" == "True" ]] || fail "scheduler pod not Ready"

if [[ -n "${GHA_WEBHOOK_HOSTNAME:-}" ]]; then
  log "Healthz via gateway hostname"
  curl -sf "https://${GHA_WEBHOOK_HOSTNAME}/healthz" || fail "healthz check failed"
fi

if [[ -n "${GHA_APP_ID:-}" && -n "${GHA_INSTALLATION_ID:-}" ]]; then
  log "Live integration (generate-jit-config)"
  "${ROOT}/scripts/integration.sh"
fi

log "Canary prechecks passed. See deploy/CANARY.md for webhook switch + measurement."
log "Grafana: docs/grafana-canary.md"
