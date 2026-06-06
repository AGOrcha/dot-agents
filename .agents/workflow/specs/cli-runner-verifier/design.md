# Spec: `cli-runner` verifier profile

**Status:** draft (for review) — feeds the `config-relevance-profiles` plan (new task t7), folds into PR #29.
**Depends on:** the execution-profile layer (`skill-relevance-filter.md`, shipped t1–t6) and the
verifier-profile taxonomy in [`app-type-profiles/design.md`](../app-type-profiles/design.md) §11.
**Grounds in:** the existing CLI smoke harness `scripts/verify.sh`, the `unit` verifier prompt
(`.agents/prompts/verifiers/unit.project.md`), and `schemas/verification-result.schema.json`.

---

## 1. Problem & goal

`cli-runner` is referenced as the second go-cli verifier across the design and the test corpus
(`verifier_sequence: ["unit", "cli-runner"]`) but was never built — there is no prompt, no
`verifier_profiles` entry, and no real invocation. So the go-cli execution profile resolves a
`verifier_sequence` of only `["unit"]` while its topology declares `verifiers_per_executor: 2`,
which is incoherent (two passes, one named profile).

The gap it leaves is real: `unit` (`go test`) proves the **code** is correct but never proves the
**wired-up binary actually runs**. A CLI tool can pass every unit test and still fail to build a
working command, mis-register a subcommand, or break a flag. `cli-runner` is the missing
end-to-end half for the `go-cli` app_type.

**Goal:** make `cli-runner` a real, first-class verifier-class profile — a sibling of `unit` —
that builds the `da` binary and exercises actual CLI invocations end-to-end, emits a schema-valid
typed result, and is wired into the go-cli execution profile so the topology is coherent.

## 2. Decisions (review these)

- **D1 — Name `cli-runner`.** Slug-valid (`^[a-z][a-z0-9_-]*$`) and already the canonical example
  name. It deliberately does **not** end in `-verifier` or begin with `verifier-`, per the
  `pr-ci-verifier-integration-audit` naming guidance (avoids collision with the shipped `verifier`
  agent and the ISP verifier-class namespace).
- **D2 — A pre-merge ISP verifier-class profile, not the post-merge watcher.** `cli-runner` runs in
  the `verify` stage after `impl-handoff.yaml` lands, exactly like `unit`. It is unrelated to the
  proposed post-merge `pr-ci-watch` agent (which lives outside the staged-runtime chain).
- **D3 — Prompt-driven, modeled on `unit.project.md`.** No new Go command. The profile is a
  `--prompt-file` overlay: read `impl-handoff.yaml`, run the CLI smoke, write the result. Same
  shape and role boundary as the other verifier prompts.
- **D4 — Reuse `scripts/verify.sh` as the smoke floor; add task-scoped checks on top.** The
  existing script already builds/locates `da` and exercises `--version/--help`, `status`, `doctor`,
  `explain`, `workflow`, dry-runs, and expected-failure cases. `cli-runner` runs that as the floor,
  then adds targeted invocations for the **commands the task touched** (mapped from
  `write_scope_touched`): run the new/changed subcommand, assert exit code and key output, including
  a negative case where the change introduces a failure mode. Reusing the script keeps one smoke
  definition rather than duplicating command lists in a prompt.
- **D5 — Typed result `cli-runner.result.yaml`.** Written to
  `.agents/active/verification/<task_id>/cli-runner.result.yaml`, validated against
  `schemas/verification-result.schema.json` (`verifier_type: cli-runner`, `status` ∈
  pass|fail|partial|unknown). No schema change needed — `verifier_type` is an open slug.
- **D6 — Dual-write the wiring to keep the two resolution paths in agreement.** Update **both**
  `app_type_verifier_map.go-cli` (the legacy flat path the bundle hydrates today) and the
  `execution_profile.by_app_type.go-cli.topology.verifier_sequence` to `["unit", "cli-runner"]`,
  in the same change, so the flat map and the profile never disagree on order (app-type-profiles
  §11.4: migrate "without silently changing verifier invocation order or outcomes").

## 3. Requirements (behavioral)

1. A `cli-runner` verifier produces a schema-valid `cli-runner.result.yaml` with `status: fail`
   when the binary does not build or a required invocation exits non-zero, and `status: pass` only
   when the smoke floor and the task-scoped invocations all succeed.
2. It never edits product code (verifier role boundary); a broken tree yields `status: fail` with a
   clear `summary`, not a fix.
3. `go-cli`'s resolved `verifier_sequence` is `["unit", "cli-runner"]` from both the legacy map and
   the execution profile, making `verifiers_per_executor: 2` coherent (one pass per named profile).
4. `da config verify` stays green and `da config relevance --filter topology --app-type go-cli`
   reports the 2-element sequence.

## 4. Done criteria

1. `.agents/prompts/verifiers/cli-runner.project.md` exists, mirrors the verifier-prompt contract
   (role boundary / preconditions / commands / result artifact / evidence classification), and
   specifies the `scripts/verify.sh` floor + task-scoped invocations.
2. `verifier_profiles.cli-runner` (`{label, prompt_files}`) is in `.agentsrc.json`; `da config
   verify` passes.
3. `app_type_verifier_map.go-cli` and `execution_profile…go-cli.topology.verifier_sequence` both
   equal `["unit", "cli-runner"]`; `da config relevance --filter topology --app-type go-cli` shows
   it.
4. `docs/CONFIG_RELEVANCE.md` reflects the `[unit, cli-runner]` sequence and explains the floor.
5. Existing test fixtures that already name `["unit", "cli-runner"]` now describe a real profile.

## 5. Out of scope / deferred

- The first-class **versioned** `verifier_profiles` config layer (app-type-profiles §11.4–11.5):
  `cli-runner` ships in today's flat `verifier_profiles` map; migration to versioned/composable
  profiles is future work and unblocked by this.
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

- **skill-relevance-filter / config-relevance-profiles** — the execution profile this completes;
  `cli-runner` makes the go-cli `verifier_sequence` real.
- **app-type-profiles §11** — the verifier-profile taxonomy and the future versioned layer.
- **pr-ci-verifier-integration-audit** — the naming/role boundary this respects (pre-merge
  verifier-class profile, not the post-merge watcher).
