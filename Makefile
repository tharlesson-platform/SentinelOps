SHELL := /bin/sh
ROOT := $(CURDIR)
COMPOSE := docker compose --env-file $(ROOT)/.env -f $(ROOT)/deploy/compose/docker-compose.yml
COMPOSE_HA := $(COMPOSE) -f $(ROOT)/deploy/compose/docker-compose.ha.yml
IMAGE_LOCK := $(ROOT)/artifacts/runtime/docker-compose.images.lock.yml
COMPOSE_RUNTIME := $(COMPOSE_HA) -f $(IMAGE_LOCK)
GO_IMAGE := golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406
GO_TEST_IMAGE := golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36
NODE_IMAGE := node:26.7.0-alpine3.23@sha256:ce3cc39fe3b8b2602d3b1c4d63d301e46b48c550ecb627869853ddcdda418b63

.PHONY: bootstrap prepare-images up local-demo prove-local prove-ha prove-ha-manifests prove-resilience prove-dashboards prove-apm validate-release down reset seed test test-synthetics check logs doctor credentials generate lint build install-server install-collector bootstrap-apm collector-bundle

bootstrap:
	@chmod +x scripts/*.sh
	@./scripts/bootstrap.sh
	@./scripts/generate-dashboards.sh

prepare-images: bootstrap
	@docker image inspect sentinelops-alloy:1.18.1-patched.1 >/dev/null 2>&1 || ./scripts/build-patched-alloy.sh
	$(COMPOSE) build migrate api worker agent web demo-api
	@./scripts/lock-local-images.sh

up: prepare-images
	$(COMPOSE_RUNTIME) up -d --no-build --scale api=2 --scale worker=2
	@echo "SentinelOps: http://localhost:3000 | Demo: http://localhost:8090 | Grafana: http://localhost:3001 | Temporal: http://localhost:8088"

local-demo: up seed prove-local prove-ha

prove-local:
	@./scripts/prove-local-pipeline.sh

prove-ha:
	@./scripts/prove-local-ha.sh

prove-ha-manifests:
	@./scripts/prove-ha-manifests.sh

prove-resilience:
	@./scripts/prove-pyroscope-recovery.sh

prove-dashboards:
	@./scripts/prove-dashboards.sh

prove-apm:
	@./scripts/prove-apm-onboarding.sh

validate-release:
	@./scripts/prove-ha-manifests.sh
	@./scripts/prove-release-controls.sh

down:
	$(COMPOSE) --profile secure-ingest --profile test down --remove-orphans

reset:
	@echo "Removendo somente volumes locais do projeto sentinelops..."
	$(COMPOSE) down --volumes --remove-orphans

seed:
	@./scripts/seed.sh

test:
	docker run --rm -v $(ROOT):/src -w /src $(GO_TEST_IMAGE) sh -ec 'go test -race ./...'
	docker run --rm -v $(ROOT)/apps/web:/app -w /app $(NODE_IMAGE) sh -ec 'npm ci --ignore-scripts --no-audit --no-fund && npm test && npm run build'

test-synthetics:
	@test -f $(IMAGE_LOCK) || { echo "Execute make prepare-images primeiro" >&2; exit 2; }
	$(COMPOSE_RUNTIME) --profile test up --scale api=2 --abort-on-container-exit --exit-code-from playwright playwright
	$(COMPOSE_RUNTIME) --profile test up --abort-on-container-exit --exit-code-from k6 k6
	$(COMPOSE_RUNTIME) --profile test rm -f playwright k6

lint:
	docker run --rm -v $(ROOT):/src -w /src $(GO_IMAGE) sh -ec 'gofmt -w $$(find apps internal demo -name "*.go") && go vet ./...'
	$(COMPOSE) config --quiet
	$(COMPOSE_HA) config --quiet

build:
	$(COMPOSE) build migrate api worker agent web demo-api

check: test lint

logs:
	@test -f $(IMAGE_LOCK) || { echo "Execute make prepare-images primeiro" >&2; exit 2; }
	$(COMPOSE_RUNTIME) logs -f --tail=200

doctor:
	@./scripts/doctor.sh

credentials:
	@chmod 600 .env
	@awk -F= '/^(LOCAL_ADMIN_USER|LOCAL_ADMIN_PASSWORD|GRAFANA_ADMIN_USER|GRAFANA_ADMIN_PASSWORD)=/{print $$1"="$$2}' .env

generate:
	@./scripts/generate-dashboards.sh

install-server:
	@./bootstrap-linux.sh

install-collector:
	@test -n "$(METRICS_ENDPOINT)" || { echo "METRICS_ENDPOINT é obrigatório" >&2; exit 2; }
	@test -n "$(LOGS_ENDPOINT)" || { echo "LOGS_ENDPOINT é obrigatório" >&2; exit 2; }
	@test -n "$(OTLP_ENDPOINT)" || { echo "OTLP_ENDPOINT é obrigatório" >&2; exit 2; }
	@./scripts/install-linux-collector.sh --phase all --metrics-endpoint "$(METRICS_ENDPOINT)" --logs-endpoint "$(LOGS_ENDPOINT)" --otlp-endpoint "$(OTLP_ENDPOINT)"

bootstrap-apm:
	@test -n "$(LANGUAGE)" || { echo "Use: make bootstrap-apm LANGUAGE=go SERVICE=my-api ENVIRONMENT=development" >&2; exit 2; }
	@test -n "$(SERVICE)" || { echo "SERVICE é obrigatório" >&2; exit 2; }
	@./scripts/bootstrap-apm.sh --language "$(LANGUAGE)" --service-name "$(SERVICE)" --environment "$(or $(ENVIRONMENT),development)"

collector-bundle:
	@test -n "$(ORGANIZATION)" || { echo "ORGANIZATION é obrigatório" >&2; exit 2; }
	@test -n "$(COLLECTOR)" || { echo "COLLECTOR é obrigatório" >&2; exit 2; }
	@test -n "$(GATEWAY_URL)" || { echo "GATEWAY_URL é obrigatório" >&2; exit 2; }
	@test -n "$(TLS_SERVER_NAME)" || { echo "TLS_SERVER_NAME é obrigatório" >&2; exit 2; }
	@./scripts/create-linux-collector-bundle.sh --organization "$(ORGANIZATION)" --collector-name "$(COLLECTOR)" --gateway-url "$(GATEWAY_URL)" --tls-server-name "$(TLS_SERVER_NAME)"
