#!/usr/bin/env bash
# E2E: POST signed workflow_job webhook to live scheduler, assert Job + pod Running.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export KUBECONFIG="${KUBECONFIG:-${HOME}/.kube/nuc-k3s.yaml}"

: "${GHA_WEBHOOK_SECRET:?set GHA_WEBHOOK_SECRET}"
: "${GHA_WEBHOOK_URL:?set GHA_WEBHOOK_URL e.g. https://gha-scheduler.example.com/webhook}"
: "${GHA_TEST_OWNER:?set GHA_TEST_OWNER}"
: "${GHA_TEST_REPO:?set GHA_TEST_REPO}"

JOB_ID="${GHA_TEST_JOB_ID:-$(date +%s)}"
RUN_ID="${GHA_TEST_RUN_ID:-$JOB_ID}"
TIMEOUT="${GHA_E2E_TIMEOUT:-120}"

log() { printf '==> %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

body=$(cat <<EOF
{
  "action": "queued",
  "workflow_job": {
    "id": ${JOB_ID},
    "run_id": ${RUN_ID},
    "labels": ["runs-on=${RUN_ID}", "cpu=2", "arch=x64", "pool=spot"],
    "runner_name": ""
  },
  "repository": {
    "full_name": "${GHA_TEST_OWNER}/${GHA_TEST_REPO}",
    "owner": {"login": "${GHA_TEST_OWNER}"},
    "name": "${GHA_TEST_REPO}"
  }
}
EOF
)

sig=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$GHA_WEBHOOK_SECRET" | awk '{print "sha256="$2}')

log "POST webhook job_id=${JOB_ID}"
http_code=$(curl -sS -o /tmp/gha-e2e-resp.txt -w '%{http_code}' \
  -X POST "$GHA_WEBHOOK_URL" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: workflow_job" \
  -H "X-Hub-Signature-256: ${sig}" \
  -d "$body")
[[ "$http_code" == "200" ]] || fail "webhook status ${http_code}: $(cat /tmp/gha-e2e-resp.txt)"

expected_job="ghs-job-${RUN_ID}-${JOB_ID}"
deadline=$((SECONDS + TIMEOUT))

log "Waiting for Job ${expected_job}"
while (( SECONDS < deadline )); do
  if kubectl -n gha-runners get job "$expected_job" >/dev/null 2>&1; then
  log "Job created"
    break
  fi
  sleep 2
done
kubectl -n gha-runners get job "$expected_job" >/dev/null 2>&1 || fail "Job not created within ${TIMEOUT}s"

log "Waiting for pod Running"
while (( SECONDS < deadline )); do
  phase=$(kubectl -n gha-runners get pods -l "gha-scheduler.gh_job_id=${JOB_ID}" -o jsonpath='{.items[0].status.phase}' 2>/dev/null || true)
  if [[ "$phase" == "Running" ]]; then
    log "Pod Running — e2e passed"
    kubectl -n gha-runners get pods -l "gha-scheduler.gh_job_id=${JOB_ID}"
    exit 0
  fi
  sleep 3
done

kubectl -n gha-runners get pods -l "gha-scheduler.gh_job_id=${JOB_ID}" || true
fail "pod not Running within ${TIMEOUT}s"
