# Canary rollout (no shadow mode)

Rollback = repoint GitHub App webhook delivery to ARC. No `GHA_SHADOW_MODE` path.

## Preconditions

- [ ] Phase 1 observability green (`go test ./...`)
- [ ] ARC baseline recorded in [`docs/arc-baseline.md`](docs/arc-baseline.md) (`./scripts/arc-baseline.sh`)
- [ ] `scripts/smoke.sh` passes
- [ ] `scripts/canary-check.sh` passes (cluster deployed)
- [ ] `scripts/integration.sh` passes (real App creds)
- [ ] Secrets applied (see [`docs/gh-app-setup.md`](docs/gh-app-setup.md))
- [ ] Ingress / HTTPRoute reachable from GitHub webhook IPs

## 1. Deploy

```bash
# Optional: bootstrap S3 creds for SeaweedFS + cache sidecar
export GHA_S3_ACCESS_KEY=your-access-key
export GHA_S3_SECRET_KEY=your-secret-key

export GHA_WEBHOOK_HOSTNAME=gha-scheduler.your-domain.com
./deploy/install.sh
```

`install.sh` applies namespace, SeaweedFS, gha-scheduler, and HTTPRoute (requires `ts-gateway`).

Manual apply (equivalent):

```bash
kubectl apply -f deploy/manifests/namespace.yaml
# create gha-cache-s3-credentials + render seaweedfs-s3-config from seaweedfs-s3-config.example.yaml
kubectl apply -f deploy/manifests/seaweedfs.yaml
kubectl apply -f deploy/manifests/serviceaccount.yaml
kubectl apply -f deploy/manifests/rbac.yaml
kubectl apply -f deploy/manifests/configmap.yaml
kubectl apply -f deploy/manifests/secrets.example.yaml  # or real secret
kubectl apply -f deploy/manifests/deployment.yaml
kubectl apply -f deploy/manifests/service.yaml
# envsubst GHA_WEBHOOK_HOSTNAME < deploy/manifests/ingress.yaml | kubectl apply -f -
```

Verify:

```bash
./scripts/canary-check.sh
kubectl -n gha-runners rollout status deploy/gha-scheduler
kubectl -n gha-runners get pods -l app=gha-scheduler
```

## 2. Point one low-risk repo

1. Choose a repo with low blast radius (infrequent jobs, easy rollback).
2. Update workflow `runs-on` labels to match gha-scheduler labelquery (see [`docs/gh-app-setup.md`](docs/gh-app-setup.md)).
3. Point GitHub App webhook URL to `https://<GHA_WEBHOOK_HOSTNAME>/webhook` — **single active handler** (remove ARC webhook).

## 3. Measure go/no-go

For ≥ 20 jobs on the canary repo:

| Check | Source | Go threshold |
|-------|--------|--------------|
| Webhook → Running p50 | `gha_scheduler.schedule_latency` + traces | < 10s |
| Webhook → Running p95 | same | < 20s |
| Dispatch errors | `gha_scheduler.dispatch_errors_total` | 0 sustained |
| JIT / k8s failures | scheduler logs, `dispatch failed` | 0 |
| Orphan GH runners | GH UI / API | 0 |

Grafana queries: [`docs/grafana-canary.md`](docs/grafana-canary.md)

## 4. Expand repo-by-repo

Repeat steps 2–3 per repo. Decommission ARC per repo once validated.

## 5. Rollback

1. GitHub App → Webhook URL back to ARC listener/ingress.
2. Optionally scale `gha-scheduler` to 0:
   `kubectl -n gha-runners scale deploy/gha-scheduler --replicas=0`
3. Existing runner Jobs in `gha-runners` namespace complete or delete per ops policy.

## 6. Full cutover

When all repos migrated and ARC idle:

- Decommission ARC controllers/listeners per platform runbook.
- ARC baseline comparison: [`docs/arc-baseline.md`](docs/arc-baseline.md)
