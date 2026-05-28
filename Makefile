# Minimal Makefile; scripts live in /scripts.
.PHONY: run build test coverage acceptance-coverage coverage-html

run:
	go run ./cmd/da

build:
	go build -o ./bin/da ./cmd/da

build-prod:
	go build -ldflags "-s -w" -o ./bin/da ./cmd/da

test:
	go test ./...

coverage:
	go test ./... -coverprofile=coverage.out

coverage-html: coverage
	go tool cover -html=coverage.out -o coverage.html