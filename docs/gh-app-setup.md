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
| Administration | **Read and write** |

### Permissions (organization) — required for org-wide App install

| Permission | Access |
|------------|--------|
| Self-hosted runners | **Read and write** |

After changing permissions, open the App install on **Simplifi-ED** → **Review request** / accept updated permissions.

`generate-jitconfig` returns **403** if Administration is read-only or the install has not accepted new permissions.

### Subscribe to events

- `Workflow job` only

## Expose webhook (homelab)

**Default: Cloudflare Tunnel** — same pattern as `sanad-admin.itchallenge.fr`. See [`deploy/cloudflare-tunnel.md`](../deploy/cloudflare-tunnel.md).

```bash
export GHA_EXPOSE=cloudflare-tunnel
export GHA_WEBHOOK_HOSTNAME=gha-scheduler.itchallenge.fr
```

Zero Trust → Tunnels → Public hostname:

`gha-scheduler.itchallenge.fr` → `http://gha-scheduler.gha-runners.svc.cluster.local:8080`

No HTTPRoute, no Tailscale — GitHub hits Cloudflare edge → `cloudflared` → Service.

Optional: `GHA_EXPOSE=ts-gateway` applies `deploy/manifests/httproute.yaml` for Tailscale-only Envoy routes.

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

Required for missed-webhook backfill (60s poll). Set at install time:

```bash
export GHA_REPOS="myorg/repo-a,myorg/repo-b"
./deploy/install.sh
```

`install.sh` renders `GHA_REPOS` into the ConfigMap via `envsubst` (not committed to git).

## Health check route

GitHub does not call `/healthz`; it is for canary scripts and gateway probes. The HTTPRoute exposes both `/webhook` and `/healthz` on `GHA_WEBHOOK_HOSTNAME`.

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
