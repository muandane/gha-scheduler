.PHONY: test build image image-scheduler image-sidecar push smoke integration

REGISTRY ?= ghcr.io/muandane
SCHEDULER_IMAGE ?= $(REGISTRY)/gha-scheduler
SIDECAR_IMAGE ?= $(REGISTRY)/gha-cache-sidecar
TAG ?= latest

test:
	go test ./... -count=1

build:
	go build -o bin/scheduler ./cmd/scheduler
	go build -o bin/cache-sidecar ./cache-sidecar/cmd/sidecar

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
