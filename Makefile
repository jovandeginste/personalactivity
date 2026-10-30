.PHONY: build install test lint deps

build:
	CGO_ENABLED=0 go build ./cmd/personalactivity

install:
	CGO_ENABLED=0 go install ./cmd/personalactivity

test:
	go test ./...

lint:
	golangci-lint run --allow-parallel-runners

deps:
	go mod tidy
	go mod vendor
