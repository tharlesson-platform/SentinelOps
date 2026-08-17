SHELL := /bin/sh
ROOT := $(CURDIR)
COMPOSE := docker compose --env-file $(ROOT)/.env -f $(ROOT)/deploy/compose/docker-compose.yml
GO_IMAGE := golang:1.26.6-alpine3.23
NODE_IMAGE := node:26.7.0-alpine3.23

.PHONY: bootstrap up down reset seed test test-synthetics check logs doctor credentials generate lint build

bootstrap:
	@chmod +x scripts/*.sh
	@./scripts/bootstrap.sh
	@./scripts/generate-dashboards.sh

up: bootstrap
	$(COMPOSE) up -d --build
	@echo "SentinelOps: http://localhost:3000 | Grafana: http://localhost:3001 | Temporal: http://localhost:8088"

down:
	$(COMPOSE) down --remove-orphans

reset:
	@echo "Removendo somente volumes locais do projeto sentinelops..."
	$(COMPOSE) down --volumes --remove-orphans

seed:
	@./scripts/seed.sh

test:
	docker run --rm -v $(ROOT):/src -w /src $(GO_IMAGE) sh -ec 'go test -race ./...'
	docker run --rm -v $(ROOT)/apps/web:/app -w /app $(NODE_IMAGE) sh -ec 'npm install --ignore-scripts --no-audit --no-fund && npm test && npm run build'

test-synthetics:
	$(COMPOSE) up --build --abort-on-container-exit --exit-code-from playwright playwright
	$(COMPOSE) up --abort-on-container-exit --exit-code-from k6 k6

lint:
	docker run --rm -v $(ROOT):/src -w /src $(GO_IMAGE) sh -ec 'gofmt -w $$(find apps internal demo -name "*.go") && go vet ./...'
	$(COMPOSE) config --quiet

build:
	$(COMPOSE) build api worker agent web demo-api

check: test lint

logs:
	$(COMPOSE) logs -f --tail=200

doctor:
	@./scripts/doctor.sh

credentials:
	@chmod 600 .env
	@awk -F= '/^(LOCAL_ADMIN_USER|LOCAL_ADMIN_PASSWORD|GRAFANA_ADMIN_USER|GRAFANA_ADMIN_PASSWORD)=/{print $$1"="$$2}' .env

generate:
	@./scripts/generate-dashboards.sh
