.PHONY: build run test test-integration fmt vet clean db-up db-down db-reset docker-build up down smoke-test

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

# Deletes the local database volume: everything is recreated on next start.
db-reset:
	docker compose down -v

docker-build:
	docker build -t claude:latest .

# Builds the image and checks it in real containers (PostgreSQL + API).
smoke-test:
	scripts/smoke-test.sh

# Starts PostgreSQL and the API in containers (needs API_KEYS, e.g. from .env).
up:
	docker compose --profile app up -d --build --wait

down:
	docker compose --profile app down
