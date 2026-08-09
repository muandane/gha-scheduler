# ARC baseline metrics (pre-canary)

Capture **before** pointing any repo at gha-scheduler. PRD targets need real ARC numbers, not estimates.

Run `./scripts/arc-baseline.sh` to snapshot control-plane idle metrics into the terminal; copy the template block below when baseline week completes.

## Metrics (PRD success table)

| Metric | ARC baseline (record here) | gha-scheduler target |
|--------|---------------------------|----------------------|
| Webhook → pod Running | _p50 / p95_ | < 10s p50, < 20s p95 |
| Control plane memory/CPU idle | _controllers + listeners_ | 1 binary, < 100Mi idle |
| Cache hit latency (warm) | _GH blob / cross-network_ | in-cluster SeaweedFS, < 1s typical restore |
| Cost per job (compute) | _current spend_ | flat or lower |
| Job trace completeness | _none_ | full span per job |

## How to measure ARC baseline

### Webhook → pod Running

1. Pick a representative repo/workflow on ARC (same node pools you will use for gha-scheduler).
2. **Traces/logs:** timestamp `workflow_job` webhook delivery vs runner pod `Running` (ARC ephemeral runner pod or EphemeralRunner CR phase).
3. Run ≥ 50 queued jobs; record p50/p95.
4. **Grafana / Tempo:** if ARC emits nothing useful, use Loki on `gha-runner-scale-set-controller` + runner namespace pod events.

### Control plane idle

```bash
kubectl top pods -n actions-runner-system
kubectl top pods -A -l app.kubernetes.io/name=gha-runner-scale-set-listener
```

Record sum memory/CPU across controllers + listeners at idle (no queued jobs).

### Cache latency (warm)

1. Workflow with `actions/cache` restore of a known artifact size (e.g. 100MB).
2. Time from cache Twirp/REST call start to restore complete in job log.
3. Compare cross-network GH cache vs future SeaweedFS sidecar (gha-scheduler Phase 4).

### Job trace completeness

ARC: note that no linked trace exists today — baseline = **0%** automated lifecycle visibility.

## gha-scheduler comparison (post-canary)

Use Phase 1 OTel metrics:

| Histogram / counter | Meaning |
|---------------------|---------|
| `gha_scheduler.dispatch_latency` | webhook received → Job created |
| `gha_scheduler.schedule_latency` | Job created → pod running |
| `gha_scheduler.job_duration` | pod running → completed |
| `gha_scheduler.dispatch_errors_total` | label=reason |
| `gha_scheduler.cache_sidecar_failures_total` | sidecar probe failures |

Query in Grafana (example PromQL if exported via OTLP → Prometheus):

```promql
histogram_quantile(0.95, sum(rate(gha_scheduler_schedule_latency_bucket[5m])) by (le))
```

Linked traces: one `TraceID` per `job_id` via `JobTraceRegistry` spans (`job.webhook_received`, `job.dispatch`, pod lifecycle).

## Record baseline

Fill this block when ARC baseline week completes:

```
Date:
Cluster:
ARC version:
Webhook→Running p50: s  p95: s  (n= jobs)
Control plane idle: Mi  CPU m
Cache warm restore (100MB): s
Notes:
```
