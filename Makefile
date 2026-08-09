.PHONY: test build web-install web-build image image-scheduler image-sidecar push smoke integration

REGISTRY ?= ghcr.io/muandane
SCHEDULER_IMAGE ?= $(REGISTRY)/gha-scheduler
SIDECAR_IMAGE ?= $(REGISTRY)/gha-cache-sidecar
TAG ?= latest
WEB_DIR := web

test: web-build
	go test ./... -count=1

build: web-build
	go build -o bin/scheduler ./cmd/scheduler
	go build -o bin/cache-sidecar ./cache-sidecar/cmd/sidecar

web-install:
	cd $(WEB_DIR) && pnpm install --frozen-lockfile

web-build:
	cd $(WEB_DIR) && pnpm install --frozen-lockfile && pnpm build
	rm -rf internal/console/static && cp -R $(WEB_DIR)/dist internal/console/static

image-scheduler:
	docker build -f Dockerfile -t $(SCHEDULER_IMAGE):$(TAG) .

image-sidecar:
	docker build -f cache-sidecar/Dockerfile -t $(SIDECAR_IMAGE):$(TAG) .

image: image-scheduler image-sidecar

push:
	docker push $(SCHEDULER_IMAGE):$(TAG)
	docker push $(SIDECAR_IMAGE):$(TAG)

smoke:
	./scripts/smoke.sh

integration:
	./scripts/integration.sh

e2e:
	./scripts/e2e-webhook.sh
