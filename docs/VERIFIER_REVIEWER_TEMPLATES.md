# Verifier & reviewer prompt templates

Verifier and reviewer prompts are **layered**, not single files. A worker's effective prompt is the
merge of two independent axes.

## Two axes

**Composition (what the instruction covers).** A shared **base** (one for verifiers, one for
reviewers) teaches the generic contract — the `da workflow verify record` surface, the result/decision
schemas, the role boundary, the evidence taxonomy, cold-start. **Per-type** templates add what is
specific to a verification kind (`unit`, `cli-runner`, …) or a reviewer lens (`architecture-standards`,
…). A **repo overlay** adds the project's concrete commands, paths, and rule set.

**Scope (whose version wins).** Each template resolves through the config-v2 scope ladder
(`config-distribution-model` §15 D1/D9), low → high:

```
product → user-local → org → team → repo-imported → repo-local committed → project-local overlay → runtime
```

- **product** = the baseline shipped in the starter, materialized to the shared home
  (`~/.agents/prompts/{verifiers,reviewers}/`).
- **repo-local committed** = the project's `.agents/prompts/{verifiers,reviewers}/*.project.md`.
- org / team / user are first-class rungs in between — there is no "global vs project" binary; the
  project is one rung of several.

The two axes are orthogonal: composition says *which* templates compose; scope says *whose version*
of each wins.

## How a profile composes

`verifier_profiles.<type>.prompt_files` and `reviewer_profiles.<lens>.prompt_files` list the
composition **base-first**:

```json
"verifier_profiles": {
  "cli-runner": {
    "prompt_files": [
      "verifiers/verifier.base.md",
      "verifiers/cli-runner.md",
      "verifiers/cli-runner.project.md"
    ]
  }
},
"reviewer_profiles": {
  "architecture-standards": {
    "prompt_files": [
      "reviewers/reviewer.base.md",
      "reviewers/architecture-standards.md",
      "reviewers/architecture-standards.project.md"
    ]
  }
}
```

`reviewer_profiles` is symmetric to `verifier_profiles`: same `{label, prompt_files}` shape, same
`CategoryMapMerge` so a higher scope deep-merges over the base.

Each entry resolves **per file** across the scope search path — repo-local (`.agents/prompts/`) over
shared-home (`~/.agents/prompts/`). A project overrides the product base by placing a same-named file
under `.agents/prompts/`; otherwise the entry resolves to the materialized starter baseline. The list
order (base → per-type → overlay) is preserved.

## Inspecting the composition

```
$ da workflow resolve-prompt --kind verifier --slug cli-runner
Composed prompt
  kind    : verifier
  slug    : cli-runner
  matched : true

  composition (base-first):
    1. verifiers/verifier.base.md       [shared-home -> ~/.agents/prompts/verifiers/verifier.base.md]
    2. verifiers/cli-runner.md          [shared-home -> ~/.agents/prompts/verifiers/cli-runner.md]
    3. verifiers/cli-runner.project.md  [repo-local  -> .agents/prompts/verifiers/cli-runner.project.md]
```

`--kind reviewer --slug <lens>` does the same for reviewers. `--json` emits the structured view. This
is the seam the orchestrator/ISP calls when dispatching a verifier or reviewer.

## Status

Phase 1 (proof) ships both bases and the composition mechanism, wired for `unit`, `cli-runner`, and
the `architecture-standards` lens. Phase 2 migrates the remaining verifiers (api, ui-e2e, batch,
streaming, schema-check, citation-check, task-schedule) and lenses (acceptance-invariants,
adversarial) onto the same base + per-type model. See
`.agents/workflow/specs/verifier-reviewer-template-architecture/design.md`.
