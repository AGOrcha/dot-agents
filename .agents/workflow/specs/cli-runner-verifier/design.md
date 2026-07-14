# Spec: `cli-runner` verifier profile

**Status:** delivered — the profile, wiring, and CI floor are in place; this spec is now the
record of *why* `cli-runner` exists and *what* it asserts. Activation (functional prompt files)
landed on `feat/cli-runner-verifier-active`.
**Grounds in:** the live-binary smoke harness `scripts/verify.sh` (extended to the 0.4.0 config-v2 +
kg surface in PR #101, "ci: extend live-binary smoke to the 0.4.0 config + kg surface"), the `unit`
verifier prompt (`.agents/prompts/verifiers/unit.project.md`), and
`schemas/verification-result.schema.json`.

---

> **Consolidation update (2026-06-07) — `stage-profile-and-routing-consolidation`:** `verifier_profiles` + `reviewer_profiles` are now unified into one **typed** `stage_profiles` map (stage `executor`/`verifier`/`reviewer`/`orchestrator` → slug → `{label, prompt_files}`), and `app_type_verifier_map` is **retired** into `execution_profile.by_app_type.<type>.topology.verifier_sequence`. Legacy keys still load (folded, deprecated). Mentions of those keys below describe the pre-consolidation surface — read them as the new model.


## 1. Problem & goal

`cli-runner` is the second go-cli verifier across the design and the test corpus
(`verifier_sequence: ["unit", "cli-runner"]`). The gap it closes is real: `unit` (`go test`) proves
the **code** is correct but never proves the **wired-up binary actually runs**. A CLI tool can pass
every unit test and still fail to build a working command, mis-register a subcommand, break a flag,
or ship a config-v2 / kg subtree that exits zero while returning the wrong content. `cli-runner` is
the missing end-to-end half for the `go-cli` app_type.

**Goal:** make `cli-runner` a real, first-class verifier-class profile — a sibling of `unit` — that
builds the `da` binary fresh, exercises actual CLI invocations end-to-end **and asserts on returned
content** (not just exit 0), emits a schema-valid typed result, and is wired into the go-cli
execution profile so the topology is coherent.

## 2. Decisions

- **D1 — Name `cli-runner`.** Slug-valid (`^[a-z][a-z0-9_-]*$`). It deliberately does **not** end in
  `-verifier` or begin with `verifier-` (avoids collision with the shipped `verifier` agent and the
  ISP verifier-class namespace).
- **D2 — A pre-merge ISP verifier-class profile, not the post-merge watcher.** `cli-runner` runs in
  the `verify` stage after `impl-handoff.yaml` lands, exactly like `unit`. It is unrelated to any
  post-merge PR-watch agent (which lives outside the staged-runtime chain).
- **D3 — Prompt-driven, modeled on `unit`.** No new Go command. The profile is a composed prompt
  overlay (base → per-type → repo-local), resolved by `da workflow resolve-prompt --kind verifier
  --slug cli-runner`: read `impl-handoff.yaml`, run the CLI smoke, write the typed result. Same
  shape and role boundary as the other verifier prompts.
- **D4 — Reuse `scripts/verify.sh` as the smoke floor; add task-scoped checks on top.** The script
  builds/locates `da` and exercises `--version`/`--help`, `status`, `doctor`, `explain`, `workflow`,
  the **config-v2** surface (`config explain`/`lint`/`sync`/`verify`, with **content** assertions on
  field+origin and `OK`/`FAILED` strings), and the **kg** note lane (`kg setup`/`ingest`/`warm`/
  `query`/`bridge`, asserting on returned content such as `notes indexed`, `src-smoke-note`,
  `status=healthy`, `available`) plus kg help/read-only paths. `cli-runner` runs that as the floor,
  then adds targeted invocations for the **commands the task touched** (mapped from
  `write_scope_touched`). Reusing the script keeps one smoke definition rather than duplicating
  command lists in a prompt, and keeps the verifier in lockstep with the CI floor (PR #101).
- **D5 — Typed result `cli-runner.result.yaml`.** Recorded via
  `da workflow verify record --kind custom --status <…> --task <task_id> --verifier-type cli-runner`,
  which writes `.agents/active/verification/<task_id>/cli-runner.result.yaml`, validated against
  `schemas/verification-result.schema.json`. No schema change needed — `verifier_type` is an open
  slug. `--command` is single-valued, so the recorded command is the build/smoke entry line and the
  full invocation story lives in `--summary`.

## 3. Requirements (behavioral)

1. A `cli-runner` verifier produces a schema-valid `cli-runner.result.yaml` with `status: fail`
   when the binary does not build or a required invocation exits non-zero **or returns wrong
   content**, and `status: pass` only when the smoke floor and the task-scoped invocations all
   succeed.
2. It never edits product code (verifier role boundary); a broken tree yields `status: fail` with a
   clear `summary`, not a fix.
3. `go-cli`'s resolved `verifier_sequence` is `["unit", "cli-runner"]`, making
   `verifiers_per_executor: 2` coherent (one pass per named profile).
4. `da config verify` stays green and `da config relevance --filter topology --app-type go-cli`
   reports the 2-element sequence.

## 4. Done criteria — status

1. **DONE.** `.agents/prompts/verifiers/cli-runner.project.md` exists, composes after
   `verifier.base.md` + `cli-runner.md`, and names the `scripts/verify.sh` floor + task-scoped
   invocations + the content-assertion contract.
2. **DONE.** `stage_profiles.verifier.cli-runner` (`{label, prompt_files}`) is in `.agentsrc.json`;
   `da config verify` passes. (Note: the config layer was renamed from the draft-era
   `verifier_profiles` flat map to the unified `stage_profiles.{verifier,reviewer}` map by the
   stage-profile-and-routing-consolidation work; `cli-runner` lives under `stage_profiles.verifier`.)
3. **DONE.** `execution_profile.by_app_type.go-cli.topology.verifier_sequence` equals
   `["unit", "cli-runner"]`; `da config relevance --filter topology --app-type go-cli` shows it.
   (The draft-era `app_type_verifier_map` flat path — and its D6 dual-write — was **retired** by the
   config-v2 migration; the execution profile is now the single resolution path, so there is no
   second map to keep in agreement.)
4. **DONE.** `docs/CONFIG_RELEVANCE.md` reflects the `[unit, cli-runner]` sequence.
5. **DONE.** The CI live-binary smoke (`scripts/verify.sh`, run by `.github/workflows/test.yml` and
   `auto-release.yml`) was extended to the 0.4.0 config-v2 + kg surface with content assertions in
   PR #101, so the floor `cli-runner` reuses is the same one CI enforces.

## 5. Out of scope / deferred

- The first-class **versioned / composable** profile layer: `cli-runner` ships in today's
  `stage_profiles.verifier` map; migration to a versioned layer is future work and unblocked by this.
- A Go subcommand to invoke `cli-runner` directly (it is prompt-driven, like `unit`).
- Per-platform CLI matrices (Windows binary smoke) beyond what `scripts/verify.sh` covers.

## 6. Resolved decisions (were open questions)

- **Q1 → full floor + scoped.** Every pass runs the entire `scripts/verify.sh` smoke as the floor,
  then adds task-scoped invocations for the touched command(s). A regression in any command —
  touched or not — fails the pass; the build+smoke cost (seconds) is accepted.
- **Q2 → always build fresh.** Each run builds `go build -o ./bin/da ./cmd/da` from the current
  tree, so the verifier also proves the tree compiles into a working binary. A prebuilt binary is
  never trusted.

## 7. Relationships

- **config-relevance / execution profiles** — the profile this completes; `cli-runner` makes the
  go-cli `verifier_sequence` real.
- **stage-profile-and-routing-consolidation** — unified the verifier/reviewer profile maps into
  `stage_profiles` and retired `app_type_verifier_map`; `cli-runner` lives under that unified key.
- **PR #101 (live-binary CI smoke)** — the realized CI floor `cli-runner` reuses: the same
  `scripts/verify.sh` content assertions over the config-v2 + kg surface.
- **verifier-owns-ci-watch-shift-left** — `cli-runner` is terminal-state (writes a typed result via
  `verify record`) and shifts the built-binary smoke left into the pre-merge verify stage.
