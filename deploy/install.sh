#!/usr/bin/env bash
# Install gha-scheduler on homelab k3s.
# Public webhook: Cloudflare Tunnel (default). Optional: Envoy HTTPRoute for Tailscale-only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MANIFESTS="${ROOT}/manifests"
export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

: "${GHA_WEBHOOK_HOSTNAME:?set GHA_WEBHOOK_HOSTNAME e.g. gha-scheduler.dev.itchallenge.fr}"
: "${GHA_REPOS:?set GHA_REPOS e.g. org/repo1,org/repo2}"

GHA_EXPOSE="${GHA_EXPOSE:-cloudflare-tunnel}"
GHA_GATEWAY_NAME="${GHA_GATEWAY_NAME:-ts-gateway-internal}"
GHA_GATEWAY_NAMESPACE="${GHA_GATEWAY_NAMESPACE:-gateway}"
GHA_GATEWAY_SECTION="${GHA_GATEWAY_SECTION:-https-gha-scheduler}"
GHA_SCHEDULER_IMAGE="${GHA_SCHEDULER_IMAGE:-ghcr.io/muandane/gha-scheduler:102db14}"
GHA_CACHE_IMAGE="${GHA_CACHE_IMAGE:-ghcr.io/muandane/gha-cache-sidecar:102db14}"

log() { printf '==> %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

require_gateway() {
  kubectl -n "${GHA_GATEWAY_NAMESPACE}" get gateway "${GHA_GATEWAY_NAME}" >/dev/null 2>&1 || \
    die "Gateway ${GHA_GATEWAY_NAMESPACE}/${GHA_GATEWAY_NAME} not found"
}

require_cloudflared() {
  kubectl -n gateway get deploy cloudflared >/dev/null 2>&1 || \
    die "cloudflared not found in gateway namespace — deploy infra-gateway-home first"
}

apply_cloudflared_tunnel() {
  log "Applying Cloudflare Tunnel ingress (includes ${GHA_WEBHOOK_HOSTNAME})"
  chmod +x "${ROOT}/scripts/ensure-cloudflare-tunnel-route.sh"
  "${ROOT}/scripts/ensure-cloudflare-tunnel-route.sh"
}

apply_configmap() {
  local tmp
  tmp="$(mktemp)"
  export GHA_REPOS GHA_CACHE_IMAGE
  envsubst '${GHA_REPOS} ${GHA_CACHE_IMAGE}' < "${MANIFESTS}/configmap.yaml" > "${tmp}"
  kubectl apply -f "${tmp}"
  rm -f "${tmp}"
}

apply_deployment() {
  local tmp
  tmp="$(mktemp)"
  export GHA_SCHEDULER_IMAGE
  envsubst '${GHA_SCHEDULER_IMAGE}' < "${MANIFESTS}/deployment.yaml" > "${tmp}"
  kubectl apply -f "${tmp}"
  rm -f "${tmp}"
}

apply_manifest() {
  kubectl apply -f "$1"
}

apply_httproute() {
  local tmp
  tmp="$(mktemp)"
  export GHA_WEBHOOK_HOSTNAME GHA_GATEWAY_NAME GHA_GATEWAY_NAMESPACE GHA_GATEWAY_SECTION
  envsubst '${GHA_WEBHOOK_HOSTNAME} ${GHA_GATEWAY_NAME} ${GHA_GATEWAY_NAMESPACE} ${GHA_GATEWAY_SECTION}' \
    < "${MANIFESTS}/httproute.yaml" > "${tmp}"
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

log "Precheck expose mode: ${GHA_EXPOSE}"
case "${GHA_EXPOSE}" in
  cloudflare-tunnel)
    require_cloudflared
  ;;
  ts-gateway)
    require_gateway
  ;;
  *)
    die "unknown GHA_EXPOSE=${GHA_EXPOSE} (use cloudflare-tunnel or ts-gateway)"
    ;;
esac

log "Applying gha-scheduler namespace"
apply_manifest "${MANIFESTS}/namespace.yaml"

ensure_s3_credentials
install_seaweedfs

log "Applying gha-scheduler manifests"
apply_manifest "${MANIFESTS}/serviceaccount.yaml"
apply_manifest "${MANIFESTS}/rbac.yaml"
apply_configmap

if kubectl -n gha-runners get secret gha-scheduler-secrets >/dev/null 2>&1; then
  log "Using existing secret gha-scheduler-secrets"
else
  die "Create gha-scheduler-secrets (see manifests/secrets.example.yaml)"
fi

apply_manifest "${MANIFESTS}/pvc-job-store.yaml"
apply_deployment
apply_manifest "${MANIFESTS}/service.yaml"

if [[ "${GHA_EXPOSE}" == "ts-gateway" ]]; then
  apply_httproute
else
  kubectl -n gha-runners delete httproute gha-scheduler-webhook --ignore-not-found
  apply_cloudflared_tunnel
fi

log "Waiting for rollout"
kubectl -n gha-runners rollout status deployment/gha-scheduler --timeout=5m

log "Health check"
kubectl -n gha-runners get pods -l app=gha-scheduler
log "Webhook URL: https://${GHA_WEBHOOK_HOSTNAME}/webhook"
log "Next: ./scripts/canary-check.sh && deploy/CANARY.md"
