# Design: `config relevance` — workflow execution profiles (config-v2-native)

**Status:** IMPLEMENTED 2026-06-08 — shipped as `internal/config/execution_profile.go` (relevance/topology/lenses facets) + `da config relevance` / `relevance recompute`. Was: proposal (not yet graduated to a spec).
**Depends on:** `config-distribution-model` §15 (the v2 coherence model — scopes / sources /
units / lock). This rides on the §15 units model as mergeable layers, not a parallel mechanism.
**Feeds on:** `r1-outcome-scoring` (iter-log + `da score` telemetry), `r1-5-hook-enforcement-telemetry`.
**Consolidates:** the routing logic today scattered across `app_type_verifier_map`,
`lens_routing.by_app_type` / `lens_set` / `lens_concurrency`, and `max_parallel_tasks`.

---

## 1. Problem & goal

Two problems, one surface:

1. **Noise.** Every session resolves ~25 skills + ~7 agents across scopes; for a given task most
   are irrelevant (`article-extract`/`playwright` on a Go-CLI task), a few are core, and some
   genuinely-useful ones are *missing*.
2. **One-size execution.** Today the workflow execution shape (how many executors, how many
   verifiers/reviewers per, which lenses, what concurrency) is fixed-ish, but real work is
   **mixed**: ideation/exploration wants divergence and light verification; execution/throughput
   wants high verifier+reviewer fan-out with gated quality lenses. Each *task in a plan* can want a
   different shape.

**Goal:** `da config relevance` — one **sliceable, scope-mergeable, per-task-tunable resolution
surface** for a task's **execution profile**, keyed by `app_type` (× stage × per-task override),
resolved entirely through the config-v2 units model. A `--filter` flag slices the facet you care
about (units, topology, lenses, or the whole resolved profile) so the system stays evolvable —
new facets slot in without new top-level commands.

## 2. The execution profile (three facets, one layer)

An **execution profile** is a mergeable config layer routed by `app_type`. It bundles three
facets — each independently overridable by scope (org → team → repo → project-local) and by a
task's own `app_type`:

```jsonc
// execution-profile layer (kind: layer) — merges by scope precedence
{
  "execution_profile": {
    "by_app_type": {
      "go-cli": {
        // facet 1 — UNIT RELEVANCE (the filter): which skills/agents/lenses, per stage
        "relevance": {
          "orchestrate": { "core": ["orchestrator-session-start","plan-wave-picker","loop-worker"],
                           "noise": ["article-extract","playwright"] },
          "verify":      { "core": ["verifier","test-runner"] },
          "review":      { "core": ["thermo-nuclear-code-quality-review","review-pr"],
                           "situational": ["self-review","review-delta"] }
        },
        // facet 2 — TOPOLOGY: the executor:verifier:reviewer fan-out shape
        "topology": {
          "executors": 1,
          "verifiers_per_executor": 2,      // n exec → 2n verifiers
          "reviewers": "per_verifier",      // or an int, or "per_executor"
          "verifier_sequence": ["unit","cli-runner"]   // supersedes app_type_verifier_map
        },
        // facet 3 — LENSES: the review-lens config (folds lens-evidence-policy routing here)
        "lenses": {
          "lens_set": ["architecture-standards","acceptance-invariants","adversarial"],
          "lens_concurrency": "parallel"    // parallel | gated | tiered
        }
      },
      // a different shape for a different kind of work — mixed usage is first-class
      "ideation": {
        "topology": { "executors": 3, "verifiers_per_executor": 0, "reviewers": 0 },
        "lenses":   { "lens_set": ["acceptance-invariants"], "lens_concurrency": "parallel" },
        "relevance":{ "orchestrate": { "core": ["plan-wave-picker"], "noise": ["test-runner"] } }
      }
    },
    "default_class": "situational"   // unlisted units are situational, never silently dropped
  }
}
```

- **Facet 1 (relevance)** is the noise filter: `noise` units are suppressed from the working set
  for the active `app_type` × `stage`; suppression is a reversible *view*, not a delete;
  `default_class: situational` means nothing unlisted is ever silently dropped.
- **Facet 2 (topology)** encodes the fan-out the user wants: `n` executors → `2n` verifiers,
  `1` reviewer per verifier, or `1 executor → m·n verifiers → 1 reviewer per`, etc. This is what
  app_type "structuralizes" — *what workflow execution model* a task runs under. It supersedes the
  flat `app_type_verifier_map` (the `verifier_sequence` moves inside `topology`).
- **Facet 3 (lenses)** folds `lens-evidence-policy`'s `lens_set` / `lens_concurrency` /
  `lens_routing` into the same per-app_type profile, so the lens half and the executor/verifier
  half are resolved together, not in two places.

## 3. Per-task selection + mixed usage

`TASKS.yaml` already carries a per-task `app_type` (the fanout resolver reads it). That becomes the
profile selector: **each task in a plan picks its execution shape by naming an `app_type`** (or
inherits `PLAN.yaml default_app_type`). So one plan can run an `ideation` task (3 executors, no
verifiers, one acceptance lens) and a `go-cli` task (1 exec → 2 verifiers → per-verifier review,
3 lenses gated) — the mixed reality the user lives in. New shapes are just new `app_type` keys;
no code change, just a layer entry.

## 4. config-v2 alignment (the load-bearing constraint)

Every element maps to a `config-distribution-model` §15 decision, so this rides the §15
resolution/lock/command machinery instead of duplicating it:

| Profile element | config-v2 §15 mechanism |
|---|---|
| skills/agents/lenses being selected | **artifact units** (D1 `kind: artifact`, D3 one `units` model) |
| the execution-profile policy | a **mergeable layer** (D1 `kind: layer`) — scope precedence gives org/team baselines + repo/project-local overrides |
| routing by app_type × stage × task | generalizes `app_type_verifier_map` (already app_type-routed) to the full profile |
| "learn from traces" without a clock | **driver-event staleness** (D4/D5) — recompute is explicit-only; trace-corpus digest folds into `inputs_digest` |
| persisted verdicts/profile state | the **one `units` lock** (R1/R7) — relevance verdict + evidence digest per unit; no parallel store |
| truth surface + gaps nudge | **D12** — `config explain` shows the resolved profile; `doctor` nudges gaps + stale-relevance |
| team/org profile writes | the **D11 `WriteAuthorizer`** editability check (already in `internal/config/editability.go`) |

Net: the execution profile is **a mergeable layer of artifact-unit selection + topology + lens
config, routed by app_type, resolved into the units lock, surfaced via `config explain`** — all
inside §15.

## 5. `da` command surface (D12-aligned)

- `da config relevance [--filter units|topology|lenses|all] [--app-type <t>] [--stage <s>] [--task <plan/task>]`
  — the inspector/slicer. Resolves the effective profile for the given context and prints just the
  requested facet. `--filter` keeps one command evolvable across facets instead of a verb per facet.
- `da config relevance --recompute [--write]` — the **explicit driver event** (D4): recompute
  unit-relevance candidates from the iter-log/`da score` corpus, print core/situational/noise +
  gaps, and with `--write` emit a *proposed* profile-layer diff routed by `--scope`/`--source`
  through the D11 editability check. Never auto-applies (explicit-only, human accepts).
- `da config explain` — extended to render the resolved execution profile (relevance class per
  unit + topology + lenses + freshness) as the single truth surface.
- `da doctor` — surfaces the gaps list + stale-profile review-nudges (guidance only).

## 6. Telemetry feedback loop (the data-driven half)

On an explicit `--recompute` (never on a timer): read the scored iteration corpus
(`iter-N.yaml` + `iter-N.score.yaml`); for each skill/agent compute a contribution signal
(cited-in-passing vs cited-in-low-scoring vs never-cited); emit proposed class changes (promote a
repeatedly-useful situational → core; flag a never-contributing unit → noise candidate) as a
profile-layer diff a human accepts. The corpus digest records into the layer's `inputs_digest`;
`config explain` reports "profile last recomputed at digest X (N new traces since)" (D4 nudge).
Content-hash, offline, explicit-only — exactly §15 D4/D5.

## 7. Phasing (config-v2 is mid-flight)

§15's units machinery is partly dormant (resolver core `p1` in progress), so:

- **Phase R1 — static, config-v2-shaped.** Ship the execution-profile layer schema + `da config
  relevance --filter …` reading the resolved profile, the iter-log/score corpus, and emitting
  relevance + topology + lenses + gaps; suppress `noise` units in the working set. Authored/merged
  through today's resolution (flat `.agentsrc.json` + local scope) but **shaped** to slot into the
  units model unchanged when §15 lands. No new lock section yet. **Topology + lenses start as
  declarative resolution the wave engine reads** (the engine already reads free_slots; it gains
  executors/verifiers/reviewers + lens config per resolved profile).
- **Phase R2 — units-lock wiring.** When `p4f` wires the §7A units lock, attach relevance verdicts
  + evidence digest per unit; `inputs_digest` covers the trace corpus; `config explain` reports
  freshness from the lock.
- **Phase R3 — feedback automation.** `--recompute` proposes profile diffs from accumulated scored
  traces; maintainer accepts via the governed write path.

R1 is independently useful (noise reduction + per-task topology the wave engine can act on) and
every artifact it produces is forward-compatible with §15.

## 8. Done criteria

1. An execution-profile layer validates against the layer schema and merges by scope precedence
   (no protected-field violations) — a real config-v2 layer, not a side file.
2. `da config relevance --filter units --app-type go-cli --stage review` classifies every resolved
   skill/agent/lens core/situational/noise with a gaps list, sourced from the iter-log/score corpus.
3. `da config relevance --filter topology --task <plan/task>` resolves the executor:verifier:reviewer
   fan-out + verifier_sequence for that task, honoring per-task `app_type` override.
4. The resolved working set suppresses `noise`-classed units; nothing unlisted is dropped.
5. Recompute is explicit-only; `config explain` reports profile freshness by content digest.
6. Writing a team/org profile goes through the D11 editability check; local is always writable.

## 9. Out of scope / deferred

- Auto-applying class/topology changes (always a proposed diff a human accepts — explicit-only).
- A separate profile store/DB (it lives in the units lock per R2; no parallel state).
- Cross-repo profile aggregation (single-repo first, like layered-fanout v1).
- The wave engine *acting on* topology beyond `executors`/free_slots is R1-min; richer
  verifier/reviewer fan-out execution is wired incrementally.

## 10. Relationships

- **config-distribution-model §15** — the model this rides on (units, lock, scopes, D11 editability).
- **r1-outcome-scoring** — the scored-iteration corpus the recompute reads.
- **layered-pr-fanout / lens-evidence-policy** — the lens facet folds in their `lens_routing` /
  `lens_set` / `lens_concurrency`; the topology facet folds in stage fan-out and `max_parallel_tasks`.
- **ultracode-wave-engine** — consumes the resolved topology (executors/verifiers/reviewers) per
  task instead of a flat per-wave cap.
