#!/usr/bin/env bash
# Install gha-scheduler on homelab k3s (requires gateway-stack + images built).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFESTS="${ROOT}/manifests"
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

: "${GHA_WEBHOOK_HOSTNAME:?set GHA_WEBHOOK_HOSTNAME e.g. gha-scheduler.example.com}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

require_gateway() {
  kubectl -n gateway get gateway ts-gateway >/dev/null 2>&1 || \
    die "ts-gateway not found — run cluster/kubernetes/private/install.sh gateway-stack first"
}

apply_manifest() {
  kubectl apply -f "$1"
}

apply_ingress() {
  local tmp
  tmp="$(mktemp)"
  export GHA_WEBHOOK_HOSTNAME
  envsubst '${GHA_WEBHOOK_HOSTNAME}' < "${MANIFESTS}/ingress.yaml" > "${tmp}"
  kubectl apply -f "${tmp}"
  rm -f "${tmp}"
}

ensure_s3_credentials() {
  if kubectl -n gha-runners get secret gha-cache-s3-credentials >/dev/null 2>&1; then
    log "Using existing secret gha-cache-s3-credentials"
    return
  fi
  if [[ -n "${GHA_S3_ACCESS_KEY:-}" && -n "${GHA_S3_SECRET_KEY:-}" ]]; then
    log "Creating gha-cache-s3-credentials from env"
    kubectl -n gha-runners create secret generic gha-cache-s3-credentials \
      --from-literal=access-key="${GHA_S3_ACCESS_KEY}" \
      --from-literal=secret-key="${GHA_S3_SECRET_KEY}"
    return
  fi
  die "Create gha-cache-s3-credentials (see manifests/cache-s3-secret.example.yaml) or set GHA_S3_ACCESS_KEY + GHA_S3_SECRET_KEY"
}

apply_seaweedfs_config() {
  local access_key secret_key tmp
  access_key="$(kubectl -n gha-runners get secret gha-cache-s3-credentials -o jsonpath='{.data.access-key}' | base64 -d)"
  secret_key="$(kubectl -n gha-runners get secret gha-cache-s3-credentials -o jsonpath='{.data.secret-key}' | base64 -d)"
  tmp="$(mktemp)"
  sed "s/REPLACE_S3_ACCESS_KEY/${access_key}/g; s/REPLACE_S3_SECRET_KEY/${secret_key}/g" \
    < "${MANIFESTS}/seaweedfs-s3-config.example.yaml" > "${tmp}"
  kubectl apply -f "${tmp}"
  rm -f "${tmp}"
}

install_seaweedfs() {
  if kubectl -n gha-runners get deploy seaweedfs >/dev/null 2>&1; then
    log "SeaweedFS already deployed — refreshing s3 config"
    apply_seaweedfs_config
    return
  fi
  log "Installing SeaweedFS S3 cache backend"
  apply_seaweedfs_config
  apply_manifest "${MANIFESTS}/seaweedfs.yaml"
  kubectl -n gha-runners rollout status deployment/seaweedfs --timeout=5m
}

log "Precheck gateway"
require_gateway

log "Applying gha-scheduler namespace"
apply_manifest "${MANIFESTS}/namespace.yaml"

ensure_s3_credentials
install_seaweedfs

log "Applying gha-scheduler manifests"
apply_manifest "${MANIFESTS}/serviceaccount.yaml"
apply_manifest "${MANIFESTS}/rbac.yaml"
apply_manifest "${MANIFESTS}/configmap.yaml"

if kubectl -n gha-runners get secret gha-scheduler-secrets >/dev/null 2>&1; then
  log "Using existing secret gha-scheduler-secrets"
else
  die "Create gha-scheduler-secrets (see manifests/secrets.example.yaml)"
fi

apply_manifest "${MANIFESTS}/deployment.yaml"
apply_manifest "${MANIFESTS}/service.yaml"
apply_ingress

log "Waiting for rollout"
kubectl -n gha-runners rollout status deployment/gha-scheduler --timeout=5m

log "Health check"
kubectl -n gha-runners get pods -l app=gha-scheduler
log "Webhook URL: https://${GHA_WEBHOOK_HOSTNAME}/webhook"
log "Next: ./scripts/smoke.sh && ./scripts/canary-check.sh && deploy/CANARY.md"
