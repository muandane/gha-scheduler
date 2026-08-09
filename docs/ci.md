# CI/CD

GitHub Actions workflow: [.github/workflows/ci.yaml](../.github/workflows/ci.yaml)

## Triggers

| Event | Jobs |
|-------|------|
| Pull request | `test`, `docker-build` (no push) |
| Push to `main` | `test`, `publish` (push images) |
| `workflow_dispatch` | Same as push to `main` |

## Images

| Image | Dockerfile |
|-------|------------|
| `ghcr.io/muandane/gha-scheduler` | `Dockerfile` |
| `ghcr.io/muandane/gha-cache-sidecar` | `cache-sidecar/Dockerfile` |

Tags on `main`: `latest`, `sha-<commit>`.

## Package visibility

After the first publish, set each GHCR package to **public** (or grant your cluster `imagePullSecrets`) under:

`https://github.com/muandane?tab=packages`

## Cluster image pins

Update [deploy/manifests/configmap.yaml](../deploy/manifests/configmap.yaml) and [deploy/manifests/deployment.yaml](../deploy/manifests/deployment.yaml) when you want to pin a digest or tag other than `latest`.

## Local mirror of CI

```bash
make test
./scripts/smoke.sh
make image REGISTRY=ghcr.io/muandane TAG=dev
```

## Required secrets

None beyond the default `GITHUB_TOKEN` (workflow has `packages: write`).
