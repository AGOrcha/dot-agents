# Multi-target families need a shared core, not N parallel copies

## Pattern

When a plan produces a **family** of near-identical implementations across
targets — per-language code generators, per-language verifiers, per-platform
adapters, per-backend drivers — do NOT scope it as N independent tasks that each
reimplement the shared shape. Design a **shared core + thin per-target adapters**
up front, and scope the tasks as (1) build the core, (2..N) add each thin adapter.

The r4 eval generator family was built the wrong way: `generator-go`,
`generator-typescript`, `generator-python` were three independent tasks. Each
produced a ~400-line `generator.go` with ~15 identical scaffolding functions
(New/Register/Generate/synthesize/findSeed/pickMatchingSeed/deriveFromSeed/
buildSpec/solutionArtifactPath/testFilePath/taskID/templateShort/sanitizeQN/
renderPrompt). Only the language-specifics differed (file extensions, test-file
naming, prompt text). Result: `generator-go` merged first, then the ts/py PRs
hit `new_duplicated_lines_density = 18.6% / 18.7%` (threshold ≤3%; the ts file
was 57.9% duplicated vs the merged go file) and could not merge. Owner ruling:
"the duplication is a sign of bad design and impl." Fix = extract a shared
`gencore` engine parameterized by a `LanguageProfile`, refactor all three onto
it — a whole rework cycle that a correct plan shape would have avoided.

## Why this bites specifically here

The repo's Coverage gate enforces `new_duplicated_lines_density < 3%` on new
code, measured **against the whole project** (see [[sonarcloud-gate-mechanics]]).
So the FIRST target in a duplicated family merges fine (nothing to duplicate
yet), and every subsequent target is blocked because it duplicates the merged
first one. The gate makes "N parallel copies" not just ugly but literally
un-mergeable past the first — you cannot ship the family without the shared core.

## Rule

1. At PLAN time, when you see "do X for {go, typescript, python}" or any
   target-parameterized fan-out, ask: *how much of X is the same across targets?*
   If the answer is "most of it," the plan is: **core task first** (the engine +
   the target-profile seam), then **thin-adapter tasks** that depend on it. Never
   N full independent implementations of the same shape.
2. The adapter seam is the deliverable of the core task: a `Profile`/`Adapter`
   interface (or struct-of-funcs) supplying exactly the per-target specifics.
   Each adapter task is then small and genuinely non-duplicative.
3. If a duplicated family already shipped its first target, the fix is to extract
   the core and refactor the merged target ONTO it (touches previously-gated
   code — preserve its tests as the behavior contract), landing the core +
   adapters as ONE coherent PR that supersedes the per-target PRs.

## How to apply

- Reviewing a plan/spec for a family: count the target-parameterized tasks; if
  ≥2 and they share >~30% structure, restructure into core + adapters before
  fanout. Cheaper than the rework cycle + N-1 blocked PRs.
- Reviewing a delivered PR in a family: check `new_duplicated_lines_density` via
  the SonarCloud PR API (`/api/qualitygates/project_status?pullRequest=<n>`),
  not just the per-OS Test jobs — the dup gate lives in the Coverage gate.
- The shared-core extraction is design-sensitive and touches merged code: brief
  it to an opus worker, keep the first target's test suite as the invariant.

## Cross-references

- `[[const-extraction-triggers-cpd-on-tables]]` — the sibling CPD failure mode; both are "Sonar block-duplication blocks a plausible-looking layout."
- `[[consolidate-vestigial-siblings-on-rename]]` — "generalize this primitive" usually means "consolidate the duplication around it"; same instinct at the primitive scale.
- `[[single-source-of-truth-across-specs-and-plans]]` — the docs/spec analogue: one canonical source, others point to it.
- `[[sonarcloud-gate-mechanics]]` — how the dup/coverage gate actually evaluates new code.
