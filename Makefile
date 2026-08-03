.PHONY: dev down prod prod-down prod-to-local test test-integration test-parity ci migrate swagger api-client openapi openapi-sync-check infra-graph infra-graph-zip infra-plan-json

export GOTOOLCHAIN := local

# Local dev database (Docker Compose Postgres on host port 64432).
# Override by exporting DATABASE_URL, e.g. to point at a remote database.
DATABASE_URL ?= postgres://ticket_pos:ticket_pos@localhost:64432/ticket_pos?sslmode=disable
export DATABASE_URL

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

# Production-parity stack: the API from the production image, migrated by that
# same image's migrate entrypoint as a one-shot. Runs alongside `make dev`.
prod:
	docker compose -f docker-compose.prod.yml up --build

prod-down:
	docker compose -f docker-compose.prod.yml down

# Replace the local dev stack's data with a copy of production: the whole
# database (server-side Cloud SQL export, since the instance has no public IP)
# and every object in the media bucket. Read-only against production, and
# destructive locally -- it prompts first. Requires `make dev` to be up and a
# gcloud login with access to the prod project.
#
# The copy includes real customer PII and payment records. `make prod-to-local
# ARGS=--yes` skips the prompt; ARGS=--db-only / --media-only does one half.
prod-to-local:
	./scripts/prod-to-local.sh $(ARGS)

test:
	cd backend && go test $$(go list ./... | grep -v '/integration$$')
	pnpm turbo test

test-integration:
	cd backend && go test ./integration/...

# Browser smoke tests against the production-parity stack. Requires `make prod`
# to be up: these run against the shipped container images, not dev servers.
test-parity:
	pnpm --filter @ticket-pos/e2e test:parity

ci:
	cd backend && go test ./... && go vet ./...
	pnpm turbo lint typecheck build
	$(MAKE) openapi-sync-check

migrate:
	cd backend && go run ./cmd/migrate

# Interactive Terraform dependency graph via Rover (github.com/im2nguyen/rover).
# Runs a read-only `terraform plan` for TF_ENV, then serves the graph until Ctrl-C.
# Needs Docker and credentials for the target environment; the env must be
# `terraform init`-ed already. Override: make infra-graph TF_ENV=prod ROVER_PORT=9001
#
# Note this is a *dependency* graph (every IAM member is a node), not an
# architecture diagram — useful for "what depends on this secret", not for a README.
TF_ENV ?= prod
ROVER_PORT ?= 9000
ROVER_IMAGE ?= im2nguyen/rover:v0.3.3
TF_DIR = terraform/envs/$(TF_ENV)
# plan.json embeds state, including secret values — gitignored, never commit it.
ROVER_DIR = $(TF_DIR)/.rover

infra-plan-json:
	mkdir -p $(ROVER_DIR)
	cd $(TF_DIR) && terraform plan -lock=false -out=.rover/plan.out
	cd $(TF_DIR) && terraform show -json .rover/plan.out > .rover/plan.json

# --user keeps Docker from leaving root-owned files in the working tree.
infra-graph: infra-plan-json
	@echo "Rover serving on http://localhost:$(ROVER_PORT) — Ctrl-C to stop"
	docker run --rm -it -p $(ROVER_PORT):9000 \
		--user $$(id -u):$$(id -g) \
		-v $(CURDIR)/$(ROVER_DIR):/src -w /src \
		$(ROVER_IMAGE) -planJSONPath=plan.json

# Self-contained static bundle: unzip anywhere and open index.html, no Docker needed.
infra-graph-zip: infra-plan-json
	docker run --rm \
		--user $$(id -u):$$(id -g) \
		-v $(CURDIR)/$(ROVER_DIR):/src -w /src \
		$(ROVER_IMAGE) -planJSONPath=plan.json -standalone=true
	@echo "Wrote $(ROVER_DIR)/rover.zip"
