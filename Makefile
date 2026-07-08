.PHONY: dev down test ci migrate

dev:
	docker compose up --build

down:
	docker compose down

test:
	cd backend && go test ./...
	pnpm turbo test

ci:
	cd backend && go test ./... && go vet ./...
	pnpm turbo lint typecheck build

migrate:
	@echo "Apply database migrations (stub: wire to backend migration runner)"
