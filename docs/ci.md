# CI/CD

GitHub Actions workflow: [.github/workflows/ci.yaml](../.github/workflows/ci.yaml)

## Triggers

| Event | Jobs |
|-------|------|
| Pull request | `test`, `lint`, `docker-build` (no push) |
| Push to `main` | `test`, `lint`, `publish` (push images) |
| `workflow_dispatch` | Same as push to `main` |

The test job runs `go test ./... -race`. The lint job runs `golangci-lint` and `govulncheck`.

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

Update [deploy/manifests/configmap.yaml](../deploy/manifests/configmap.yaml) and [deploy/manifests/deployment.yaml](../deploy/manifests/deployment.yaml) when you want to pin a digest or tag other than `latest`:

```bash
# After publish on main, resolve digest from GHCR:
docker buildx imagetools inspect ghcr.io/muandane/gha-scheduler:latest --format '{{json .Manifest.Digest}}'
# Set deployment image to ghcr.io/muandane/gha-scheduler@sha256:...
```

Optional: pass `GHA_SCHEDULER_IMAGE` to `deploy/install.sh` if you add image override support in your cluster workflow.

## Local mirror of CI

```bash
make test
go test -count=1 -race ./scripts/smoke/ -run TestSmokeWebhookCreatesJobAndSecret -v
make image REGISTRY=ghcr.io/muandane TAG=dev
```

## Required secrets

None beyond the default `GITHUB_TOKEN` (workflow has `packages: write`).
