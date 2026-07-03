# `da` recipe scripts — Product Contract

**Status:** draft
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

## 5. Open questions

- **OQ1.** v1 variable support — env-substitution only (recommended), or also positional args
  (`$1`, `$@`)? Recommendation: env-only for v1; add positionals only if the dogfood needs them.
- **OQ2.** Where do shipped recipes live and what is the convention (`.da` extension; a
  `recipes/` or `src/share/recipes/` dir; scaffolded into user homes like skills)?
- **OQ3.** Relationship to the JS `Workflow` orchestration engine: recipes are *`da`-command*
  sequences (mechanical); workflows are *agent fan-out*. They are distinct — confirm no
  overlap is intended and document the boundary.

## 6. Deferred (out of scope for v1)

- YAML/structured recipes with conditionals, loops, or `on_error` policies (→ skill territory
  per D3).
- Loops over dynamically-discovered sets (→ skill).
- Remote/URL recipes; parallel step execution.

## 7. Relationship to other artifacts

Graduates the `da-shebang-scriptability` proposal. Distinct from **skills** (judgment
sequences) and the **Workflow** engine (agent fan-out); sits as the mechanical-recipe layer
below skills. Done criteria trace to this spec; the plan resolves OQ1–OQ3 in its task notes.
