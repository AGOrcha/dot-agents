# Minimal Makefile; scripts live in /scripts.
.PHONY: run build test coverage acceptance-coverage coverage-html

run:
	go run ./cmd/dot-agents

build:
	go build -o ./bin/dot-agents ./cmd/dot-agents

build-prod:
	go build -ldflags "-s -w" -o ./bin/dot-agents ./cmd/dot-agents

test:
	go test ./...

coverage:
	go test ./... -coverprofile=coverage.out

coverage-html: coverage
	go tool cover -html=coverage.out -o coverage.html