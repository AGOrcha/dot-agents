# CLI-runner verifier — dot-agents repo overlay

Repo-local committed layer. Composes **after** `verifiers/verifier.base.md` (the contract) and
`verifiers/cli-runner.md` (the kind: build fresh, smoke floor with content assertions, scoped
invocations). This file adds **only** the dot-agents command matrix and the concrete content each
invocation must assert.

`--kind custom`, `--verifier-type cli-runner`.

## 1. Build fresh (required, always)

```
go build -o ./bin/da ./cmd/da
```

A build failure is a terminal `--status fail` — the tree does not produce a working binary; record
and stop.

## 2. Smoke floor (required) — assert content, not just exit 0

```
bash scripts/verify.sh
```

This is the same live-binary smoke CI runs (`.github/workflows/test.yml`, `auto-release.yml`; see
PR #101). It builds/locates `./bin/da` and asserts on **returned content** across the post-0.4.0
surface — do not accept a pass that only proves exit 0:

- **Core read-only:** `--version`/`--help`, `status` / `status --json` (asserts the JSON keys),
  `status --audit`, `doctor` / `doctor --json`, `explain`.
- **config-v2 (`config-distribution-model`):** inside a throwaway fixture manifest it runs
  `config lint` (asserts `OK`, and `file not found` / `FAILED` / `invalid JSON` on the negative
  fixtures), `config explain <field> --json` (asserts the value **and** origin), `config explain
  --origin-only` / `--all --json` (asserts the merged layer values), `config sync` (asserts it
  writes `.agentsrc.lock`), `config sync --dry-run` (asserts the lock is left byte-identical, #105),
  and `config verify` (asserts `OK`).
- **kg note lane:** `kg setup`, `kg ingest <note>`, `kg warm` (asserts `notes indexed`),
  `kg query --intent source_lookup` (asserts the ingested note id `src-smoke-note`),
  `kg query --intent graph_health` (asserts `status=healthy`), `kg bridge health` (asserts
  `available`), plus `kg --help` / `kg code-status` read-only paths.

Any failure fails the pass — a change must not regress a sibling command. If the floor fails, you
may skip the scoped invocations but record `--status fail`.

## 3. Task-scoped invocations (required when a touched path maps to a command)

Map `write_scope_touched` to commands and exercise the changed one end-to-end against `./bin/da`:

- `commands/<area>/…` (and `commands/<area>.go`) → the `da <area> …` command — e.g.
  `commands/config/…` → `da config …`, `commands/kg/…` → `da kg …`, `commands/workflow/…` →
  `da workflow …`.
- `internal/config/…` → the `da config` surface (explain/lint/sync/verify/relevance) it backs.

For the changed command:

- **Positive:** run the happy path; assert exit 0 **and** a stable output substring or a `--json`
  field value (e.g. for an `execution_profile` change:
  `da config relevance --filter topology --app-type go-cli` must show
  `verifier_sequence: unit, cli-runner`). Do not assert on the whole stream.
- **Negative:** where the change adds a failure mode, run the invalid invocation and assert a
  non-zero exit and a clear error (mirror `scripts/verify.sh`'s `expect_success=false` /
  `assert_contains` negative cases).

## 4. Record

`--command` is single-valued — pass the build+smoke entry line; put the full invocation story in
`--summary`.

```
da workflow verify record --kind custom --status <pass|fail|partial|unknown> \
  --task <task_id> --verifier-type cli-runner \
  --command "go build -o ./bin/da ./cmd/da && bash scripts/verify.sh" \
  --summary "<floor result + content asserted, scoped invocations + what each asserted, first failure, evidence>"
```

A binary that builds and smokes clean but is missing the task's intended command — or returns the
wrong content for it — is `missing-feature`, not `ok`.
