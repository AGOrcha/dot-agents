# Minimal Makefile; scripts live in /scripts.
.PHONY: run build test coverage acceptance-coverage coverage-html gate gate-cross

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

# gate-cross — the HEAVY pre-merge cross-OS tier (companion to the fast `gate`).
# ssh-runs the changed-package tests on the Windows box (pap-h@pap-home.local),
# merges local + Windows coverage into a true multi-OS per-file profile, and runs
# the per-file coverage ENFORCE over the union (exactly as CI's post-matrix
# coverage-gate job). Deterministic; never mutates the local working tree. If the
# box is unreachable it LOUD-SKIPS with exit 0 (CI is the authoritative multi-OS
# gate); set GATE_CROSS_STRICT=1 to hard-fail instead. See scripts/gate-cross.sh.
gate-cross:
	bash scripts/gate-cross.sh