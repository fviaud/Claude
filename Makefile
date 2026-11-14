.PHONY: build run test fmt vet clean

BINARY := bin/claude

build:
	go build -o $(BINARY) ./cmd/claude

run:
	go run ./cmd/claude

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

clean:
	rm -rf bin
