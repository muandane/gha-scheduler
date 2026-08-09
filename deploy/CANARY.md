# Canary rollout (no shadow mode)

Rollback = repoint GitHub App webhook to the previous handler URL.

## Preconditions

- [ ] `go test ./...` passes locally
- [ ] `go test -count=1 -race ./scripts/smoke/ -run TestSmokeWebhookCreatesJobAndSecret` passes
- [ ] `scripts/canary-check.sh` passes (cluster deployed)
- [ ] `scripts/integration.sh` passes (real App creds)
- [ ] Secrets applied (see [`docs/gh-app-setup.md`](docs/gh-app-setup.md))
- [ ] Ingress / HTTPRoute reachable from GitHub webhook IPs

## 1. Deploy

```bash
export GHA_REPOS=your-org/your-repo
export GHA_WEBHOOK_HOSTNAME=gha-scheduler.your-domain.com
export GHA_S3_ACCESS_KEY=your-access-key
export GHA_S3_SECRET_KEY=your-secret-key
./deploy/install.sh
```

`install.sh` applies namespace, SeaweedFS, gha-scheduler, and HTTPRoute (requires `ts-gateway`).

Verify:

```bash
./scripts/canary-check.sh
kubectl -n gha-runners rollout status deploy/gha-scheduler
kubectl -n gha-runners get pods -l app=gha-scheduler
```

## 2. Point one low-risk repo

1. Choose a repo with low blast radius (infrequent jobs, easy rollback).
2. Update workflow `runs-on` labels to match gha-scheduler labelquery (see [`docs/gh-app-setup.md`](docs/gh-app-setup.md)).
3. Point GitHub App webhook URL to `https://<GHA_WEBHOOK_HOSTNAME>/webhook` — **single active handler** (remove the previous webhook).

## 3. Measure go/no-go

For ≥ 20 jobs on the canary repo:

| Check | Source | Go threshold |
|-------|--------|--------------|
| Webhook → Running p50 | `gha_scheduler.webhook_to_running_latency` | < 10s |
| Webhook → Running p95 | same | < 20s |
| Dispatch errors | `gha_scheduler.dispatch_errors_total` | 0 sustained |
| JIT / k8s failures | scheduler logs, `dispatch failed` | 0 |
| Orphan GH runners | GH UI / API | 0 |

Grafana queries: [`docs/grafana-canary.md`](docs/grafana-canary.md)

## 4. Expand repo-by-repo

Repeat steps 2–3 per repo.

## 5. Rollback

1. GitHub App → Webhook URL back to the previous handler.
2. Optionally scale `gha-scheduler` to 0:
   `kubectl -n gha-runners scale deploy/gha-scheduler --replicas=0`
3. Existing runner Jobs in `gha-runners` namespace complete or delete per ops policy.

## 6. Full cutover

When all repos migrated and legacy controllers idle:

- Decommission prior runner controllers/listeners per platform runbook.
