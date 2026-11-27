SHELL := /bin/bash
GO ?= go
NPM ?= npm
COMPOSE := docker compose
PRODUCTION := $(COMPOSE) --env-file .env.production -f deploy/compose.yaml

.PHONY: help init setup up down logs db dev-api dev-web fmt lint test build check smoke e2e deploy production-logs

help:
	@printf '%s\n' \
	  'make init       Create local .env without overwriting it' \
	  'make up         Build and start the full local stack' \
	  'make down       Stop the stack; preserve data' \
	  'make setup      Install Go/npm dependencies for host development' \
	  'make db         Start only PostgreSQL' \
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
	$(COMPOSE) up -d --build --wait --wait-timeout 120

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

build:
	cd backend && $(GO) build -trimpath -o bin/api ./cmd/api
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
