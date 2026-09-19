.PHONY: tidy test unit migrate up down run-api run-worker smoke upgrade

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

up:
	docker compose up -d --build

down:
	docker compose down -v

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

smoke:
	@./scripts/smoke.sh
