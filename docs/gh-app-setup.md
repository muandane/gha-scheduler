# GitHub App setup for gha-scheduler

Direct ARC replacement — single webhook handler, no shadow mode.

## Create the App

GitHub → Settings → Developer settings → GitHub Apps → New GitHub App

| Field | Value |
|-------|--------|
| Webhook URL | `https://<GHA_WEBHOOK_HOSTNAME>/webhook` |
| Webhook secret | Random string → `GHA_WEBHOOK_SECRET` |
| Active | Yes |

### Permissions (repository)

| Permission | Access |
|------------|--------|
| Actions | Read and write |
| Metadata | Read-only |
| Administration | Read-only (org runner scope) |

### Subscribe to events

- `Workflow job` only

## Install the App

Install on org or selected repos. Note **Installation ID** → `GHA_INSTALLATION_ID`.

## Credentials (cluster secret)

```bash
kubectl -n gha-runners create secret generic gha-scheduler-secrets \
  --from-literal=webhook-secret='...' \
  --from-literal=app-id='123456' \
  --from-literal=installation-id='789' \
  --from-file=app-private-key=./gha-app.pem
```

Env mapping:

| Secret key | Env var |
|------------|---------|
| `webhook-secret` | `GHA_WEBHOOK_SECRET` |
| `app-id` | `GHA_APP_ID` |
| `installation-id` | `GHA_INSTALLATION_ID` |
| `app-private-key` | `GHA_APP_PRIVATE_KEY` |

## Reconciler repos

Comma-separated list in ConfigMap `GHA_REPOS`:

```yaml
GHA_REPOS: "myorg/repo-a,myorg/repo-b"
```

Required for missed-webhook backfill (60s poll).

## Workflow labels

```yaml
jobs:
  build:
    runs-on:
      - runs-on=${{ github.run_id }}
      - cpu=2
      - arch=x64
      - pool=spot        # optional: node pool
      - cache=s3         # optional: SeaweedFS cache sidecar
```

## Cutover from ARC

1. Deploy gha-scheduler (`./deploy/install.sh`)
2. Run `./scripts/canary-check.sh`
3. Update **one** repo workflow labels
4. Point App webhook URL to gha-scheduler (remove ARC webhook)
5. Validate ≥20 jobs (see `deploy/CANARY.md`, `docs/grafana-canary.md`)
6. Migrate remaining repos; decommission ARC

## Verify

```bash
./scripts/smoke.sh
./scripts/integration.sh          # JIT API dry-run
./scripts/e2e-webhook.sh          # live cluster (needs GHA_WEBHOOK_URL)
```
