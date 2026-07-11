# New app_type profiles — DRAFT for sign-off (do NOT author to master yet)

Concrete `.agentsrc.json` `execution_profile.by_app_type` additions + the new
`stage_profiles` slugs (each needs a prompt `.md`). Authoring these = the `meta`
task, gated on the 4 decisions below. Existing (shipped) slugs reused where noted;
NEW slugs marked `*NEW*` (need `.agents/prompts/{verifiers,reviewers}/<slug>.md`).

## execution_profile.by_app_type (draft)
```jsonc
"web": {                       // go http service + ui + ui-e2e (r2 dashboard)
  "topology": { "executors": 1, "verifiers_per_executor": 2, "reviewers": "per_verifier",
                "verifier_sequence": ["unit", "integration*NEW*", "ui-e2e*NEW*"] },
  "lenses": { "lens_set": ["architecture-standards","acceptance-invariants","adversarial",
                           "cross-harness-adversarial","web-a11y*NEW?*"],
              "lens_concurrency": "gated" }
},
"docs/web": {                  // docs site
  "topology": { "executors": 1, "verifiers_per_executor": 1, "reviewers": "per_executor",
                "verifier_sequence": ["schema-check","citation-check","link-check*NEW*","build-site*NEW*"] },
  "lenses": { "lens_set": ["architecture-standards","acceptance-invariants"],
              "lens_concurrency": "parallel" }
},
"meta": {                      // self-refinement loop / pipeline + agent/subagent defs
  "topology": { "executors": 1, "verifiers_per_executor": 1, "reviewers": "per_executor",
                "verifier_sequence": ["schema-check","pipeline-lint*NEW*"] },
  "lenses": { "lens_set": ["architecture-standards","acceptance-invariants","adversarial"],
              "lens_concurrency": "gated" }   // reviewed for LOOP-SAFETY
},
"daemon": {                    // COMING bg service (high-freq comms) — STUB pending the service
  "topology": { "verifier_sequence": ["unit","race*NEW*","integration*NEW*","load-soak*NEW*","comms-contract*NEW*"] },
  "lenses": { "lens_set": ["architecture-standards","adversarial","cross-harness-adversarial","concurrency-safety*NEW*"],
              "lens_concurrency": "gated" }
}
```
Reused shipped slugs: `unit`, `cli-runner`, `schema-check`, `citation-check`,
`task-schedule`, `architecture-standards`, `acceptance-invariants`, `adversarial`,
`cross-harness-adversarial`.

## NEW stage_profiles to author (each = a prompt file)
- verifier: `integration` (http-handler/API), `ui-e2e` (playwright/browser), `link-check`,
  `build-site`, `pipeline-lint` (agent-def/DSL validate + dry-run), `race`, `load-soak`,
  `comms-contract`.
- reviewer: `web-a11y` (or `design`), `concurrency-safety`.

## DECISIONS NEEDED (blocking authoring)
1. Confirm/adjust each verifier_sequence + lens_set above.
2. `web` a11y/design lens — include (`web-a11y`) or not?
3. `meta` — is `pipeline-lint` the right verifier? lens_set for loop-safety OK?
4. `daemon` — stub now (as above) or defer entirely until the service exists?

## After sign-off
Author the agreed `by_app_type` entries + `stage_profiles` + prompt files (meta task → PR),
then launch the profile-driven loop (`profile-driven.swarm.yaml`) per task.
