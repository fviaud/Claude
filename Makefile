.PHONY: build run test test-integration fmt vet clean db-up db-down

BINARY := bin/claude

# Load variables from .env if present (e.g. DATABASE_URL).
-include .env
export

build:
	go build -o $(BINARY) ./cmd/claude

run:
	go run ./cmd/claude

test:
	go test ./...

test-integration:
	TEST_DATABASE_URL=$(DATABASE_URL) go test -count=1 -p 1 ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin

db-up:
	docker compose up -d --wait postgres

db-down:
	docker compose down
