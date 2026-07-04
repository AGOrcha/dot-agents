# Eval Harness

**Status:** active
**Owners:** dot-agents
**Related:** [`OUTCOME_SCORING_RUBRIC.md`](./OUTCOME_SCORING_RUBRIC.md) (the rubric the score
breakdown is produced under); [`internal/eval/store/store.go`](../internal/eval/store/store.go)
(the `eval-run.yaml` / `taskspec.yaml` writer); [`internal/eval/taskspec.go`](../internal/eval/taskspec.go)
(the `TaskSpec` type); [`internal/eval/scoringbridge/`](../internal/eval/scoringbridge/)
(the `iteration-log/iter-1.yaml` + `iter-1.score.yaml` writer)

The R4 eval harness generates a language-agnostic `TaskSpec`, runs an agent against it in a
sandbox, verifies the result, and scores the run through the production R1 scoring rubric. It
persists each run as a small set of YAML sidecar files under a canonical run directory.

> **Document map.** The [user guide](#pipeline) below covers generating tasks, running the
> harness, the `TaskSpec` schema, language coverage, the sandbox model, how outcomes feed R1,
> and the CLI. The [R2 visibility contract](#r2-visibility-contract) is a specialized appendix:
> the exact on-disk shape a dashboard consumes. The two are complementary; the appendix is not
> folded into the guide.

## Pipeline

`da eval run` drives one task through five stages, each behind a seam interface so the harness is
language-agnostic ([`internal/eval/harness/harness.go`](../internal/eval/harness/harness.go)):

1. **generate** — a per-language generator synthesises a `TaskSpec` from the Tree-sitter
   knowledge graph ([`internal/eval/gen/`](../internal/eval/gen/)).
2. **provision** — the sandbox checks out an isolated working tree at the source repo's HEAD
   ([`internal/eval/sandbox/`](../internal/eval/sandbox/)).
3. **run** — the agent runner invokes the configured agent inside that working tree
   ([`internal/eval/runner/`](../internal/eval/runner/)). It classifies a missing agent CLI
   and an auth-shaped failure as a distinct signal — see
   [Agent CLI and the isolated HOME](#agent-cli-and-the-isolated-home).
4. **verify** — the language verifier runs the spec's `build_cmd` then `test_cmd`, after a
   **toolchain pre-flight** that resolves the required interpreter/compiler on PATH
   ([`internal/eval/verifier/`](../internal/eval/verifier/)) — see
   [Toolchain pre-flight](#toolchain-pre-flight).
5. **score** — the scoring bridge writes an R1-shaped iteration record and scores it under the
   production rubric ([`internal/eval/scoringbridge/`](../internal/eval/scoringbridge/)).

A completed run is persisted as a small set of YAML sidecars under
`.agents/eval/runs/<run-id>/`; the store owns `taskspec.yaml` + `eval-run.yaml` and the score
stage owns the `iteration-log/` sidecars ([`internal/eval/store/store.go`](../internal/eval/store/store.go)).
An agent or verifier that *fails* is not a pipeline error — the failure flows into the score;
only an infrastructure fault aborts the run. Those aborts are kept **distinct from a scored
failure** so an environment problem is never mistaken for the agent producing bad code:

| Abort | Top-line prefix | Meaning |
| --- | --- | --- |
| no generator / sandbox provision failure / verifier step could not start | `harness: …` | classic wiring / infra fault. |
| the language toolchain is not installed | `harness: toolchain unavailable: …` (wraps `*verifier.ToolchainError`) | the interpreter/compiler the `test_cmd`/`build_cmd` needs is absent on PATH — see [Toolchain pre-flight](#toolchain-pre-flight). |
| the agent CLI is missing or fails to authenticate | `harness: agent did not run: …` (wraps `*runner.AgentStartError`) | the CLI is not on PATH, or it exited with an auth/config failure under the sandbox's credential-free HOME — see [Agent CLI and the isolated HOME](#agent-cli-and-the-isolated-home). |

A **scored** run, by contrast, is one where the toolchain was present, the agent ran and
completed, and the verifier's `build_cmd`/`test_cmd` produced a real pass/fail — that failure is
data, and it flows into the score.

## CLI usage

The operator surface is the `da eval` command group
([`commands/eval/eval.go`](../commands/eval/eval.go), wired at
[`commands/root.go`](../commands/root.go)). It mirrors `da score`'s idioms: subcommands under a
group, a `--repo-dir` root resolver, and the global `--json` flag.

### `da eval gen` — synthesise a TaskSpec

```
da eval gen --language go
da eval gen --language typescript --difficulty medium
da eval gen --language python --template add-test-coverage --out task.yaml
```

| Flag | Meaning |
| --- | --- |
| `--language` | Task language: `go`, `python`, or `typescript` (required). |
| `--difficulty` | Constrain the band: `easy`, `medium`, or `hard` (default: the generator's choice). |
| `--template` | Task template id: `impl-pure-fn` (default), `refactor-extract`, or `add-test-coverage`. |
| `--out` | Write the `TaskSpec` YAML to this file (atomic write) instead of stdout. |

> **Build the graph for *this* repo first.** `da eval gen` reads the **global**
> knowledge-graph store at `$KG_HOME/ops/graphstore.db` — the same warm store
> `da kg` maintains — not a repo-local store. Before generating, build (or warm)
> the graph for the repository you are standing in with `da kg build` (or
> `da kg warm`). If the global KG was last built for a *different* repository,
> `gen` fails loudly rather than emitting a spec against foreign paths — its seed
> paths "resolve outside the repository root". Repo-scoped `--repo-dir` generation
> and a cross-machine portable KG are planned for 0.5.1.

### `da eval run` — run one task end-to-end and score it

```
da eval run --language go
da eval run --language go --agent codex
da eval run --task task.yaml
```

| Flag | Meaning |
| --- | --- |
| `--language` | Task language (inferred from `--task` when a spec is supplied). |
| `--task` | Run a pre-generated `TaskSpec` YAML instead of generating one. |
| `--agent` | Agent adapter: `claude` (default), `codex`, or `copilot`. |
| `--difficulty` | Constrain the generated band (ignored with `--task`). |
| `--template` | Task template id (ignored with `--task`). |
| `--repo-dir` | Repository root (default: current working directory). |

The flag that selects the agent is `--agent`, not `--runner`; the internal
`runner.Adapter` type is unchanged, only the CLI flag string is `agent`
([`commands/eval/run.go`](../commands/eval/run.go),
[`internal/eval/runner/runner.go`](../internal/eval/runner/runner.go)). `da eval run` also
sweeps stale sandbox worktrees from crashed prior runs before it provisions its own — see
[Sandbox model](#sandbox-model). Add the global `--json` flag for machine-readable output.

### `da eval ls` — list persisted runs

```
da eval ls
da eval ls --repo-dir /path/to/repo
```

Lists the persisted eval runs under `<repo>/.agents/eval/runs/`, reading each run's
`eval-run.yaml` summary. Takes `--repo-dir` and the global `--json`
([`commands/eval/ls.go`](../commands/eval/ls.go)).

## TaskSpec schema

The `TaskSpec` is the versioned, language-agnostic description of one evaluable task and the
central contract of the harness: generators produce it, the sandbox provisions against it,
verifiers consume its verification commands, and the scoring bridge records it. It is defined and
parsed in [`internal/eval/taskspec.go`](../internal/eval/taskspec.go) (`TaskSpec` /
`ParseTaskSpec`) and round-trips through YAML via the canonical field tags, so the on-disk
`taskspec.yaml` sidecar matches the in-memory shape exactly.

| Field | YAML key | Type | Notes |
| --- | --- | --- | --- |
| Schema version | `task_spec_version` | int | Must equal `CurrentTaskSpecVersion` (`1`). A mismatched version is rejected by `ParseTaskSpec`. |
| Task id | `task_id` | string | Required. Deterministic, e.g. `kg-go-impl-<seed>`; the run id is derived from it. |
| Language | `language` | string | `go`, `python`, or `typescript` (`Language.Valid()`). |
| Difficulty band | `difficulty` | string | `easy` \| `medium` \| `hard` — reproducibly derived from the KG signals below (see [Language coverage](#language-coverage) and `internal/eval/difficulty.go`). |
| Difficulty signals | `difficulty_signals` | map[string]int | KG-derived structural counts the band is bucketed from; omitted when empty. Canonical keys: `involved_symbols`, `edge_count`, `cyclomatic_complexity` (`difficulty.go`). Emitted in sorted key order for byte-stable output. |
| Provenance | `generated_from` | object | `kind` (`kg_template` \| `benchmark_seed`), `template_id`, and an optional `kg_query` (`intent`, `seed_symbol`). |
| Prompt | `prompt` | string | Required. The full instruction the agent runs against. |
| Expected artifacts | `solution_artifacts` | list | Files the task expects to be written, each `{path, role}` (e.g. `role: target`); omitted when empty. |
| Verification | `verification` | object | `build_cmd` (optional []string), `test_cmd` (required []string), `timeout_seconds` (optional int, applied as a context deadline when non-zero). |

`ParseTaskSpec` decodes strictly (`KnownFields(true)`) so a stale-version or typo'd sidecar is
rejected rather than silently misread, then runs `TaskSpec.Validate` (version match, non-empty
`task_id` and `prompt`, valid `language`/`difficulty`/`kind`, non-empty `test_cmd`,
non-negative `timeout_seconds`).

The test command is **hidden from the agent** (R4 decision D4.7): the agent sees the prompt, the
verifier runs `verification.test_cmd` after the agent finishes.

### Example

A Go `impl-pure-fn` task, as the generator emits it (values illustrative; keys and shape are the
generator's actual output — [`internal/eval/gen/gencore/generator.go`](../internal/eval/gen/gencore/generator.go)):

```yaml
task_spec_version: 1
task_id: kg-go-impl-mathx-gcd
language: go
difficulty: medium
difficulty_signals:
    cyclomatic_complexity: 4
    edge_count: 9
    involved_symbols: 6
generated_from:
    kind: kg_template
    template_id: impl-pure-fn
    kg_query:
        intent: code_context
        seed_symbol: mathx.GCD
prompt: |-
    Implement the function `mathx.GCD` in `mathx/gcd.go` so that the existing tests pass.

    Nearby symbols (within 2 hops): mathx.abs, mathx.Reduce

    Constraints:
    - Do not modify any existing *_test.go file.
    - The solution must satisfy: go test -race ./mathx/...
solution_artifacts:
    - path: mathx/gcd.go
      role: target
verification:
    build_cmd:
        - go
        - build
        - ./mathx/...
    test_cmd:
        - go
        - test
        - -race
        - ./mathx/...
    timeout_seconds: 120
```

A byte-exact on-disk sample (hand-frozen for the R2 contract) ships at
[`internal/eval/store/testdata/r2contract-go-impl-pure-fn/taskspec.yaml`](../internal/eval/store/testdata/r2contract-go-impl-pure-fn/taskspec.yaml).

## Language coverage

The v1 harness covers three languages. Each is a thin adapter over shared engines: a **generator**
(`internal/eval/gen/<lang>`) supplies a `gencore.Profile` to the shared generation engine
([`internal/eval/gen/gencore/`](../internal/eval/gen/gencore/)), and a **verifier**
(`internal/eval/verifier/<lang>`) embeds the shared run engine
([`internal/eval/verifier/engine.go`](../internal/eval/verifier/engine.go)) and contributes only
its `Language()` identity. All generation control flow (template selection, KG seed search,
difficulty derivation, prompt rendering) lives in `gencore` once; the build-then-test run loop
lives in `verifier.BaseVerifier` once.

| Language | `language` | Generator profile | Verifier | `build_cmd` | `test_cmd` |
| --- | --- | --- | --- | --- | --- |
| Go | `go` | [`gen/golang`](../internal/eval/gen/golang/generator.go) | [`verifier/golang`](../internal/eval/verifier/golang/verifier.go) | `go build ./<dir>/...` | `go test -race ./<dir>/...` |
| Python | `python` | [`gen/python`](../internal/eval/gen/python/generator.go) | [`verifier/python`](../internal/eval/verifier/python/verifier.go) | `python -m py_compile <file>` | `python -m pytest -v <dir>` |
| TypeScript | `typescript` | [`gen/typescript`](../internal/eval/gen/typescript/generator.go) | [`verifier/typescript`](../internal/eval/verifier/typescript/verifier.go) | `tsc --noEmit` | `node --test <dir>/*.test.ts` |

Each generator resolves the seed's implementation file and derives the language's conventional
test file (`foo.go` → `foo_test.go`; `utils.py` → `test_utils.py`; `foo.ts` → `foo.test.ts`) and
verify target (Go package pattern, Python directory, TypeScript test glob). The verify commands
above are emitted onto the `TaskSpec`; the shared `BaseVerifier` runs `build_cmd` first and
short-circuits the test step on a non-zero build exit, applies `timeout_seconds` as a context
deadline when non-zero, and records the outcome as a
[`VerifyResult`](../internal/eval/verifier/verifier.go) (`Passed`, `Phase`, `ExitCode`, combined
`Stdout`/`Stderr`, `Duration`).

The `difficulty` band is a pure, reproducible function of three KG-derived signals — the
neighborhood node count (`involved_symbols`), edge count (`edge_count`), and the seed's
cyclomatic-complexity proxy (`cyclomatic_complexity`) — bucketed per-signal against the rubric v1
thresholds with hardest-signal-wins ([`internal/eval/difficulty.go`](../internal/eval/difficulty.go)).
Re-running a generator on the same graph state yields the same band and the same
`difficulty_signals` map.

### Toolchain pre-flight

Before it runs any `build_cmd`/`test_cmd`, the shared `BaseVerifier` **resolves the required
toolchain on PATH** ([`internal/eval/verifier/toolchain.go`](../internal/eval/verifier/toolchain.go)).
This is what keeps a missing interpreter/compiler from being scored as a code failure: a machine
with no `python3`, no `node`, or no `tsc` would otherwise fail the verify commands cryptically and
score the run *poor* — indistinguishable from the agent genuinely producing broken code.

The pre-flight resolves each command's leading executable with a **candidate list per binary**
(`exec.LookPath`, which applies Windows `PATHEXT` so `python`/`node`/`go` resolve
`python.exe`/`node.exe`/`go.exe`), and rewrites the argv to the resolved candidate so the actual
binary flows into execution:

| `TaskSpec` binary | Candidates tried, in order | Notes |
| --- | --- | --- |
| `python` | `python3`, then `python` | prefers `python3` (many machines ship only it); the resolved name replaces `python` in the executed command. |
| `tsc` | `tsc`, then `npx tsc` | falls back to running the local devDependency via `npx` when no global `tsc` is installed. |
| `node` / `go` | itself | resolved as-is. |

If **no** candidate resolves, verification does not run: the verifier returns a distinct
`*verifier.ToolchainError` (not the `*verifier.VerifyError` a started-but-failed step returns),
which the harness surfaces as a `harness: toolchain unavailable: …` abort. The message is
actionable, e.g.:

```
harness: toolchain unavailable: verifier: python toolchain unavailable: "python" not found on
PATH (tried: python3, python); install it or run a language whose toolchain is present
```

Because the run **aborts rather than scores**, a missing toolchain can never be recorded as a
*poor* outcome. A scored verify failure means the toolchain was present and the tests actually
ran and failed.

## Sandbox model

Each run executes in an isolated working tree provisioned by the v1 worktree sandbox
([`internal/eval/sandbox/`](../internal/eval/sandbox/), `sandbox.Sandbox` interface). `Provision`:

- checks out a **linked git worktree** detached at the source repo's current HEAD
  (recorded as `BaseCommit` for reproducibility) under `<run-dir>/worktree`, and
- creates a **scratch HOME** under `<run-dir>/home`, exported to the agent as
  `HOME` / `USERPROFILE` so the agent never touches the operator's real home.

**Concurrent isolation.** Two runs can never see each other's writes: each gets its own
worktree directory, and the run id is reserved by exclusively creating a sibling
`<run-id>.claim` file (`O_CREATE|O_EXCL`) before anything shared exists — a concurrent
provision that derives an identical id loses the create and regenerates. Run ids are
directory-name-safe on every CI OS (sanitized task id + colon-free UTC timestamp + random
suffix).

**Retention.** A provisioned working tree lingers no longer than `DefaultRetention` — **7 days**
(the R4 OQ6 default) — before `PruneStale` removes it. Pruning is wired to run **on the next
`da eval run`**: before it provisions its own sandbox, the run sweeps stale worktrees from
crashed prior runs (`pruneStaleSandbox` in [`commands/eval/run.go`](../commands/eval/run.go)).
The sweep is best-effort — a prune failure is warned to stderr, never aborts the run.

**Sidecars are never pruned.** Retention removes only the working tree, scratch home, and
sandbox marker; it preserves the run dir and every YAML sidecar in it (`taskspec.yaml`,
`eval-run.yaml`, and the `iteration-log/` records), so a run's score record and reproducibility
metadata survive indefinitely even after its working tree is reclaimed.

The `Sandbox` interface is the provider swap point: the v1 worktree implementation, a future
`DockerSandbox`, or a managed provider all sit behind it.

### Agent CLI and the isolated HOME

The sandbox exports its scratch directory as `HOME` / `USERPROFILE` so the agent never reads or
writes the operator's real home. This isolation is deliberate and correct — but it means the agent
CLI runs **with no credentials or config unless they live somewhere the isolated HOME can see**.
An unauthenticated agent CLI typically exits non-zero on the first turn; left unclassified, that
exit would flow into the score as a *poor* outcome, mistaking an **auth/config problem for poor
model quality** (dogfood #10).

**Operator requirement.** For a real (non-fake) `da eval run`, the chosen `--agent` CLI must be
(a) installed on PATH and (b) usable under an isolated HOME — e.g. authenticated via an
environment variable the sandbox passes through (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, a
`GH_TOKEN`), or via credentials the agent reads from a location independent of `$HOME`. If it is
not, the run cannot produce a meaningful sample.

**Distinguishable signal.** Two agent start failures are classified as a distinct
`*runner.AgentStartError` and surfaced by the harness as a `harness: agent did not run: …` abort
(instead of a scored `Result`):

| `Reason` | Owner | Trigger | Message gist |
| --- | --- | --- | --- |
| `unavailable` | runner ([`agentcheck.go`](../internal/eval/runner/agentcheck.go)) | the agent binary does not resolve on PATH (`exec.ErrNotFound`) — a pure launch fault | `… agent CLI "claude" not found on PATH — install it or choose an installed --agent` |
| `auth` | harness ([`authcheck.go`](../internal/eval/harness/authcheck.go)) | **all three** hold: non-zero exit, an auth-startup signature in the CLI's **stderr**, **and no solution produced** | `… agent auth/config failure under isolated HOME (exit N, matched "…") — the eval sandbox runs the agent under a credential-free HOME; authenticate the CLI or see docs/EVAL_HARNESS.md` |

The `auth` classification is deliberately narrow to avoid corrupting eval results on auth-related
tasks — a legitimate run that *produced a solution* about, say, an auth handler must be **scored**,
even if its diff or test output contains auth vocabulary. Three guards combine:

1. **No-solution gate (the robust guard).** The harness checks the sandbox working tree
   (`detectWorktreeChanges`, via go-git status). If the agent produced **any** change — a tracked
   edit or an untracked file — it *ran*: the run is scored, never auth-aborted, regardless of its
   output text. Only a run that changed nothing is a candidate for the auth abort. On any
   uncertainty (not a resolvable worktree, status error) the detector fails safe to "changed", so a
   run is scored rather than wrongly aborted.
2. **stderr-only scan.** Signatures are matched against the CLI's **stderr** only — an agent's
   solution and task output flow to stdout and the working tree, so solution content never feeds the
   match.
3. **Narrow signatures.** Only phrases that are unambiguously the CLI's own login/api-key startup
   failure are used (`not logged in`, `please run /login`, `claude login`, `codex login`,
   `gh auth login`, `invalid api key`, `no api key`, `missing api key`, `api key not set`). Bare
   `unauthorized` / `authentication failed` / `401` are **excluded** — they occur in legitimate
   solution and test output.

## How eval outcomes feed R1

An eval run is scored by the **same** production rubric as a real workflow iteration — there is
no eval-special scoring path (R4 invariant D4.4). The scoring bridge
([`internal/eval/scoringbridge/`](../internal/eval/scoringbridge/), `ScoreRun`) emits an
R1-shaped iteration record and scores it with `scoring.DefaultRubric()`, the same rubric
[`da score`](./OUTCOME_SCORING_RUBRIC.md) applies to production iterations.

Because an eval run is a single 1-shot sample (R4 OQ2), its iteration log always holds exactly
`iter-1.*`. The run-dir layout is:

```
.agents/eval/runs/<run-id>/
├── taskspec.yaml                     # the TaskSpec that ran               (store)
├── eval-run.yaml                     # run aggregate + score summary + R9/R10 (store)
└── iteration-log/
    ├── iter-1.yaml                   # R1-shaped iteration record          (score stage)
    └── iter-1.score.yaml             # explainable rubric score sidecar    (score stage)
```

`iter-1.yaml` round-trips through `scoring.ParseIterationRecord` and `iter-1.score.yaml` through
`scoring.PersistedScore`, so the eval run's iteration and score sidecar are directly loadable by
`da score iteration`. Point its `--iter-log-dir` at the run's iteration-log directory:

```
da score iteration 1 --iter-log-dir .agents/eval/runs/<run-id>/iteration-log
da score iteration 1 --iter-log-dir .agents/eval/runs/<run-id>/iteration-log --recompute
```

`--recompute` re-scores `iter-1.yaml` fresh under the current rubric; without it the persisted
`iter-1.score.yaml` is rendered as-is.

Some objective signals are **absent by construction** for an eval run: an ephemeral sandbox
worktree never lands on trunk (`landed`), eval tasks declare no `write_scope` (`scope`), and v1
captures no agent transcript window (the objective process checks). Absent is first-class — the
rubric renormalizes over the present signals — so an eval run is scored on what it actually
produced (verifier + test outcomes, correction pressure, token efficiency, and a human label if
one was attached), exactly as a production iteration with the same sparse telemetry would be.
See [`OUTCOME_SCORING_RUBRIC.md`](./OUTCOME_SCORING_RUBRIC.md) for the signal set and
combination, and the [R2 visibility contract](#r2-visibility-contract) below for the on-disk
join a dashboard performs over these files.

## Adding a fourth language

Adding a language is a new adapter, not a new engine. Python and TypeScript were both added this
way over the Go-first foundation; use [`gen/golang`](../internal/eval/gen/golang/generator.go) +
[`gen/python`](../internal/eval/gen/python/generator.go) and their verifier siblings as the
template. The concrete steps:

1. **Register the language value.** Add a `Language` constant (e.g. `LanguageRust Language =
   "rust"`) in [`internal/eval/taskspec.go`](../internal/eval/taskspec.go) and a matching arm in
   `Language.Valid()`.
2. **Add a generator profile.** Create `internal/eval/gen/rust/` exporting a `gencore.Profile`
   that fills in only what varies by language: the `Language`, the task-id `IDToken`,
   `ErrPrefix`/`DisplayName`, the no-test-edit prompt fragment, the `MustSatisfyCmd`, and the
   `TestFilePath` / `VerifyTarget` / `BuildCmd` / `TestCmd` functions. All generation control
   flow stays in `gencore` — do not copy the engine. `gencore.New` validates that the required
   function fields are non-nil at construction.
3. **Wire the generator into the CLI registry.** Add the profile to the `languageProfiles` slice
   in [`commands/eval/registry.go`](../commands/eval/registry.go) — one entry; the shared engine
   drives them all.
4. **Add a verifier adapter.** Create `internal/eval/verifier/rust/` with a thin wrapper that
   embeds `*verifier.BaseVerifier`: `func New() *RustVerifier { return
   &RustVerifier{verifier.NewBase(eval.LanguageRust)} }`. `Language()` and `Verify()` are
   promoted from the engine; no per-language run logic is added.
5. **Register the verifier factory.** Add a `commands/eval/verifiers_rust.go` file whose `init`
   calls `registerVerifier(func() verifier.Verifier { return rustverifier.New() })` — a new
   file per language keeps `run.go` language-agnostic and per-language deliveries disjoint.
6. **Update the language-list surfaces.** The `--language` help text and the `validateLanguage`
   error message enumerate the supported languages
   ([`commands/eval/registry.go`](../commands/eval/registry.go),
   [`commands/eval/eval.go`](../commands/eval/eval.go)); extend them so a typo still fails with
   an actionable list.

## R2 visibility contract

R2 is the dashboard that renders eval runs. It reads the run directory **as data** — it never
re-runs the harness or the scorer. This section is the contract R2 builds against: the run
directory layout, which stage owns each file, and the JOIN a dashboard performs to render a run
with its task metadata joined to its score breakdown.

A **frozen fixture run** that satisfies this contract ships at
[`internal/eval/store/testdata/r2contract-go-impl-pure-fn/`](../internal/eval/store/testdata/r2contract-go-impl-pure-fn/).
It was produced by the real store / scoring / scoringbridge / taskspec pipeline, so R2 can build
against its exact bytes without a harness run. The layout is pinned by
[`internal/eval/store/visibility_contract_test.go`](../internal/eval/store/visibility_contract_test.go),
so a change that breaks the contract breaks that test.

### Run directory layout

Each completed run is a directory `<repo-root>/.agents/eval/runs/<run-id>/`
(the path `store.RunDir` returns). It is **incrementally assembled** — no single stage owns the
whole directory; each stage writes only the files it owns, each with a per-file atomic write:

| File | Owner stage | Type it round-trips through |
| --- | --- | --- |
| `taskspec.yaml` | store (`WriteEvalRun`) | `eval.TaskSpec` — `eval.ParseTaskSpec` |
| `eval-run.yaml` | store (`WriteEvalRun`) | `store.PersistedEvalRun` |
| `iteration-log/iter-1.yaml` | score stage (`scoringbridge.ScoreRun`) | `scoring.IterationRecord` — `scoring.ParseIterationRecord` |
| `iteration-log/iter-1.score.yaml` | score stage (`scoring.WriteIterationScore`) | `scoring.PersistedScore` |

v1 scoring is 1-shot, so the iteration log always holds exactly `iter-1.*` (a run is a single
sample).

- **`taskspec.yaml`** is the full task: `task_id`, `language`, `difficulty`,
  `difficulty_signals`, `generated_from` (incl. `template_id`, the prompt template id), the full
  `prompt`, `solution_artifacts`, and the `verification` commands. It is the source of truth for
  task metadata.
- **`eval-run.yaml`** is the run aggregate + R9/R10 reproducibility block. It **denormalizes**
  the task metadata a run-list view needs (`task_id`, `language`, `difficulty`) and carries a
  **score summary** (`score.value` / `band` / `scored` / `rubric_version`) plus the `agent`
  identity (`harness`, `model`, `session_id`, `prompt_digest`, `output_digest`, durations) and
  the `verify` outcome (`passed`, `phase`, `exit_code`, `duration`). Its purpose is to let R2
  render a run row **without opening all four files**.
- **`iteration-log/iter-1.yaml`** is the R1-shaped iteration record (schema_version 2). Its
  `wave` field carries the **run id** (an eval run belongs to no plan), which is how the score
  joins back to the run.
- **`iteration-log/iter-1.score.yaml`** is the full explainable score: `value`, `band`,
  `scored`, `rubric_version`, and the per-signal `breakdown` (one row per rubric dimension, with
  `signal`, `label`, `present`, `sub_score`, `detail`, `nominal_weight`, `effective_weight`,
  `contribution`). It is the source of truth for the **score dimensions** — see
  [Score dimensions](#score-dimensions) for the per-field contract.

### Score dimensions

`iteration-log/iter-1.score.yaml:breakdown[]` carries one row per rubric signal, in the rubric's
declared order. Each row emits exactly these fields, in this order — the set written by
`scoring.PersistedContribution` — so a dashboard renders them verbatim without re-running the
scorer:

| Field | Meaning |
| --- | --- |
| `signal` | Stable rubric signal id the row scores. |
| `label` | Human-readable name of the signal. |
| `present` | Whether the signal was observed on this run; an absent signal still gets a row but votes nothing (`effective_weight` and `contribution` are 0). |
| `sub_score` | The signal's score in `[0, 1]`; meaningful only when `present` is true. |
| `detail` | Short human-readable note on what produced the value; omitted from the YAML when empty. |
| `nominal_weight` | The signal's configured weight in the rubric, before absence renormalization. |
| `effective_weight` | The weight actually applied after renormalizing across the present signals (`nominal_weight ÷ Σ present nominal weights`); 0 when the signal is absent. |
| `contribution` | The signal's share of the run `value` (`effective_weight × sub_score`); the present rows sum exactly to `value`. |

### The join a dashboard performs

To render one run with its task metadata joined to its score breakdown, R2 joins the files on
three keys:

1. **Run-identity join** — `eval-run.yaml:run_id == <dir name> == iter-1.yaml:wave`. This ties
   the run aggregate to its iteration record and locates the run directory. The score sidecar
   carries no run id of its own; it is addressed by **directory containment plus iteration
   number** (`iteration-log/iter-1.score.yaml`, `iteration == 1`).
2. **Task-metadata join** — `eval-run.yaml:task_id == taskspec.yaml:task_id == iter-1.yaml:task_id`.
   This ties the denormalized run metadata to the canonical `TaskSpec`.
3. **Score-summary join** — `eval-run.yaml:score.*` is a summary of `iteration-log/iter-1.score.yaml`;
   they must agree on `value`, `band`, `scored`, and `rubric_version`. R2 renders the summary
   from `eval-run.yaml` and drills into `iter-1.score.yaml` for the breakdown.

### Field provenance

For every field R2 renders, the authoritative source and the join that reaches it:

| Rendered field | Provenance (file → field) | Join used |
| --- | --- | --- |
| run id | `eval-run.yaml:run_id` (== dir name == `iter-1.yaml:wave`) | run-identity (root) |
| language | `eval-run.yaml:language` (list view) — canonical `taskspec.yaml:language` | task-metadata |
| difficulty | `eval-run.yaml:difficulty` (list view) — canonical `taskspec.yaml:difficulty` | task-metadata |
| prompt id | `taskspec.yaml:generated_from.template_id` | task-metadata |
| prompt integrity | `eval-run.yaml:agent.prompt_digest` == `sha256(taskspec.yaml:prompt)` | task-metadata |
| score value / band | `eval-run.yaml:score.value` / `score.band` (summary) — full `iter-1.score.yaml:value` / `band` | score-summary |
| rubric version | `eval-run.yaml:score.rubric_version` == `iter-1.score.yaml:rubric_version` | score-summary |
| score dimensions | `iter-1.score.yaml:breakdown[]` (`signal`, `label`, `present`, `sub_score`, `detail`, `nominal_weight`, `effective_weight`, `contribution` — see [Score dimensions](#score-dimensions)) | run dir + iteration |
| verify outcome | `eval-run.yaml:verify.passed` / `phase` / `exit_code` / `duration` | run-identity |
| agent identity | `eval-run.yaml:agent.*` (also `iter-1.yaml:agent.*`) | run-identity |

`language` and `difficulty` appear on **both** `eval-run.yaml` (denormalized for the list view)
and `taskspec.yaml` (canonical); the contract test asserts they agree. The **prompt id** is the
human-readable `template_id` on `taskspec.yaml`; its integrity is pinned by
`eval-run.yaml:agent.prompt_digest`, which is `sha256` over the exact `taskspec.yaml:prompt`
bytes — so a dashboard can prove the rendered prompt is the one the agent actually ran.
