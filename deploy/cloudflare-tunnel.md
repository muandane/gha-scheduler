# Expose gha-scheduler via Cloudflare Tunnel (same pattern as sanad-admin.itchallenge.fr)

Homelab `cloudflared` in namespace `gateway` uses **GitOps ingress** (`platforms-flux/infrastructure/gateway/home/cloudflared/configmap.yaml`). The route for `gha-scheduler.dev.itchallenge.fr` is defined there — no manual Zero Trust dashboard step.

| Field | Value |
|-------|--------|
| **Public hostname** | `gha-scheduler.dev.itchallenge.fr` |
| **Service** | `http://gha-scheduler.gha-runners.svc.cluster.local:8080` |

- GitHub webhook: `https://gha-scheduler.dev.itchallenge.fr/webhook`
- Health: `https://gha-scheduler.dev.itchallenge.fr/healthz`

After changing ingress, reconcile gateway:

```bash
flux reconcile kustomization infra-gateway-home --with-source
```

## 1. DNS (`itchallenge.fr` zone)

CNAME `gha-scheduler.dev` → tunnel (proxied), or use **Route DNS** in Zero Trust.

```bash
dig +short gha-scheduler.dev.itchallenge.fr @1.1.1.1
curl -sf https://gha-scheduler.dev.itchallenge.fr/healthz
```

## 3. Install gha-scheduler

```bash
export KUBECONFIG=~/.kube/nuc-k3s.yaml
export GHA_EXPOSE=cloudflare-tunnel
export GHA_WEBHOOK_HOSTNAME=gha-scheduler.dev.itchallenge.fr
export GHA_REPOS="Simplifi-ED/sanad"
export GHA_SCHEDULER_IMAGE=ghcr.io/muandane/gha-scheduler:102db14
export GHA_CACHE_IMAGE=ghcr.io/muandane/gha-cache-sidecar:102db14

./deploy/install.sh
```

`GHA_EXPOSE=cloudflare-tunnel` does **not** create an HTTPRoute — traffic is public-only via Cloudflare Tunnel to the in-cluster Service.

## 4. GitHub App

| Field | Value |
|-------|--------|
| Webhook URL | `https://gha-scheduler.dev.itchallenge.fr/webhook` |
| Webhook secret | same as `gha-scheduler-secrets` / `webhook-secret` |

Org-wide App install is fine; set `GHA_REPOS` for reconciler backfill.
