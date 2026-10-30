.PHONY: build install test lint deps

build:
	go build ./cmd/personalactivity

install:
	go install ./cmd/personalactivity

test:
	go test ./...

lint:
	golangci-lint run --allow-parallel-runners

deps:
	go mod tidy
	go mod vendor
