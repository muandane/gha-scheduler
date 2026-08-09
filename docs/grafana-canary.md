# Grafana: gha-scheduler canary metrics

OTel metrics export when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (see `deploy/manifests/configmap.yaml`).

## Verify OTLP path

1. Deploy scheduler with OTel endpoint pointing at homelab collector or Victoria push route.
2. Grafana → Explore → VictoriaMetrics datasource.
3. Search metrics: `gha_scheduler_*`

If empty, check scheduler logs for telemetry errors and confirm OTLP receiver on observability stack accepts HTTP on port 4318.

## PromQL (canary go/no-go)

Webhook → running p95 (primary SLO):

```promql
histogram_quantile(0.95, sum(rate(gha_scheduler_webhook_to_running_latency_bucket[5m])) by (le))
```

Webhook → running p50:

```promql
histogram_quantile(0.50, sum(rate(gha_scheduler_webhook_to_running_latency_bucket[5m])) by (le))
```

Schedule latency p95 (Job created → pod running):

```promql
histogram_quantile(0.95, sum(rate(gha_scheduler_schedule_latency_bucket[5m])) by (le))
```

Dispatch errors (should be 0):

```promql
sum(rate(gha_scheduler_dispatch_errors_total[5m])) by (reason)
```

Cache sidecar probe failures:

```promql
sum(rate(gha_scheduler_cache_sidecar_failures_total[5m]))
```

## Traces

Grafana → Explore → Tempo/Victoria Traces:

- Service: `gha-scheduler`
- Span names: `job.webhook_received`, `job.dispatch`, `job.pod_scheduled`, `job.pod_running`, `job.pod_completed`
- One `TraceID` per `job_id` (JobTraceRegistry)

## Dashboard

Import [`deploy/grafana/gha-scheduler-canary.json`](grafana/gha-scheduler-canary.json) or add to homelab Grafana provisioning.
