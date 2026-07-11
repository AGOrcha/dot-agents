# `da` recipe scripts — Product Contract

**Status:** shipped — `da run <file>` is live (`commands/run.go`, registered `commands/root.go:222`);
all done criteria (§4) hold. Plan: `.agents/workflow/plans/da-recipe-scripts/` (status: completed).
**Written:** 2026-07-03
**Plan:** da-recipe-scripts
**Graduated from:** `.agents/proposals/da-shebang-scriptability.md`

---

## 1. Problem statement

The meta-loop issues many `da` invocations across its lifecycle (main loop, orchestrator,
loop-workers) that are *script-like* sequences — e.g. the iteration-close chain
(verify → checkpoint → merge-back|advance) and the gate-flush. Today those sequences are
either **bash** (non-portable — the meta-loop runs on Windows too, and bash-isms have
repeatedly caused hook/`--no-verify` pain) or **agent-improvised** (re-derived each pass,
introducing variance). yq's shebang acceptance test (mikefarah/yq #1851) shows the ergonomic
target: a `.yq` file whose first line is `#!/usr/bin/env -S yq …` is directly executable.

This spec defines a first-class, **deterministic, cross-platform** `da` recipe format so those
mechanical sequences become versionable artifacts executed identically by every actor.

## 2. Decisions (with rationale)

- **D1 — Entrypoint.** Add `da run <file>`. Shebang form: `#!/usr/bin/env -S da run`, so a
  chmod-+x `.da` file is directly executable. (`da` is a Cobra subcommand multiplexer, not an
  expression evaluator — so a recipe is a *sequence of `da` steps*, not one expression. This is
  the key departure from yq.)
- **D2 — Format: line-oriented.** One `da` subcommand invocation per line; a leading `#!`
  shebang, `#` comment lines, and blank lines are ignored. Rationale: simplest thing that
  covers the mechanical-sequence use case; matches yq #1851. Structured/YAML recipes are
  deferred (§6).
- **D3 — Boundary vs skills.** Recipes are **mechanical / deterministic only** — no judgment,
  no branching on outcomes. Sequences that need agent judgment stay **skills**; a skill MAY
  call a recipe for its mechanical spine. If a proposed recipe wants a conditional/loop over
  outcomes, it belongs in a skill, not a recipe.
  Recipes are the deterministic floor of orchestration; the Workflow engine is the programmable
  ceiling. If a recipe wants to become programmable, it wants to be a workflow.
- **D4 — Error handling: fail-fast.** A non-zero step aborts the run, reporting the failing
  step index + source line, with a non-zero process exit. Rationale: recipes mutate workflow
  state; continuing past a failed step risks corrupt state. (A future opt-in continue-on-error
  is deferred.)
- **D5 — In-process dispatch, no shell.** Each effective line is tokenized with minimal
  shell-like quoting (a quoted-field splitter, NOT a shell) and dispatched through the existing
  `da` root command in-process. No `sh -c`. This is what makes recipes cross-platform — `da`
  already owns platform handling (`internal/platform`).
- **D6 — Minimal variables (v1).** Environment-variable substitution only (`$VAR` / `${VAR}`)
  in recipe lines. Positional args / templating deferred.

## 3. Requirements (behavioral)

- **R1.** `da run FILE` executes each effective line as `da <args>` in file order.
- **R2.** A first-line `#!` shebang is ignored; lines beginning `#` and blank lines are ignored.
- **R3.** Fail-fast: the first non-zero step aborts with a clear `step N ("<line>") failed: …`
  message and a non-zero exit code; subsequent steps do not run.
- **R4.** Identical behavior on macOS, Linux, and Windows — no shell dependency.
- **R5.** An executable `.da` file carrying the D1 shebang runs directly (`./recipe.da`).
- **R6.** Environment-variable substitution is applied to each line before tokenization.

## 4. Done criteria (verifiable)

- Table-driven unit tests: multi-step recipe executes in order; a failing step stops the run
  with the R3 message + non-zero exit; comments/blank/shebang lines are skipped; env
  substitution works.
- A shebang **acceptance test** (mirroring yq's) proves a chmod-+x `.da` file executes; runs on
  every target OS in CI (or a documented cross-platform equivalent where a POSIX shebang can't).
- Dogfood: at least one real meta-loop mechanical sequence is expressed as a shipped `.da`
  recipe and exercised.

## 5. Open questions — resolved

- **OQ1 (resolved).** Env-substitution only (`$VAR`/`${VAR}`) for v1; no positional args. The p5
  dogfoods (`checkpoint-advance.da`, `scaffold-plan.da`) both needed only named env vars, so the
  recommendation stood without amendment.
- **OQ2 (resolved).** Shipped recipes live under `src/share/recipes/*.da`, executable (chmod
  0755), one real subcommand invocation per line.
- **OQ3 (resolved).** Recipes are fixed-shape `da`-command sequences (D2/D3: no loops, no
  branching on outcomes) — distinct from the JS `Workflow` engine's agent fan-out. A recipe may
  be called from a skill for its mechanical spine; it never replaces the judgment a skill or the
  Workflow engine applies. No overlap.

## 6. Deferred (out of scope for v1)

- YAML/structured recipes with conditionals, loops, or `on_error` policies (→ skill territory
  per D3).
- Loops over dynamically-discovered sets (→ skill).
- Remote/URL recipes; parallel step execution.

## 7. Relationship to other artifacts

Graduates the `da-shebang-scriptability` proposal. Distinct from **skills** (judgment
sequences) and the **Workflow** engine (agent fan-out); sits as the mechanical-recipe layer
below skills. Done criteria trace to this spec; the plan resolves OQ1–OQ3 in its task notes.

## 8. Amendment (2026-07-11): mechanical loops + shallow data-driven conditionals

D3/OQ3/§6 originally barred **all** loops and branching ("Loops over
dynamically-discovered sets → skill"). Practice showed the flat-only form can't
express a common *mechanical* need — "ingest every file in a folder" — without
pushing an otherwise-deterministic sequence into a skill. This amendment relaxes
the boundary to admit **mechanical** control flow while keeping the D3 principle
that matters intact.

- **D7 — Mechanical `for … in <glob> … end` loop.** A recipe may iterate a fixed
  body over a **statically-discovered** set: the loop header `for <VAR> in
  <PATTERN>` env-expands `<PATTERN>` and resolves it with `filepath.Glob` **once**
  at loop entry, binds `<VAR>` per match (sorted for cross-OS determinism, R4),
  and runs the body per match. An empty match set runs the body zero times (a
  folder with no matching files is a clean no-op, not an error). The iteration set
  is filesystem STATE captured up front — it cannot grow based on what the body
  does, so the loop is bounded and deterministic.
- **D8 — Shallow data-driven `if [not] <pred> <arg> … end` conditional.** A recipe
  may guard a body on a **data/state** predicate evaluated before dispatch:
  `exists <glob>` (≥1 path matches) or `set <NAME…>` (one or more space-separated
  names, all non-empty); `not`
  negates. This is not a general expression language and, critically, there is **no
  predicate over a command's exit status**.
- **D9 — The preserved D3 line: no branching on OUTCOMES.** The thing D3 forbids —
  and still forbids — is reacting to a *step's result* (retry, if-failed-then,
  continue-on-error). That is genuine judgment and stays **skill / Workflow-engine**
  territory; recipes remain fail-fast (D4). D7/D8 branch only on *inputs* (the
  filesystem/env before the step runs), never on *results*.
- **D10 — Strict depth cap.** `for` and `if` blocks share a **maximum nesting depth
  of 2** (`maxBlockNesting`, `commands/run.go`). Deeper nesting is rejected at parse
  time (before any dispatch) with a `depth cap` error. Rationale: a 1–2 level cap
  keeps a recipe readable and its cost obviously bounded; anything deeper is a
  signal the work wants a skill, not a recipe.

**Grammar (line-oriented, extends D2):** `for <VAR> in <PATTERN>` / `if [not]
exists <PATTERN>` / `if [not] set <NAME…>` (all names non-empty) open a block; a lone `end` closes the
nearest open block. Balanced open/`end` pairing and the depth cap are validated
before dispatch; a structural error aborts with no side effects. Malformed headers
(e.g. `for` with no `in`) are not recognized as blocks — they fall through to
normal dispatch and fail loudly there. Everything else (shebang, `#` comments,
blank lines, env-substitution, quote-blind tokenization, fail-fast, in-process
no-shell dispatch) is unchanged from D1–D6.

**§6 deferrals updated:** "loops over dynamically-discovered sets" and
"conditionals" are **no longer deferred** for the *mechanical* case above.
Outcome-branching, `on_error` policies, parallel steps, and general expressions
remain deferred / skill territory. OQ3's "no loops, no branching on outcomes" now
reads: *no branching on outcomes* (mechanical loops + input-predicated conditionals
are in).

**Tests:** `commands/run_test.go` — loop iteration order/empty-set/var-restore/
fail-fast, `if exists`/`set`/`not` gating, nesting-cap + unterminated + dangling-`end`
parse errors. **Dogfood:** `.agents/recipes/kg-ingest.da` (folder ingestion).
