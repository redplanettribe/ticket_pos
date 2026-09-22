.PHONY: dev down prod prod-down prod-to-local seed-dev deploy test test-integration test-parity ci ci-preflight ci-go ci-js ci-openapi migrate swagger api-client openapi openapi-sync-check infra-graph infra-graph-zip infra-plan-json

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

# Restore the dev seed data into the running local stack. The seed lives in
# migrations, so it is applied once on a fresh database and is missing for good
# after `make prod-to-local` -- production has never held those rows, but its
# schema_migrations claims their versions are applied. Idempotent, and it leaves
# any production copy alongside it untouched.
seed-dev:
	./scripts/seed-dev.sh

test:
	cd backend && go test $$(go list ./... | grep -v '/integration$$')
	pnpm turbo test

test-integration:
	cd backend && go test ./integration/...

# Browser smoke tests against the production-parity stack. Requires `make prod`
# to be up: these run against the shipped container images, not dev servers.
test-parity:
	pnpm --filter @ticket-pos/e2e test:parity

# The Go suite runs in two invocations rather than one `./...`, and it has to.
# `go test` runs separate packages CONCURRENTLY, and two packages now own a
# Postgres testcontainer: the integration harness, and the repository-level
# concurrency test the Follow Digest drain needs (docs/testing.md sanctions
# repository tests for locking only, which is what that one is). Run together,
# one container is torn down under the other and the integration package fails
# with "connection reset by peer" — a failure about the runner, not the code.
# Splitting them along the layer boundary the testing guide already draws keeps
# every package covered and never has two container owners in flight at once.
#
# It lives in its own target because CI runs the Go suite as a separate job and
# must not restate how to invoke it: stated twice, the split was fixed here and
# missed there, and the branch that introduced the second container owner went
# green locally and red on the runner.
#
# `go vet` runs first: make stops at the first failing command, so vet after
# the tests would never report on a branch whose tests fail.
ci-go:
	cd backend && go vet ./...
	cd backend && go test $$(go list ./... | grep -v '/integration$$')
	cd backend && go test ./integration/...

# The JS job, step for step: a frozen-lockfile install (CI's first step, which
# a plain `pnpm turbo ...` skips and then fails on a missing module), lint,
# typecheck and build in one turbo invocation as the runner does (turbo.json
# runs each app's build after its typecheck, as both write .next), then every
# workspace's unit tests minus the Playwright suite, which needs a stack already
# serving that neither a runner nor this target has.
ci-js: ci-preflight
	pnpm install --frozen-lockfile
	pnpm turbo lint typecheck build
	pnpm turbo test --filter='!@ticket-pos/e2e'

# The OpenAPI job: regenerate the spec and client and require a clean diff
# against HEAD. Regenerated artifacts have to be COMMITTED, not merely present,
# or a correct regeneration still fails here exactly as it fails on the runner.
ci-openapi: ci-preflight
	pnpm install --frozen-lockfile
	$(MAKE) openapi-sync-check

# The whole of .github/workflows/ci.yml on this machine: the three jobs, in the
# order the workflow lists them. The runner starts each job from a clean
# checkout; this tree does not, which is what ci-preflight is for.
ci: ci-go ci-js ci-openapi
	@echo "CI passed locally: go-test, js-checks, openapi-sync"

# Fail fast on what makes a local run diverge from the runner. The `make dev`
# containers run `next dev` as root against the mounted repo, so they own and
# keep rewriting apps/*/.next, and a container-side install leaves root-owned
# pnpm symlinks and .bin shims under node_modules. `pnpm install`, the
# typecheck's `next typegen` and `next build` then die EACCES into the run.
# Check up front, name the topmost offenders, and print the fix -- through a
# throwaway container, never sudo.
# Stop the two Next.js containers first or .next comes straight back.
ci-preflight:
	@bad=$$(find apps/*/.next apps/*/node_modules packages/*/node_modules -maxdepth 2 ! -user $$(id -un) -prune -print 2>/dev/null); \
	if [ -n "$$bad" ]; then \
		echo "ci-preflight: $$(echo "$$bad" | wc -l) entries not owned by $$(id -un) would fail pnpm install / next build:"; \
		echo "$$bad" | head -10 | sed 's/^/  /'; \
		[ $$(echo "$$bad" | wc -l) -gt 10 ] && echo "  ..."; \
		echo; \
		echo "Fix (no sudo), then re-run:"; \
		echo "  docker compose stop storefront staff"; \
		echo "  docker run --rm -v \"$$PWD:/w\" -w /w alpine:3 rm -rf $$(echo "$$bad" | tr '\n' ' ')"; \
		echo "  pnpm install"; \
		echo "and \`docker compose start storefront staff\` afterwards."; \
		exit 1; \
	fi

migrate:
	cd backend && go run ./cmd/migrate

# Production deploy from this machine, for when GitHub Actions cannot run (out
# of minutes). Same sequence as .github/workflows/deploy.yml -- build and push
# the three SHA-tagged images, run the migrate Job to completion, then roll the
# three services -- against a `main` that is clean and fast-forwarded to
# origin/main first, so what ships is a commit that is on GitHub. Needs Docker
# and a `gcloud auth login` with deploy rights on the prod project.
#
# `make deploy ARGS=--no-pull` deploys the current HEAD of main without
# fetching. See scripts/deploy.sh for the details and for DEPLOY_IMPERSONATE.
deploy:
	./scripts/deploy.sh $(ARGS)

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
