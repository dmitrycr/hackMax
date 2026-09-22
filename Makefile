.PHONY: setup up down logs check test test-go test-python build-web probe generate-contracts sqlc

setup:
	python3 scripts/init_env.py

up: setup
	docker compose up --build -d --wait

down:
	docker compose down

logs:
	docker compose logs -f --tail=100 api worker document-processor

check: test build-web
	docker compose --env-file .env.example config --quiet

test: test-go test-python

test-go:
	cd services/backend && go test ./... && go vet ./...

test-python:
	cd services/document-processor && uv sync --frozen && uv run ruff check . && uv run pytest

build-web:
	cd apps/web && npm ci && npm run build

probe:
	docker compose exec worker /app/probe

generate-contracts:
	cd services/document-processor && uv run python ../../scripts/export_contracts.py

sqlc:
	cd services/backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate

