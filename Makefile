SHELL := /bin/bash
GO ?= go
NPM ?= npm
COMPOSE := docker compose
PRODUCTION := $(COMPOSE) --env-file .env.production -f deploy/compose.yaml

.PHONY: help init setup up down logs db migrate migrate-host create-user create-user-host dev-api dev-web fmt lint test test-integration build check smoke e2e deploy production-logs

help:
	@printf '%s\n' \
	  'make init       Create local .env without overwriting it' \
	  'make up         Build and start the full local stack' \
	  'make down       Stop the stack; preserve data' \
	  'make setup      Install Go/npm dependencies for host development' \
	  'make db         Start only PostgreSQL' \
	  'make migrate    Apply migrations using the built API image' \
	  'make migrate-host Apply migrations with Go on the host' \
	  'make create-user Create a specialist; prompt for credentials' \
	  'make create-user-host Create a specialist with Go on the host' \
	  'make dev-api    Run Go on the host using .env' \
	  'make dev-web    Run Vite with hot reload' \
	  'make check      Format checks, lint, tests, and builds' \
	  'make smoke      Check a running stack (requires curl)' \
	  'make e2e        Browser smoke checks (requires Playwright Chromium)' \
	  'make deploy     Pull and run production images using .env.production'

init:
	@test -f .env || cp .env.example .env

setup: init
	cd backend && $(GO) mod download
	cd frontend && $(NPM) ci

up: init
	$(COMPOSE) build
	$(COMPOSE) up -d --wait --wait-timeout 120 db
	$(MAKE) migrate
	$(COMPOSE) up -d --wait --wait-timeout 120

migrate: init
	$(COMPOSE) run --rm --no-deps --entrypoint manage api migrate

migrate-host: init
	set -a; source .env; set +a; cd backend && $(GO) run ./cmd/manage migrate

create-user:
	bash scripts/create-user.sh

create-user-host: init
	GO=$(GO) bash scripts/create-user.sh host

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100

db: init
	$(COMPOSE) up -d --wait db

dev-api: init
	set -a; source .env; set +a; cd backend && $(GO) run ./cmd/api

dev-web:
	cd frontend && $(NPM) run dev

fmt:
	cd backend && $(GO) fmt ./...
	cd frontend && $(NPM) run format

lint:
	@test -z "$$(cd backend && gofmt -l .)" || { echo 'Run make fmt'; exit 1; }
	cd backend && $(GO) vet ./...
	cd frontend && $(NPM) run format:check && $(NPM) run lint && $(NPM) run typecheck

test:
	cd backend && $(GO) test -race -count=1 ./...

test-integration:
	cd backend && $(GO) test -race -count=1 -tags=integration ./internal/auth

build:
	cd backend && $(GO) build -trimpath -o bin/api ./cmd/api
	cd backend && $(GO) build -trimpath -o bin/manage ./cmd/manage
	cd frontend && $(NPM) run build

check: lint test build

smoke:
	bash scripts/smoke.sh

e2e:
	cd frontend && $(NPM) run test:e2e

deploy:
	bash scripts/deploy.sh

production-logs:
	$(PRODUCTION) logs -f --tail=100
