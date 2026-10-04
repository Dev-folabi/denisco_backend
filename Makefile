.PHONY: run-api run-worker run-outbox build build-api build-worker build-outbox test fmt vet tidy

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

run-outbox:
	go run ./cmd/outbox-publisher

build: build-api build-worker build-outbox

build-api:
	go build -o bin/api ./cmd/api

build-worker:
	go build -o bin/worker ./cmd/worker

build-outbox:
	go build -o bin/outbox-publisher ./cmd/outbox-publisher

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

tidy:
	go mod tidy
