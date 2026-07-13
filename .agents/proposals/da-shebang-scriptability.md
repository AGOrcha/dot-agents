# Proposal: `da` recipe scripts (shebang-executable command sequences)

Status: shipped · Created: 2026-07-03 · Origin: owner idea (yq shebang acceptance test)

> **Shipped** as `da run` (`commands/run.go`) — shebang-executable, in-process, cross-platform da-command recipes. Canonical plan: `.agents/workflow/plans/da-recipe-scripts/` (core `da run` = p1 done; fail-fast messaging / env-substitution / cross-platform acceptance test / dogfood recipe tracked there as p2–p5).

## Idea

yq ships a shebang acceptance test (see mikefarah/yq #1851): a `.yq` file whose first
line is `#!/usr/bin/env -S yq --null-input --from-file` is directly executable — yq
ignores the shebang and evaluates the rest as its filter. Owner observation: the
meta-loop issues **many** `da` invocations across the lifecycle (main loop, orchestrator,
loop-workers) that are "script-like" sequences. A first-class `da` script format could
make those sequences reusable, deterministic, and cross-platform.

## Key distinction from yq (don't copy it literally)

yq is an **expression evaluator** — one filter language, one expression per file. `da` is
a **Cobra subcommand multiplexer**. So a "da script" is NOT one expression; it is a
**sequence of `da` subcommand steps** (a recipe). The natural shebang is therefore a
`run`-style entrypoint:

```
#!/usr/bin/env -S da run
# gate-flush.da — recipe, not an expression
workflow eligible --plan {{plan}}
# for each ready PR: gate, then on SOUND:
#   pr merge --admin ; workflow advance <task> --status completed
```

`da run <recipe>` parses the recipe (ignoring a leading `#!` line), executes each step as
if typed, with explicit error handling / stop-on-fail semantics.

## Why this is worth it for the meta-loop

1. **Cross-platform.** The meta-loop runs on macOS **and Windows**. Repeated sequences
   are currently either bash (non-portable, the source of past `--no-verify`/hook pain) or
   agent-improvised. A `da`-native recipe removes bash-isms — `da` already owns platform
   handling (`internal/platform`).
2. **Determinism / less variance.** Sequences like the iteration-close chain
   (verify → checkpoint → merge-back|advance) and the gate-flush are re-improvised by
   agents each pass. A recipe executes the same way every time — the reliability win the
   meta-loop keeps needing.
3. **First-class, versionable artifacts.** A `.da` recipe is reviewable, diff-able, and
   ships with the repo — unlike an agent's in-context sequence.
4. **Uniform across actors.** Main loop, orchestrator, and workers invoke the same recipe
   instead of each re-deriving the steps.

## Boundary vs skills (resolve before building)

This overlaps with **skills** (markdown action sequences) and the **workflow CLI**. The
clean split:
- **Skills** = sequences that need *agent judgment* (decide, adapt, gate).
- **`da` recipes** = *mechanical, deterministic* sequences (no judgment) — a layer BELOW
  skills. A skill may *call* a recipe for its mechanical spine.
If a proposed recipe needs a branch on judgment, it belongs in a skill, not a recipe.

## Feasibility

Low. Add `da run <file>` (Cobra) that: strips a leading `#!` line, parses a recipe
(line-oriented `da <args>`, or a small YAML `steps:` list with `on_error`), and dispatches
each step through the existing command tree. `#!/usr/bin/env -S da run` makes files
executable. yq #1851 is the precedent.

## Open questions

- Recipe format: line-oriented (simplest, matches yq) vs YAML steps (error-handling,
  conditionals, variables)? Start line-oriented; add structure only if needed.
- Variables/params: `{{plan}}`-style substitution, or env only?
- Does it need control flow (loops over eligible tasks)? If yes, it's drifting toward a
  skill — reconsider the boundary.
- Relationship to `Workflow` orchestration scripts (the JS workflow engine): recipes are
  for *da-command* sequences, not agent fan-out — keep them distinct.

## Recommendation

Capture now; do not build mid-flush. Good ergonomics + reliability idea. Before a plan,
resolve the skills/recipe boundary and pick the minimal recipe format (line-oriented).
