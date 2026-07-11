# App_type profiles — FINALIZED spec (user-signed-off 2026-07-11)

Authoritative spec for authoring the new app_type profiles into `.agentsrc.json`
(`execution_profile.by_app_type` + `stage_profiles`) + the new prompt files under
`.agents/prompts/{verifiers,reviewers}/<slug>.md`. Reused shipped slugs are NOT
re-authored. `lens_concurrency: gated` = sequential-with-defensive-skip in the
profile-driven pipeline.

## web  (go http service + UI + ui-e2e; e.g. r2 dashboard)
- verifier_sequence: `unit`, `integration`*, `ui-e2e`*
- lens_set: `architecture-standards`, `acceptance-invariants`, `adversarial`,
  `cross-harness-adversarial`, `web-a11y`* ; gated
- **RAISED DELIVERY BAR (user):** verification MUST *produce and link PROOF artifacts*
  that it works as expected — **screenshots + video** of the ui-e2e run, plus the usual
  test/coverage evidence. The `ui-e2e` verifier captures them; the gate stage attaches/links
  them in the PR (proof-of-delivery), stored on the coordination ref
  (`.agents/worktrees/_state/active/proof/<task>/`) and linked from the PR body.

## docs/web  (docs site)
- verifier_sequence: `schema-check`, `citation-check`, `link-check`*, `build-site`*
- lens_set: `architecture-standards`, `acceptance-invariants` ; parallel

## meta  (self-improvement surface — BROAD, per user)
Covers: run/session **evaluation techniques**, **skills + tools**, **analysis**,
**product refinement**, **hypothesis-testing runs + ideation**, **agentic-config
improvements**, **KG content refinement + maintenance**, and the loop/pipeline/agent
+ subagent definitions themselves.
- verifier_sequence: `schema-check` (defs/configs/skills/tools valid),
  `pipeline-lint`* (agent/subagent + pipeline/DSL + config dry-run/no-wedge),
  `eval-fidelity`* (for eval/hypothesis/analysis runs: faithful inputs + real control +
  no hidden losses — the prototype-experiment-fidelity-gate lesson)
- lens_set: `architecture-standards`, `acceptance-invariants`, `adversarial`,
  `scientific-method-fidelity`* (gates experiment-backed/eval/ideation claims) ; gated
- Note: pure ideation content may still use the `ideation` app_type; `meta` is the
  umbrella for the reflexive/self-improvement + config/eval/KG-maintenance work.

## daemon  (bg service — STUB NOW, per user: HBHF comms + auth-proxy + secure handling + KG)
- verifier_sequence: `unit`, `race`*, `integration`*, `comms-contract`* (HBHF protocol/
  backpressure/framing), `auth-proxy-security`* (auth proxy + secret/secure handling),
  `kg-integration`* (KG read/write correctness under the daemon), `load-soak`*
- lens_set: `architecture-standards`, `adversarial`, `cross-harness-adversarial`,
  `concurrency-safety`*, `security`* (auth-proxy + secure-handling review) ; gated

## NEW stage_profiles to author (slug -> prompt file)
verifiers: `integration`, `ui-e2e`, `link-check`, `build-site`, `pipeline-lint`,
`eval-fidelity`, `race`, `comms-contract`, `auth-proxy-security`, `kg-integration`,
`load-soak`.
reviewers: `web-a11y`, `scientific-method-fidelity`, `concurrency-safety`, `security`.
Each composes the base (`verifiers/verifier.base.md` / `reviewers/reviewer.base.md`) +
a focused slug overlay. Reused (no authoring): `unit`, `cli-runner`, `schema-check`,
`citation-check`, `task-schedule`, `architecture-standards`, `acceptance-invariants`,
`adversarial`, `cross-harness-adversarial`.

## Authoring plan (bootstrap — done directly, then loop handles future meta work)
1. One writer authors `.agentsrc.json` `by_app_type` (web/docs-web/meta/daemon) +
   `stage_profiles` entries (coherent single file).
2. Parallel writers author the ~15 new prompt files (base-composing overlays).
3. Validate: `da config relevance --filter topology|lenses --app-type <t> --json` resolves
   each; `da config verify`; go build/test unaffected. PR -> master.
