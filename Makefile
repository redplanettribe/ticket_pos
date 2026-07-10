.PHONY: dev down test test-integration ci migrate swagger

export GOTOOLCHAIN := local

swagger:
	cd backend && go run github.com/swaggo/swag/cmd/swag@v1.16.4 init -g cmd/server/main.go -o docs
	cp backend/docs/swagger.yaml openapi/openapi.yaml

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

migrate:
	cd backend && go run ./cmd/migrate
