COMPOSE ?= $(shell docker compose version >/dev/null 2>&1 && echo "docker compose" || echo docker-compose)

.PHONY: up down logs ps reset gen test test-integration test-backend test-web smoke debug

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) --profile debug down

logs:
	$(COMPOSE) logs -f --tail=100

ps:
	$(COMPOSE) ps

reset:
	$(COMPOSE) --profile debug down -v

debug:
	$(COMPOSE) --profile debug up -d opensearch-dashboards

gen:
	cd backend/api/gen && go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oapi-codegen.yaml ../../../api/openapi.yaml
	cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
	$(COMPOSE) run --rm --no-deps web npm run gen:api

test: test-backend test-web

test-integration:
	$(COMPOSE) --profile test run --rm backend-test

test-backend:
	cd backend && go test ./...

test-web:
	$(COMPOSE) run --rm --no-deps web npm test

smoke:
	./scripts/smoke.sh
