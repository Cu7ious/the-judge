.PHONY: tidy test unit migrate infra up down docker local-api local-worker restart-apps smoke upgrade help

help:
	@echo "Local-friendly workflow (recommended while learning):"
	@echo "  make infra          # start Postgres + Redis only"
	@echo "  make local-api      # go run API (loads .env)"
	@echo "  make local-worker   # go run worker (loads .env)"
	@echo ""
	@echo "All-in-Docker:"
	@echo "  make docker         # build + start api/worker/postgres/redis"
	@echo "  make restart-apps   # recreate api/worker after .env changes"
	@echo "  make down           # stop everything (wipes DB volume)"

tidy:
	go mod tidy

unit:
	go test ./internal/domain/... ./internal/validate/... ./internal/provider/...

test:
	go test ./...

upgrade:
	go get -u ./...
	go mod tidy
	go test ./...

migrate:
	@echo "Migrations run automatically on API startup"

# --- Local DX: infra in Docker, Go on the host ---

infra:
	docker compose up -d postgres redis

local-api: infra
	@set -a && . ./.env && set +a && go run ./cmd/api

local-worker: infra
	@set -a && . ./.env && set +a && go run ./cmd/worker

# --- Full Docker stack ---

docker:
	docker compose up -d --build

up: docker

restart-apps:
	docker compose up -d --force-recreate api worker

down:
	docker compose down -v

# Back-compat aliases
run-api: local-api
run-worker: local-worker

smoke:
	@./scripts/smoke.sh
