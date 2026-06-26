# Minimal Makefile; scripts live in /scripts.
.PHONY: run build test coverage acceptance-coverage coverage-html gate

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

# gate — the FAST every-push merge-blocking mandate (single source of truth).
# Runs build + vet (POSIX + windows cross-compile) + gofmt + FULL per-file
# coverage ENFORCE (exactly as CI: coverage-gate.sh enforce mode + the shared
# coverage-exceptions allowlist + 95% threshold). The prek pre-push hook invokes
# this, and it is deterministic (no working-tree mutation). See scripts/gate.sh
# and the local-gate-ci-parity spec. The heavier pre-merge tier (real Windows
# tests, merged multi-OS coverage, native sonar) is `make gate-cross` (sibling
# task t3) — it also closes the one residual gap here: a NEW platform-only file
# that has zero coverage rows on this single-OS local run.
gate:
	bash scripts/gate.sh