# gha-scheduler

Ephemeral GitHub Actions runner scheduler for Kubernetes. One `workflow_job:queued` webhook creates one runner Job; the pod exits when the job finishes.

ARC replacement: no CRDs, no listener pods, no scale-set controller.

## How it works

1. GitHub sends `workflow_job` (`action: queued`) to `POST /webhook`.
2. The scheduler verifies HMAC, parses `runs-on` labels, and calls `generate-jit-config`.
3. It creates a JIT `Secret` and a `batch/v1` Job (runner + optional cache sidecar).
4. A 60s reconciler picks up missed webhooks. Leader election runs the reconciler on one replica only.
5. A pod informer emits OpenTelemetry spans and metrics.
6. Optional: SQLite job store + embedded console UI at `/` (see [docs/console.md](docs/console.md)).

## Workflow labels

```yaml
runs-on:
  - runs-on=${{ github.run_id }}
  - cpu=2
  - arch=x64
  - pool=spot
  - cache=s3
```

See [docs/gh-app-setup.md](docs/gh-app-setup.md) for GitHub App permissions and cutover steps.

## Images

Published by GitHub Actions to:

- `ghcr.io/muandane/gha-scheduler`
- `ghcr.io/muandane/gha-cache-sidecar`

## Local development

```bash
make test    # runs web-build then go test
make build
go test -count=1 -race ./scripts/smoke/ -run TestSmokeWebhookCreatesJobAndSecret -v
```

With App credentials:

```bash
export GHA_APP_ID=... GHA_INSTALLATION_ID=... GHA_APP_PRIVATE_KEY_FILE=./app.pem
./scripts/integration.sh
```

## Deploy (homelab)

```bash
export GHA_WEBHOOK_HOSTNAME=gha-scheduler.example.com
export GHA_S3_ACCESS_KEY=... GHA_S3_SECRET_KEY=...
./deploy/install.sh
./scripts/canary-check.sh
```

Details: [deploy/CANARY.md](deploy/CANARY.md), [docs/console.md](docs/console.md), [docs/grafana-canary.md](docs/grafana-canary.md).

## Docs

| Doc | Purpose |
|-----|---------|
| [docs/gh-app-setup.md](docs/gh-app-setup.md) | GitHub App, secrets, cutover |
| [docs/ci.md](docs/ci.md) | CI/CD and image publishing |
| [docs/console.md](docs/console.md) | Built-in job console (SQLite + embedded UI) |
| [deploy/CANARY.md](deploy/CANARY.md) | Rollout checklist |

## License

MIT. See [LICENSE](LICENSE).
