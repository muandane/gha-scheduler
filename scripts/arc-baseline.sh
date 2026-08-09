#!/usr/bin/env bash
# Capture ARC baseline metrics before gha-scheduler canary.
# Fill results into docs/arc-baseline.md template block.
set -euo pipefail

export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"
OUT="${1:-}"

log() { printf '==> %s\n' "$*"; }

section() {
  printf '\n## %s\n' "$*"
}

run_capture() {
  section "ARC control plane idle (kubectl top)"
  if kubectl top pods -n actions-runner-system 2>/dev/null; then
    :
  else
    echo "WARN: actions-runner-system not found or metrics-server unavailable"
  fi
  echo
  kubectl top pods -A -l 'app.kubernetes.io/name=gha-runner-scale-set-listener' 2>/dev/null || \
    echo "WARN: no gha-runner-scale-set-listener pods (label may differ on your ARC install)"

  section "ARC runner pods (sample)"
  kubectl get pods -A -l 'actions.github.com/scale-set-name' 2>/dev/null | head -20 || \
    kubectl get pods -A | grep -i runner | head -20 || true

  section "gha-scheduler targets (PRD)"
  cat <<'EOF'
Webhook → Running: measure p50/p95 from ≥50 queued jobs (webhook time vs pod Running)
Cache warm restore: time 100MB actions/cache restore in workflow logs
Trace completeness: ARC baseline = 0% (no linked job traces)
EOF

  section "Template (copy into docs/arc-baseline.md)"
  cat <<'EOF'
Date:
Cluster: homelab nuc-k3s
ARC version:
Webhook→Running p50: s  p95: s  (n= jobs)
Control plane idle: Mi  CPU m
Cache warm restore (100MB): s
Notes:
EOF
}

if [[ -n "$OUT" ]]; then
  run_capture | tee "$OUT"
else
  run_capture
fi
