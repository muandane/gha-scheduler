#!/usr/bin/env bash
# Live integration dry-run: GH App token → generate-jit-config (no runner registration).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

: "${GHA_APP_ID:?set GHA_APP_ID}"
: "${GHA_INSTALLATION_ID:?set GHA_INSTALLATION_ID}"
if [[ -z "${GHA_APP_PRIVATE_KEY:-}" && -z "${GHA_APP_PRIVATE_KEY_FILE:-}" ]]; then
  echo "set GHA_APP_PRIVATE_KEY or GHA_APP_PRIVATE_KEY_FILE" >&2
  exit 1
fi

echo "==> gha-scheduler integration (generate-jit-config dry run)"
go run ./scripts/integration/
