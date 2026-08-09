# gha-scheduler console

**The built-in console replaces Grafana for day-to-day scheduler operations** — recent jobs, per-job timelines, dispatch/schedule latency, error counts. **OTel export supplements it for deep-dive debugging** — cross-service traces, long-range trends, and homelab alerting via Victoria/Grafana.

## Build

```bash
make web-build   # pnpm build + copy dist → internal/console/static
make build
```

Requires [pnpm](https://pnpm.io/) 9.x (`web/package.json` `packageManager` field).

## Local dev

```bash
# terminal 1
export GHA_JOB_STORE_ENABLED=true GHA_CONSOLE_ENABLED=true GHA_JOB_STORE_PATH=/tmp/jobs.db
# ... plus required GHA_* secrets for full scheduler
go run ./cmd/scheduler

# terminal 2
cd web && pnpm dev   # proxies /api → :8080
```

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/jobs` | List jobs (`repo`, `status`, `limit`, `cursor`) |
| `GET /api/v1/jobs/{id}` | Job detail + timeline |
| `GET /api/v1/stats?since=24h` | Operational aggregates (p50/p95, errors, active) |

Optional `GHA_CONSOLE_TOKEN`: Bearer auth on `/api/v1/*` and SPA routes. `/webhook` is never protected.

## Config

| Env | Default | Description |
|-----|---------|-------------|
| `GHA_JOB_STORE_ENABLED` | `false` | Persist job lifecycle to SQLite |
| `GHA_JOB_STORE_PATH` | `/data/jobs.db` | SQLite file path |
| `GHA_JOB_STORE_RETENTION_DAYS` | `30` | Prune completed jobs older than N days (`0` = disable) |
| `GHA_JOB_STORE_PRUNE_INTERVAL` | `6h` | Leader-only prune interval |
| `GHA_CONSOLE_ENABLED` | `false` | Serve embedded UI + API (requires job store) |
| `GHA_CONSOLE_TOKEN` | — | Optional Bearer token |

## Deploy

- SQLite on RWO PVC: [`deploy/manifests/pvc-job-store.yaml`](manifests/pvc-job-store.yaml)
- Deployment: **`replicas: 1`**, **`strategy: Recreate`** (single SQLite writer)
- Webhook: [`deploy/manifests/ingress.yaml`](manifests/ingress.yaml) — `/webhook` only
- Console: [`deploy/manifests/httproute-console.yaml`](manifests/httproute-console.yaml) — internal Tailscale gateway

## Stats implementation

Percentiles use **app-side sort** on a bounded query (max 10k rows, `since` window) via `modernc.org/sqlite`. See `internal/store/sqlite/stats_bench_test.go`.

## Canary validation

1. Trigger a workflow job on a canary repo.
2. Open console → job appears with timeline phases.
3. `curl /api/v1/stats?since=24h` returns non-zero `completed_jobs` after job finishes.
4. Confirm OTel metrics still export to homelab collector (supplemental path).
