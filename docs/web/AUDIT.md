# docs/web content audit — items for maintainer review

This file flags ambiguous claims in the interactive docs site that could not
be verified with high confidence during the PR #143 content audit. Resolve
or accept each item, then delete this file before merging.

## In-place fixes already applied (FYI)

- `src/pages/diagrams/lens-dispatch.astro` — corrected SoT reference from
  `commands/review.go` (which is `da review` proposal-approval, unrelated) to
  the `isp` skill's `staged-runtime.md` § Review stage + the three per-lens
  agent definitions under
  `internal/scaffold/home/starter/agents/global/<lens>-reviewer/AGENT.md`.
- `src/data/da-resources.json` — added `global/` segment to all
  `~/.agents/{skills,agents}/<name>/` paths (actual scaffolded layout is
  `~/.agents/{skills,agents}/global/<name>/`); also corrected
  `cmd/dot-agents/main.go` → `cmd/da/main.go` (renamed in `91e5a342`).
- `src/data/workflow-resources.json` — corrected `task_count` for
  `pr10-branch-split` from 19 → 25 (actual count via `grep -c '^    - id:'`).
- `src/pages/about.astro` — replaced "Until PR #136 lands…" caveat with a
  note that PR #136 is merged (visible in master git log).

## Ambiguities flagged for maintainer attention

### A1. Resource-graph skills/agents that don't ship in the starter scaffold
`src/data/da-resources.json` lists skills as `hosts`-edges from
`scaffold:home`, but four skills referenced are not in
`internal/scaffold/home/starter/skills/global/`:

- `split-reviewable-commits`
- `gh-fix-ci`
- `skill-architect`
- `platform-docs-refresh`

They DO exist as real skills in the maintainer's live `~/.agents/skills/global/`,
but a fresh `da init` won't install them. Same situation for two agents:

- `verifier` (live at `~/.agents/agents/global/verifier/`, not in scaffold)
- `test-runner` (live at `~/.agents/agents/global/test-runner/`, not in scaffold)

**Decision needed:** are these "planned to ship in starter," or should the
graph be split into "starter-shipped" vs "user-supplemental" tiers, or
should the four non-shipped skills + two non-shipped agents be removed from
the graph entirely?

### A2. Three lens-reviewer agents present in scaffold but missing from the graph
The scaffold ships `acceptance-invariants-reviewer`,
`adversarial-reviewer`, and `architecture-standards-reviewer` (under
`internal/scaffold/home/starter/agents/global/`). The da-resources graph
does not include them. Once the lens-dispatch diagram page is built out (it
is currently stubbed), these three lens reviewers should probably appear in
the da-resources graph too.

### A3. workspace-state snapshot: stale bundle filenames
`src/data/workspace-state.json` references these delegation bundles by
filename:

- `del-pr8a-stale-refs-sweep-1779944616.yaml`
- `del-cg6b-b3a-globalflagcov-lift-1779928730.yaml`
- `del-p3-badge-and-countlinks-1779923836.yaml`

None of these are present in `.agents/active/delegation-bundles/` today
(only docs-interactive-html, gcc5-verify-close-unblock, release-v0-4-0,
signing-native-mac-windows, thermo-nuclear-lens-evaluation remain).

The file is labelled "captured_at: 2026-05-28" — a snapshot — so this may
be intentional (showing a moment-in-time view) or it may indicate the
snapshot was taken earlier and three bundles have since been
completed/archived. Either way: the `captured_at` date should likely
predate the bundle reaping, OR the snapshot should be refreshed to match
the current `.agents/active/delegation-bundles/` listing.

### A4. workflow-resources.json: snapshot date vs current state
The workflow-resources graph snapshot reflects pr10-branch-split with
delegations `del-docs-interactive-html-…`, `del-pr8a-stale-refs-sweep-…`,
`del-signing-native-1779944529`. Of these, `pr8a-stale-refs-sweep` was
merged on master (commit `1547e7ea Merge pull request #142`). The graph
still shows it as `task-inflight`. If the graph is intended as a
moment-in-time snapshot this is fine; if intended as live, refresh.

### A5. about.astro: "live JSON ingestion is a follow-up"
about.astro lists "Live JSON ingestion for workspace state (currently a
committed snapshot)" as stubbed for follow-up. Confirm this is still
accurate (i.e., no live-JSON wiring landed since the scaffold commit).

### A6. da-resources.json: `cli:da` `purpose` field
The `da CLI` node enumerates subcommands as: `init, add, refresh, doctor,
install, status, workflow, kg, review, skills, rules, agents, hooks, mcp,
settings, ux`. Worth a maintainer sanity check that `score` and
`session_stats` (visible in `commands/`) are intentionally omitted (likely
because they're internal/hidden), and that `import` / `remove` / `explain`
are intentionally omitted from this overview list.

## Done-when

Delete this file once each A-item above is either:
- accepted as-is (snapshot view, intentional omission), or
- folded back into the underlying JSON / page content.
