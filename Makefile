.PHONY: help run-api run-worker run-outbox build build-api build-worker build-outbox \
        test test-race test-integration cover seed-admin fmt vet check tidy \
        docker-build docker-up docker-up-local docker-down docker-logs backup

# help prints the available targets.
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

run-api: ## Run the HTTP API server
	go run ./cmd/api

run-worker: ## Run the background job worker
	go run ./cmd/worker

run-outbox: ## Run the transactional outbox publisher
	go run ./cmd/outbox-publisher

build: build-api build-worker build-outbox ## Build every binary

build-api:
	go build -o bin/api ./cmd/api

build-worker:
	go build -o bin/worker ./cmd/worker

build-outbox:
	go build -o bin/outbox-publisher ./cmd/outbox-publisher

# UNIT is every package but tests/, which needs a MongoDB replica set and is
# run by its own target.
UNIT := ./cmd/... ./internal/... ./scripts/...

test: ## Run the unit tests
	go test $(UNIT)

test-race: ## Run the unit tests with the race detector
	go test -race -count=1 $(UNIT)

test-integration: ## Run the MongoDB-backed integration suite (needs a replica set)
	@set -a; [ -f .env ] && . ./.env; set +a; \
	MONGODB_TEST_URI="$${MONGODB_TEST_URI:-$$MONGODB_URI}" \
	MONGODB_TEST_DATABASE="$${MONGODB_TEST_DATABASE:-denisco_integration_test}" \
	go test -race -count=1 -timeout 20m ./tests/...

cover: ## Run tests and open the coverage report
	go test -coverprofile=coverage.out $(UNIT)
	go tool cover -func=coverage.out | tail -1

seed-admin: ## Create the first administrator (set ADMIN_EMAIL and ADMIN_PASSWORD)
	go run ./scripts/seed-admin -email "$(ADMIN_EMAIL)"

fmt: ## Format the source tree
	gofmt -w .

vet:
	go vet ./...

check: fmt vet test ## Format, vet and run the unit tests

tidy:
	go mod tidy

docker-build: ## Build the deployment images
	docker compose -f deployments/compose.yaml build

docker-up: ## Start the deployment stack
	docker compose -f deployments/compose.yaml up -d

docker-up-local: ## Start the stack with the application ports published
	docker compose -f deployments/compose.yaml -f deployments/compose.local.yaml up -d

docker-down: ## Stop the deployment stack
	docker compose -f deployments/compose.yaml down

docker-logs: ## Follow the deployment stack's logs
	docker compose -f deployments/compose.yaml logs -f --tail=100

backup: ## Back up MongoDB and the Redis queue (see deployments/README.md)
	deployments/backup.sh
