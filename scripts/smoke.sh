#!/usr/bin/env bash
# Offline smoke: signed webhook → fake k8s clientset → Job + Secret created.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "==> gha-scheduler smoke (offline fake clientset)"
go test -count=1 -timeout=30s ./scripts/smoke/ -run TestSmokeWebhookCreatesJobAndSecret -v
