.PHONY: dev down test test-integration ci migrate swagger api-client openapi openapi-sync-check

export GOTOOLCHAIN := local

swagger:
	cd backend && go run github.com/swaggo/swag/v2/cmd/swag@v2.0.0-rc5 init -g cmd/server/main.go -o docs --v3.1
	cp backend/docs/swagger.yaml openapi/openapi.yaml

api-client:
	pnpm --filter @ticket-pos/api-client generate

openapi: swagger api-client

openapi-sync-check: swagger api-client
	git diff --exit-code -- openapi/ packages/api-client/src/generated/ backend/docs/

dev:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test $$(go list ./... | grep -v '/integration$$')
	pnpm turbo test

test-integration:
	cd backend && go test ./integration/...

ci:
	cd backend && go test ./... && go vet ./...
	pnpm turbo lint typecheck build
	$(MAKE) openapi-sync-check

migrate:
	cd backend && go run ./cmd/migrate
